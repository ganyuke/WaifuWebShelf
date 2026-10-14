package web

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) withPageCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cache := "skip"
		defer func() { s.logRequest(r, cache) }()

		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/admin") && s.loggedIn(r) {
			cw := newCaptureWriter()
			next.ServeHTTP(cw, r)
			setPrivate(cw.h)
			if cw.code == http.StatusSeeOther {
				s.bumpPages()
				cache = "bust"
			}
			writeCaptured(w, cw)
			return
		}

		if !s.publicCacheable(r) {
			if s.loggedIn(r) || strings.HasPrefix(r.URL.Path, "/admin") || r.URL.Query().Get("ok") != "" {
				setPrivate(w.Header())
			}
			next.ServeHTTP(w, r)
			return
		}

		key := r.URL.RequestURI()
		if body, ok := s.cachedPage(key); ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			etag := pageETag(body)
			setPublicPage(w.Header(), etag)
			if etagMatch(r.Header.Get("If-None-Match"), etag) {
				w.WriteHeader(http.StatusNotModified)
				cache = "304"
				return
			}
			_, _ = w.Write(body)
			cache = "hit"
			return
		}

		gen := s.currentPageGen()
		cw := newCaptureWriter()
		next.ServeHTTP(cw, r)
		if cw.code == http.StatusOK {
			etag := pageETag(cw.body.Bytes())
			s.storePage(key, cw.body.Bytes(), gen)
			setPublicPage(cw.h, etag)
			cache = "miss"
			if etagMatch(r.Header.Get("If-None-Match"), etag) {
				cw.code = http.StatusNotModified
				cache = "304"
			}
		}
		writeCaptured(w, cw)
	})
}

func (s *Server) publicCacheable(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if s.loggedIn(r) {
		return false
	}
	if r.URL.Query().Get("ok") != "" {
		return false
	}
	path := r.URL.Path
	switch path {
	case "/login", "/logout", "/healthz", "/favicon.ico":
		return false
	}
	if strings.HasPrefix(path, "/admin") || strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/images/") {
		return false
	}
	return true
}

func setPrivate(h http.Header) {
	h.Set("Cache-Control", "private, no-store")
	h.Set("Vary", "Cookie")
}

func setPublicPage(h http.Header, etag string) {
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=0, must-revalidate")
	h.Set("Vary", "Cookie")
}

func (s *Server) bumpPages() {
	s.pageMu.Lock()
	s.pageGen++
	s.pageHTML = map[string][]byte{}
	s.pageMu.Unlock()
}

func (s *Server) currentPageGen() uint64 {
	s.pageMu.Lock()
	defer s.pageMu.Unlock()
	return s.pageGen
}

func pageETag(body []byte) string {
	return fmt.Sprintf(`W/"%x"`, sha256.Sum256(body))
}

func (s *Server) cachedPage(path string) ([]byte, bool) {
	s.pageMu.Lock()
	defer s.pageMu.Unlock()
	body, ok := s.pageHTML[path]
	return body, ok
}

func (s *Server) storePage(path string, body []byte, gen uint64) {
	s.pageMu.Lock()
	defer s.pageMu.Unlock()
	if gen != s.pageGen {
		return
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	s.pageHTML[path] = cp
}

func etagMatch(header, etag string) bool {
	if header == "" || etag == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

type captureWriter struct {
	h           http.Header
	code        int
	wroteHeader bool
	body        bytes.Buffer
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{h: make(http.Header), code: http.StatusOK}
}

func (c *captureWriter) Header() http.Header { return c.h }

func (c *captureWriter) WriteHeader(code int) {
	if c.wroteHeader {
		return
	}
	c.wroteHeader = true
	c.code = code
}

func (c *captureWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	return c.body.Write(p)
}

func writeCaptured(w http.ResponseWriter, c *captureWriter) {
	dst := w.Header()
	for k, vs := range c.h {
		dst[k] = vs
	}
	w.WriteHeader(c.code)
	if c.body.Len() > 0 && c.code != http.StatusNotModified {
		_, _ = w.Write(c.body.Bytes())
	}
}
