package web

import (
	"bytes"
	"database/sql"
	"io"
	"mime/multipart"
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
	wid, err := store.CreateWork(sid, db.WorkWrite{Kind: "manga"})
	if err != nil {
		t.Fatal(err)
	}
	lid, err := store.CreateEdition(wid, db.EditionWrite{Language: "en", DefaultCurrency: "CAD"})
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

	books := url.Values{"receipt_line": {strconv.FormatInt(lines[0].ID, 10)}, "edition": {strconv.FormatInt(lid, 10)}, "volumes": {"1-3"}}
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
	if first := get("/series/rag/manga/english/1"); !strings.Contains(first, `<span class="off">← Previous</span>`) || !strings.Contains(first, `rel="next"`) {
		t.Fatal("the first volume should show Previous greyed out and link Next")
	}
	vol := get("/series/rag/manga/english/2")
	if !strings.Contains(vol, "+ tax") || !strings.Contains(vol, "Same trip") || !strings.Contains(vol, `href="/trips/`+trip+`"`) {
		t.Fatal("volume page should link its trip and show what else was bought")
	}

	// A series not on the shelf yet is added from the same form.
	fly := url.Values{"receipt_line": {strconv.FormatInt(lines[0].ID, 10)}, "edition": {"new"}, "kind": {"manga"}, "language": {"en"}, "volumes": {"1-2"}}
	if res, body := post("/admin/trips/"+trip+"/books", fly); res.StatusCode != 422 || !strings.Contains(body, "Enter a series name.") {
		t.Fatalf("missing series name should be refused: %d", res.StatusCode)
	}
	fly.Set("series_name", "Fly Me to the Moon")
	if res, _ := post("/admin/trips/"+trip+"/books", fly); res.StatusCode != 303 {
		t.Fatalf("add books from a new series %d", res.StatusCode)
	}
	if page := get("/trips/" + trip); !strings.Contains(page, "4 of 8") || !strings.Contains(page, "Fly Me to the Moon") {
		t.Fatal("trip page should show the new series' books on the receipt line")
	}
	get("/series/fly-me-to-the-moon/manga/english/2")

	// Hovering a small picture shows the same details as its page.
	card := get("/preview/series/rag/manga/english/2")
	for _, want := range []string{"Rent-A-Girlfriend vol. 2", "English manga", "11 Oct 2026 at Book Off Robson", "Before tax"} {
		if !strings.Contains(card, want) {
			t.Fatalf("volume card missing %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "<html") {
		t.Fatal("a card is a fragment, not a page")
	}
	if card := get("/preview/series/rag/manga/english/9"); !strings.Contains(card, "Not owned") {
		t.Fatal("a missing volume's card should say Not owned")
	}
	if card := get("/preview/trips/" + trip); !strings.Contains(card, "Book Off Robson") || !strings.Contains(card, "CA$23.52") {
		t.Fatal("trip card should show the store and what was paid")
	}
	if res, err := client.Get(ts.URL + "/preview/merch/999"); err != nil || res.StatusCode != 404 {
		t.Fatal("unknown items give no card")
	}

	// Tags only show on a trip with a photo, so tag through the store and check the item page.
	vols, _ := store.ListVolumesByNumber(lid, "2")
	if err := store.AddTag(tripID, "volume", vols[0].ID, 250, 400); err != nil {
		t.Fatal(err)
	}
	if res, _ := post("/admin/trips/"+trip+"/tags", url.Values{"item": {"volume:" + strconv.FormatInt(vols[0].ID, 10)}, "x": {"300"}, "y": {"410"}}); res.StatusCode != 303 {
		t.Fatalf("tag %d", res.StatusCode)
	}
	if tags, _ := store.TripTags(tripID); len(tags) != 1 || tags[0].X != 300 {
		t.Fatalf("tags %+v", tags)
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
}

func TestReceiptPhotoIsPrivate(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv, err := New(config.Config{DataDir: dir, AdminUser: "admin", AdminPass: "secret"}, store)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	visitor := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	login := url.Values{"username": {"admin"}, "password": {"secret"}}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/login", strings.NewReader(login.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", ts.URL)
	if res, err := owner.Do(req); err != nil || res.StatusCode != 303 {
		t.Fatalf("login %v", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range map[string]string{"visited_on": "2026-10-11", "store": "new", "store_name": "Book Off", "currency": "CAD"} {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("receipt", "receipt.png")
	_, _ = fw.Write(tinyPNG())
	_ = mw.Close()
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/admin/trips/new", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", ts.URL)
	res, err := owner.Do(req)
	if err != nil || res.StatusCode != 303 {
		t.Fatalf("create trip %v %d", err, res.StatusCode)
	}
	trips, _ := store.ListTrips()
	if len(trips) != 1 || !trips[0].ReceiptPath.Valid {
		t.Fatalf("receipt not saved: %+v", trips)
	}
	name := trips[0].ReceiptPath.String
	path := "/admin/trips/" + strconv.FormatInt(trips[0].ID, 10) + "/receipt"

	for _, c := range []struct {
		client *http.Client
		path   string
		want   int
	}{
		{visitor, "/images/" + name, 404},
		{owner, "/images/" + name, 404},
		{visitor, path, 303},
		{owner, path, 200},
	} {
		res, err := c.client.Get(ts.URL + c.path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != c.want {
			t.Errorf("%s: got %d, want %d", c.path, res.StatusCode, c.want)
		}
	}
	if page, _ := visitor.Get(ts.URL + "/trips/" + strconv.FormatInt(trips[0].ID, 10)); page != nil {
		b, _ := io.ReadAll(page.Body)
		page.Body.Close()
		if strings.Contains(string(b), name) {
			t.Error("trip page shows the receipt file to visitors")
		}
	}
}
