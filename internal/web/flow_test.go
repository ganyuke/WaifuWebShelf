package web

import (
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

func TestPublicAndAdminFlow(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Config{DataDir: dir, Port: "8080", AdminUser: "admin", AdminPass: "secret"}
	srv, err := New(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	mustOK := func(path string) string {
		t.Helper()
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("%s: %d\n%s", path, res.StatusCode, body)
		}
		if strings.Contains(string(body), "template error") {
			t.Fatalf("template error on %s", path)
		}
		return string(body)
	}

	mustOK("/")
	mustOK("/series")
	mustOK("/merch")
	mustOK("/stats")
	mustOK("/healthz")
	mustOK("/login")

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	post := func(path string, form url.Values) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := post("/login", url.Values{"username": {"admin"}, "password": {"secret"}})
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatalf("login %d", res.StatusCode)
	}
	res = post("/admin/series/new", url.Values{"name": {"Spice and Wolf"}})
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatalf("create series %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "/admin/series/") || !strings.Contains(loc, "/edit") {
		t.Fatalf("create series location %s", loc)
	}

	ser, err := store.GetSeriesBySlug("spice-and-wolf")
	if err != nil {
		t.Fatal(err)
	}
	res = post("/admin/series/"+strconv.FormatInt(ser.ID, 10)+"/book-lines/new", url.Values{
		"kind": {"light_novel"}, "total_volumes": {"3"},
		"default_currency": {"CAD"}, "default_msrp": {"14.99"},
	})
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatalf("create line %d", res.StatusCode)
	}
	lines, err := store.ListBookLines(ser.ID)
	if err != nil || len(lines) != 1 {
		t.Fatalf("lines %v %v", lines, err)
	}
	res = post("/admin/book-lines/"+strconv.FormatInt(lines[0].ID, 10)+"/volumes/new", url.Values{
		"volume_number": {"1"}, "currency": {"CAD"}, "msrp": {"15.00"}, "price_ex_tax": {"12.00"}, "price_inc_tax": {"13.80"},
	})
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatalf("create volume %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "/admin/book-lines/") {
		t.Fatalf("create volume location %s", loc)
	}
	for _, price := range []string{"2.00", "10.00", "2.00"}[1:] {
		res = post("/admin/book-lines/"+strconv.FormatInt(lines[0].ID, 10)+"/volumes/new", url.Values{
			"volume_number": {"1"}, "currency": {"CAD"}, "store": {"Book Off"}, "price_ex_tax": {price},
		})
		res.Body.Close()
		if res.StatusCode != 303 {
			t.Fatalf("create extra copy %s: %d", price, res.StatusCode)
		}
	}

	body := mustOK("/series/spice-and-wolf")
	if !strings.Contains(body, "Light novel  1 / 3 · 3 copies") {
		t.Fatalf("grid caption missing:\n%s", body)
	}
	if !strings.Contains(body, "copy-count") {
		t.Fatal("expected copy badge")
	}
	if !strings.Contains(body, "vol-cell owned") || !strings.Contains(body, "vol-cell missing") {
		t.Fatal("expected owned and missing cells")
	}

	res = post("/admin/series/"+strconv.FormatInt(ser.ID, 10)+"/merch/new", url.Values{
		"name": {"Holo figure"}, "kind": {"figure"}, "quantity": {"1"}, "currency": {"CAD"}, "msrp": {"80"}, "price_ex_tax": {"60"},
	})
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatalf("create merch %d", res.StatusCode)
	}
	mustOK("/merch")
	stats := mustOK("/stats")
	if !strings.Contains(stats, "CAD") {
		t.Fatalf("stats missing currency:\n%s", stats)
	}
	if !strings.Contains(stats, "Spice and Wolf") || !strings.Contains(stats, "Holo figure") {
		t.Fatalf("stats missing purchases:\n%s", stats)
	}

	vols, err := store.ListVolumes(lines[0].ID)
	if err != nil || len(vols) != 3 {
		t.Fatalf("volumes %v %v", vols, err)
	}
	if lines[0].Slug != "light-novel" {
		t.Fatalf("book line slug %q", lines[0].Slug)
	}
	res = post("/admin/links", url.Values{
		"owner_type": {"volume_number"},
		"owner_id":   {strconv.FormatInt(lines[0].ID, 10)},
		"owner_key":  {"1"},
		"label":      {"Yen Press"},
		"url":        {"https://yenpress.com/example"},
		"next":       {"/admin"},
	})
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatalf("create volume link %d", res.StatusCode)
	}
	copiesPage := mustOK("/series/spice-and-wolf/light-novel/1")
	if !strings.Contains(copiesPage, "3 copies") {
		t.Fatalf("copies page:\n%s", copiesPage)
	}
	if !strings.Contains(copiesPage, "Yen Press") {
		t.Fatalf("copies page missing volume link:\n%s", copiesPage)
	}
	merch, err := store.ListMerch(ser.ID)
	if err != nil || len(merch) != 1 {
		t.Fatalf("merch %v %v", merch, err)
	}
	mustOK("/merch/" + strconv.FormatInt(merch[0].ID, 10))

	adminGet := func(path string) string {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("GET %s: %d\n%s", path, res.StatusCode, body)
		}
		return string(body)
	}
	adminGet("/admin")
	adminGet("/admin/series/new")
	adminGet("/admin/series/" + strconv.FormatInt(ser.ID, 10) + "/edit")
	adminGet("/admin/book-lines/" + strconv.FormatInt(lines[0].ID, 10) + "/edit")
	newVol := adminGet("/admin/book-lines/" + strconv.FormatInt(lines[0].ID, 10) + "/volumes/new")
	if !strings.Contains(newVol, `value="14.99"`) {
		t.Fatalf("new volume form missing default MSRP:\n%s", newVol)
	}
	adminGet("/admin/volumes/" + strconv.FormatInt(vols[0].ID, 10) + "/edit")
	adminGet("/admin/merch/" + strconv.FormatInt(merch[0].ID, 10) + "/edit")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin", nil)
	anon := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err = anon.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 303 || !strings.Contains(res.Header.Get("Location"), "/login") {
		t.Fatalf("admin without auth %d %s", res.StatusCode, res.Header.Get("Location"))
	}
}
