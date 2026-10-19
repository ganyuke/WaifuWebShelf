package favicon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

var png1x1 = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

var ico = []byte("\x00\x00\x01\x00\x01\x00\x10\x10\x00\x00\x01\x00\x20\x00\x00\x00\x00\x00\x16\x00\x00\x00")

// fakeSite serves pages on 127.0.0.1, which Host turns away for having no
// dot-separated name, so tests look links up by their real host.
func fakeSite(t *testing.T, routes map[string][]byte) (*Store, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != UserAgent {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	s, err := New(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	return s, srv.URL
}

func fetch(t *testing.T, s *Store, site, host string) string {
	t.Helper()
	u, _ := url.Parse(site)
	name, err := s.download(context.Background(), &url.URL{Scheme: "http", Host: u.Host, Path: "/"}, host)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestDeclaredIcon(t *testing.T) {
	s, site := fakeSite(t, map[string][]byte{
		"/":               []byte(`<html><head><link rel="apple-touch-icon" href="/big.png"><link rel="icon" type="image/svg+xml" href="/i.svg"><link rel="shortcut icon" sizes="32x32" href="static/i32.png"></head><body><link rel="icon" href="/late.png"></body></html>`),
		"/static/i32.png": png1x1,
		"/big.png":        ico,
		"/favicon.ico":    ico,
		"/i.svg":          []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"/late.png":       ico,
	})
	name := fetch(t, s, site, "example.org")
	if name != "favicon-example.org.png" {
		t.Fatalf("saved %q, want the declared 32px PNG", name)
	}
}

func TestFallsBackToFaviconICO(t *testing.T) {
	s, site := fakeSite(t, map[string][]byte{
		"/":            []byte(`<html><head><link rel="icon" href="/missing.png"></head></html>`),
		"/favicon.ico": ico,
	})
	if name := fetch(t, s, site, "example.org"); name != "favicon-example.org.ico" {
		t.Fatalf("saved %q", name)
	}
}

func TestRejectsSVGAndPages(t *testing.T) {
	s, site := fakeSite(t, map[string][]byte{
		"/":            []byte(`<html><head><link rel="icon" href="/i.svg"></head></html>`),
		"/i.svg":       []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"/favicon.ico": []byte(`<html>not found</html>`),
	})
	u, _ := url.Parse(site)
	if _, err := s.download(context.Background(), &url.URL{Scheme: "http", Host: u.Host, Path: "/"}, "example.org"); err == nil {
		t.Fatal("saved an SVG or an HTML page as an icon")
	}
	if entries, _ := os.ReadDir(s.dir); len(entries) != 0 {
		t.Fatalf("left files behind: %v", entries)
	}
}

func TestSavedIconsAreFoundAgain(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "favicon-myanimelist.net.ico"), ico, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.File("https://www.myanimelist.net/manga/1"); got != "favicon-myanimelist.net.ico" {
		t.Fatalf("got %q", got)
	}
	if got := s.File("https://anilist.co/manga/1"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestRefusesPrivateAddresses(t *testing.T) {
	s, site := fakeSite(t, map[string][]byte{"/favicon.ico": ico})
	s, _ = New(s.dir, false)
	u, _ := url.Parse(site)
	if _, err := s.download(context.Background(), &url.URL{Scheme: "http", Host: u.Host, Path: "/"}, "example.org"); err == nil {
		t.Fatal("fetched from a loopback address")
	}
}

func TestPublicOnly(t *testing.T) {
	cases := map[string]bool{
		"93.184.215.14:443":          true,
		"[2606:4700::6810:84e5]:443": true,
		"127.0.0.1:80":               false,
		"10.1.2.3:80":                false,
		"100.64.0.1:80":              false,
		"0.1.2.3:80":                 false,
		"169.254.169.254:80":         false,
		"198.18.0.1:80":              false,
		"[::1]:80":                   false,
		"[::ffff:192.168.1.1]:80":    false,
		"[64:ff9b::a00:1]:80":        false,
		"[2002:a00:1::]:80":          false,
		"[fd00::1]:80":               false,
		"[fe80::1]:80":               false,
	}
	for addr, want := range cases {
		if got := publicOnly("tcp", addr, nil) == nil; got != want {
			t.Errorf("%s: allowed=%v want %v", addr, got, want)
		}
	}
}

func TestHost(t *testing.T) {
	cases := map[string]string{
		"https://www.Amazon.ca/dp/x": "amazon.ca",
		"http://myanimelist.net":     "myanimelist.net",
		"https://localhost/":         "",
		"ftp://example.org/":         "",
		"https://../x":               "",
	}
	for in, want := range cases {
		if got := Host(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}
