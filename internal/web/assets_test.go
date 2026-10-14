package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"waifuwebshelf/internal/config"
)

func TestAssetContentVersions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "style.css")
	write := func(body string) {
		t.Helper()
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.Chtimes(path, time.Unix(100, 0), time.Unix(100, 0)); e != nil {
			t.Fatal(e)
		}
	}
	load := func() staticAssets {
		t.Helper()
		a, e := loadStaticAssets(dir)
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	get := func(a staticAssets, url, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", url, nil)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	write("body{color:red}")
	first := load()
	version := first.URL("style.css")
	if !strings.Contains(version, "?v=") {
		t.Fatal(version)
	}
	if load().URL("style.css") != version {
		t.Fatal("unchanged assets changed version")
	}
	w := get(first, version, "")
	tag := w.Header().Get("ETag")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") || !strings.Contains(w.Header().Get("Content-Type"), "text/css") {
		t.Fatal(w.Code, w.Header())
	}
	if w := get(first, version, tag); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("asset did not revalidate")
	}
	write("body{color:tan}") // Same length and modification time; only the content changed.
	second := load()
	if second.URL("style.css") == version {
		t.Fatal("changed bytes did not change URL")
	}
	if w := get(first, version, ""); w.Body.String() != "body{color:red}" {
		t.Fatal("running process changed immutable asset")
	}
	for _, url := range []string{version, "/static/style.css"} {
		w := get(second, url, tag)
		if w.Code != 200 || w.Body.String() != "body{color:tan}" || strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
			t.Fatal("legacy/stale version was pinned", w.Code, w.Header())
		}
	}
	if w := get(second, "/static/missing.css", tag); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestTemplatesUseVersionedAssets(t *testing.T) {
	s, e := New(config.Config{DataDir: t.TempDir()}, nil)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/login", nil)
	s.render(w, r, "login", "Log in", map[string]any{"Next": "/"})
	for _, name := range []string{"style.css", "forms.js", "favicon.svg"} {
		if !strings.Contains(w.Body.String(), s.assets.URL(name)) {
			t.Fatalf("missing versioned %s", name)
		}
	}
	w = httptest.NewRecorder()
	e = s.tmpl.ExecuteTemplate(w, "item_form", View{Data: itemForm{Type: "book", Errors: map[string]string{}}})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(w.Body.String(), s.assets.URL("items.js")) {
		t.Fatal("items.js not versioned")
	}
}
