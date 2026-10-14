package web

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/db"
)

func TestTripFlow(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sid, err := store.CreateSeries("Rent-A-Girlfriend", "rag", "", sql.NullString{})
	if err != nil {
		t.Fatal(err)
	}
	lid, err := store.CreateBookLine(sid, db.BookLineWrite{Kind: "manga", Label: "English", DefaultCurrency: "CAD"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateVolume(lid, "1", 100, db.Purchase{PurchasedAt: "2024-09-02", Store: "Book Off", Currency: "CAD"}); err != nil {
		t.Fatal(err)
	}

	srv, err := New(config.Config{DataDir: dir, AdminUser: "admin", AdminPass: "secret"}, store)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(path string, form url.Values) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", ts.URL)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}
	get := func(path string) string {
		t.Helper()
		res, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 || strings.Contains(string(b), "template error") {
			t.Fatalf("%s: %d\n%s", path, res.StatusCode, b)
		}
		return string(b)
	}
	if res, _ := post("/login", url.Values{"username": {"admin"}, "password": {"secret"}}); res.StatusCode != 303 {
		t.Fatalf("login %d", res.StatusCode)
	}

	get("/admin/trips/new")
	res, body := post("/admin/trips/new", url.Values{
		"visited_on": {"2026-10-11"}, "store": {"new"}, "store_name": {"Book Off Robson"}, "store_currency": {"CAD"}, "store_tax": {"12"},
		"line_id": {"", "", ""}, "line_label": {"Book", "Keychain", ""}, "line_price": {"2", "5", ""}, "line_count": {"8", "", ""},
		"paid": {"23.52"},
	})
	if res.StatusCode != 303 {
		t.Fatalf("create trip %d\n%s", res.StatusCode, body)
	}
	trip := strings.TrimPrefix(strings.Split(res.Header.Get("Location"), "?")[0], "/trips/")
	tripID, _ := strconv.ParseInt(trip, 10, 64)
	lines, err := store.ReceiptLines(tripID)
	if err != nil || len(lines) != 2 || lines[1].Count != 1 {
		t.Fatalf("receipt lines %+v %v", lines, err)
	}

	books := url.Values{"receipt_line": {strconv.FormatInt(lines[0].ID, 10)}, "book_line": {strconv.FormatInt(lid, 10)}, "volumes": {"1-3"}}
	if res, body := post("/admin/trips/"+trip+"/books", books); res.StatusCode != 422 || !strings.Contains(body, "Already owned: 1.") {
		t.Fatalf("owned volume should be refused: %d", res.StatusCode)
	}
	books.Set("volumes", "2, 3")
	if res, _ := post("/admin/trips/"+trip+"/books", books); res.StatusCode != 303 {
		t.Fatalf("add books %d", res.StatusCode)
	}
	res, body = post("/admin/items/new", url.Values{"type": {"merch"}, "series": {strconv.FormatInt(sid, 10)}, "name": {"Chizuru keychain"},
		"quantity": {"1"}, "trip_id": {trip}, "currency": {"CAD"}, "receipt_line": {strconv.FormatInt(lines[1].ID, 10)}, "action": {"save"}})
	if res.StatusCode != 303 || !strings.HasPrefix(res.Header.Get("Location"), "/trips/"+trip) {
		t.Fatalf("add merch %d %s\n%s", res.StatusCode, res.Header.Get("Location"), body)
	}

	page := get("/trips/" + trip)
	for _, want := range []string{"Book Off Robson", "CA$23.52", "2 of 8", "1 of 1", "Chizuru keychain", "Vol. 3"} {
		if !strings.Contains(page, want) {
			t.Fatalf("trip page missing %q", want)
		}
	}
	vol := get("/series/rag/english/2")
	if !strings.Contains(vol, "+ tax") || !strings.Contains(vol, "Same trip") || !strings.Contains(vol, `href="/trips/`+trip+`"`) {
		t.Fatal("volume page should link its trip and show what else was bought")
	}

	// A visit with nothing bought still counts as a trip.
	if res, _ := post("/admin/trips/new", url.Values{"visited_on": {"2026-10-04"}, "store": {"1"}}); res.StatusCode != 303 {
		t.Fatalf("empty visit %d", res.StatusCode)
	}
	stats := get("/stats")
	for _, want := range []string{"Bought nothing", "Oct 2026", "Keychain", "CA$23.52"} {
		if !strings.Contains(stats, want) {
			t.Fatalf("spending page missing %q", want)
		}
	}

	// Earlier purchases are grouped only after a backup.
	if res, _ := post("/admin/trips/group", nil); res.StatusCode != 303 {
		t.Fatalf("group %d", res.StatusCode)
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "backups", "*.db"))
	if len(backups) != 1 {
		t.Fatalf("backups %v", backups)
	}
	if n, _, _ := store.UngroupedPurchases(); n != 0 {
		t.Fatalf("still ungrouped: %d", n)
	}
}
