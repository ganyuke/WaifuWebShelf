package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPageCacheContentValidators(t *testing.T) {
	server := func(content string) http.Handler {
		s := &Server{pageHTML: map[string][]byte{}}
		return s.withPageCache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/missing" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(content + r.URL.RequestURI()))
		}))
	}
	get := func(h http.Handler, path, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	first := server("before")
	original := get(first, "/", "")
	tag := original.Header().Get("ETag")
	if tag == "" {
		t.Fatal("missing validator")
	}
	for _, validator := range []string{tag, strings.TrimPrefix(tag, "W/"), `"unrelated", ` + tag} {
		w := get(first, "/", validator)
		if w.Code != 304 || w.Body.Len() != 0 {
			t.Fatalf("conditional hit: %d %q", w.Code, w.Body.String())
		}
	}
	// An unchanged page can revalidate even when the new process has an empty cache.
	if w := get(server("before"), "/", tag); w.Code != 304 {
		t.Fatalf("unchanged restart: %d", w.Code)
	}
	// Changed HTML after a restart must never re-use the old generation's validator.
	w := get(server("after"), "/", tag)
	if w.Code != 200 || w.Header().Get("ETag") == tag || w.Body.String() != "after/" {
		t.Fatalf("changed restart: %d %s", w.Code, w.Body.String())
	}
	if w := get(first, "/other", tag); w.Code != 200 {
		t.Fatalf("different page: %d", w.Code)
	}
	for _, validator := range []string{tag, "*"} {
		if w := get(first, "/missing", validator); w.Code != 404 {
			t.Fatalf("missing page: %d", w.Code)
		}
	}
	for _, query := range []string{"a", "b", "a"} {
		w := get(first, "/?q="+query, "")
		if w.Body.String() != "before/?q="+query {
			t.Fatalf("query cache collision: %s", w.Body.String())
		}
	}
}

type observeHeaders struct {
	*httptest.ResponseRecorder
	check func()
}

func (w observeHeaders) WriteHeader(code int) { w.check(); w.ResponseRecorder.WriteHeader(code) }

func TestSaveInvalidatesBeforeRedirect(t *testing.T) {
	s := &Server{pageHTML: map[string][]byte{"/": []byte("old")}, sessions: map[string]time.Time{"test": time.Now().Add(time.Hour)}}
	r := httptest.NewRequest("POST", "/admin/items/new", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test"})
	w := observeHeaders{ResponseRecorder: httptest.NewRecorder(), check: func() {
		if s.currentPageGen() != 1 {
			t.Error("redirect sent before invalidation")
		}
		if _, ok := s.cachedPage("/"); ok {
			t.Error("old page still cached at redirect")
		}
	}}
	s.withPageCache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", 303) })).ServeHTTP(w, r)
	// A render started before the save cannot repopulate the new generation.
	s.storePage("/", []byte("stale in-flight render"), 0)
	if _, ok := s.cachedPage("/"); ok {
		t.Fatal("stale render repopulated cache")
	}
}
