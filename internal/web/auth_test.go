package web

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/db"
)

func mustProxies(t *testing.T, raw string) proxyList {
	t.Helper()
	p, err := parseProxies(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClientIP(t *testing.T) {
	p := mustProxies(t, "127.0.0.1,::1,10.0.0.0/8")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	if got := p.clientIP(req); got != "127.0.0.1" {
		t.Fatalf("remote %q", got)
	}

	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := p.clientIP(req); got != "203.0.113.9" {
		t.Fatalf("forwarded %q", got)
	}

	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9")
	if got := p.clientIP(req); got != "203.0.113.9" {
		t.Fatalf("visitor-added entry believed: %q", got)
	}

	req.Header.Del("X-Forwarded-For")
	req.Header.Set("X-Real-IP", "198.51.100.4")
	if got := p.clientIP(req); got != "198.51.100.4" {
		t.Fatalf("real-ip %q", got)
	}
}

func TestClientIPIgnoresHeadersFromStrangers(t *testing.T) {
	p := mustProxies(t, "127.0.0.1")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.77:4000"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := p.clientIP(req); got != "198.51.100.77" {
		t.Fatalf("spoofed ip %q", got)
	}
	if p.https(req) {
		t.Fatal("spoofed https")
	}
}

func TestParseProxiesRejectsGarbage(t *testing.T) {
	if _, err := parseProxies("127.0.0.1, nope"); err == nil {
		t.Fatal("want error")
	}
}

func TestRequestHTTPS(t *testing.T) {
	p := mustProxies(t, "127.0.0.1")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	if p.https(req) {
		t.Fatal("plain http")
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	if !p.https(req) {
		t.Fatal("forwarded https")
	}
	req.Header.Set("X-Forwarded-Proto", "https, http")
	if !p.https(req) {
		t.Fatal("first hop https")
	}
}

func TestLoginCookieSecureBehindProxy(t *testing.T) {
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

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	form := url.Values{"username": {"admin"}, "password": {"secret"}}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatalf("status %d", res.StatusCode)
	}
	secure := false
	for _, c := range res.Cookies() {
		if c.Name == sessionCookie {
			secure = c.Secure
		}
	}
	if !secure {
		t.Fatalf("cookie %#v", res.Cookies())
	}
}

func TestLoginFailedLogsUsername(t *testing.T) {
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

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	form := url.Values{"username": {"nope"}, "password": {"wrong"}}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	out := buf.String()
	if !strings.Contains(out, `login failed user="nope" ip=203.0.113.9`) {
		t.Fatalf("missing failed login log:\n%s", out)
	}
	if !strings.Contains(out, "POST /login ip=203.0.113.9 cache=skip") {
		t.Fatalf("missing request log:\n%s", out)
	}
}
