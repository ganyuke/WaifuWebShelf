package web

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/db"
)

func TestAnonymousPageCache(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	srv, err := New(config.Config{DataDir: dir, Port: "8080", AdminUser: "admin", AdminPass: "secret"}, store)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	get := func(path string, extra func(*http.Request)) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if extra != nil {
			extra(req)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := get("/", nil)
	_, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("home %d", res.StatusCode)
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	if !strings.Contains(res.Header.Get("Cache-Control"), "must-revalidate") {
		t.Fatalf("cache-control %q", res.Header.Get("Cache-Control"))
	}
	if !strings.Contains(res.Header.Get("Vary"), "Cookie") {
		t.Fatalf("missing Vary Cookie: %q", res.Header.Get("Vary"))
	}

	res = get("/", func(req *http.Request) { req.Header.Set("If-None-Match", etag) })
	res.Body.Close()
	if res.StatusCode != 304 {
		t.Fatalf("revalidate %d", res.StatusCode)
	}

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
	post := func(path string, form url.Values) {
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
		res.Body.Close()
		if res.StatusCode != 303 {
			t.Fatalf("POST %s %d", path, res.StatusCode)
		}
	}
	post("/login", url.Values{"username": {"admin"}, "password": {"secret"}})
	post("/admin/series/new", url.Values{"name": {"Spice and Wolf"}})

	res = get("/", nil)
	home, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(home), "Spice and Wolf") {
		t.Fatalf("cache not invalidated:\n%s", home)
	}
	if res.Header.Get("ETag") == etag {
		t.Fatal("etag should change after save")
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("signed-in home %d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
		t.Fatalf("signed-in cache-control %q", res.Header.Get("Cache-Control"))
	}
	if strings.Contains(res.Header.Get("Cache-Control"), "public") {
		t.Fatal("signed-in response must not be public")
	}
}
