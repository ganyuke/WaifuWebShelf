package web

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"waifuwebshelf/internal/db"
)

func TestWorkAndVolumeNames(t *testing.T) {
	srv, ts := newTestServer(t)
	store := srv.db
	sid, err := store.CreateSeries("K-On!", "k-on", "", sql.NullString{})
	if err != nil {
		t.Fatal(err)
	}
	mainWork, _ := store.CreateWork(sid, db.WorkWrite{Kind: "manga"})
	shuffleWork, _ := store.CreateWork(sid, db.WorkWrite{Kind: "manga", Title: "K-On! Shuffle"})
	mainEn, _ := store.CreateEdition(mainWork, db.EditionWrite{Language: "en", DefaultCurrency: "USD"})
	shuffleEn, _ := store.CreateEdition(shuffleWork, db.EditionWrite{Language: "en", DefaultCurrency: "USD"})
	bought := db.Purchase{PurchasedAt: "2026-10-17", Currency: "USD"}
	mainVol, err := store.CreateVolume(mainEn, "1", 100, bought)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateVolume(shuffleEn, "1", 100, bought); err != nil {
		t.Fatal(err)
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) string {
		t.Helper()
		res, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("%s: %d\n%s", path, res.StatusCode, body)
		}
		return string(body)
	}

	// The spinoff is named by its own title, and the main manga by the series.
	home := get("/")
	for _, want := range []string{">K-On! Shuffle vol. 1</a>", ">K-On! vol. 1</a>", "K-On! Shuffle, English manga"} {
		if !strings.Contains(home, want) {
			t.Errorf("home missing %q", want)
		}
	}
	if page := get("/series/k-on/k-on-shuffle/english/1"); !strings.Contains(page, "<h1>K-On! Shuffle vol. 1") {
		t.Error("spinoff volume page should be headed by the spinoff's name")
	}
	series := get("/series/k-on")
	for _, want := range []string{`id="manga"`, `id="k-on-shuffle"`, `id="k-on-shuffle-english"`, `<h2>K-On! Shuffle <span class="count">Manga</span></h2>`} {
		if !strings.Contains(series, want) {
			t.Errorf("series page missing %q", want)
		}
	}

	// A volume with its own title goes by it, and the number moves under it.
	res, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"secret"}})
	if err != nil || res.StatusCode != http.StatusSeeOther {
		t.Fatalf("login %v %v", err, res)
	}
	res, err = client.PostForm(ts.URL+"/admin/volumes/"+formatID(mainVol)+"/edit", url.Values{
		"volume_number": {"1"}, "volume_title": {"K-On! College Days"}, "currency": {"USD"}, "purchased_at": {"2026-10-17"},
	})
	if err != nil || res.StatusCode != http.StatusSeeOther {
		t.Fatalf("save title %v %v", err, res)
	}
	page := get("/series/k-on/manga/english/1")
	if !strings.Contains(page, "<h1>K-On! College Days") || !strings.Contains(page, "vol. 1, English manga") {
		t.Error("titled volume page should use the title and show the number under it")
	}
	if !strings.Contains(get("/"), ">K-On! College Days</a>") {
		t.Error("home should list the titled volume by its title")
	}
}

func TestWorkAdmin(t *testing.T) {
	srv, ts := newTestServer(t)
	sid, _ := srv.db.CreateSeries("Fullmetal Alchemist", "fma", "", sql.NullString{})
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if res, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"secret"}}); err != nil || res.StatusCode != 303 {
		t.Fatal("login")
	}
	post := func(path string, form url.Values) (*http.Response, string) {
		t.Helper()
		res, err := client.PostForm(ts.URL+path, form)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res, string(body)
	}

	if res, body := post("/admin/series/"+formatID(sid)+"/works/new", url.Values{"kind": {"comic"}, "language": {"en"}}); res.StatusCode != 200 || !strings.Contains(body, "Pick a medium.") {
		t.Fatalf("bad medium %d", res.StatusCode)
	}
	res, _ := post("/admin/series/"+formatID(sid)+"/works/new", url.Values{"kind": {"manga"}, "language": {"en"}, "edition_name": {"3-in-1"}})
	if res.StatusCode != 303 {
		t.Fatalf("new work %d", res.StatusCode)
	}
	eds, _ := srv.db.ListSeriesEditions(sid)
	if len(eds) != 1 || eds[0].Volume("3", "") != "Fullmetal Alchemist 3-in-1 vol. 3" {
		t.Fatalf("editions %+v", eds)
	}
	if res, body := post("/admin/editions/"+formatID(eds[0].ID)+"/edit", url.Values{"language": {"xx"}}); res.StatusCode != 200 || !strings.Contains(body, "Pick a language.") {
		t.Fatalf("bad language %d", res.StatusCode)
	}
	if res, _ := post("/admin/works/"+formatID(eds[0].WorkID)+"/editions/new", url.Values{"language": {"ja"}}); res.StatusCode != 303 {
		t.Fatalf("second edition %d", res.StatusCode)
	}
	if res, _ := post("/admin/works/"+formatID(eds[0].WorkID)+"/delete", nil); res.StatusCode != 303 {
		t.Fatalf("delete work %d", res.StatusCode)
	}
	if eds, _ := srv.db.ListSeriesEditions(sid); len(eds) != 0 {
		t.Fatalf("editions left %+v", eds)
	}
}
