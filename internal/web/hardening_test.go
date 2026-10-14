package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/db"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
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
	return srv, ts
}

func postLogin(t *testing.T, ts *httptest.Server, pass string) *http.Response {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	form := url.Values{"username": {"admin"}, "password": {pass}}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestLoginLocksOutAfterRepeatedMisses(t *testing.T) {
	_, ts := newTestServer(t)
	for i := 0; i < loginMaxFails; i++ {
		if res := postLogin(t, ts, "wrong"); res.StatusCode != 200 {
			t.Fatalf("miss %d: status %d", i, res.StatusCode)
		}
	}
	res := postLogin(t, ts, "secret")
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d", res.StatusCode)
	}
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

func TestLoginThrottleExpiresAndClears(t *testing.T) {
	th := newLoginThrottle()
	now := time.Now()
	for i := 0; i < loginMaxFails; i++ {
		th.fail("1.1.1.1", now)
	}
	if th.wait("1.1.1.1", now) <= 0 {
		t.Fatal("should wait")
	}
	if th.wait("2.2.2.2", now) != 0 {
		t.Fatal("other address affected")
	}
	if th.wait("1.1.1.1", now.Add(loginWindow+time.Second)) != 0 {
		t.Fatal("should have expired")
	}
	th.fail("3.3.3.3", now)
	th.clear("3.3.3.3")
	if th.fails["3.3.3.3"] != nil {
		t.Fatal("not cleared")
	}
}

func TestSuccessfulLoginResetsMisses(t *testing.T) {
	_, ts := newTestServer(t)
	for i := 0; i < loginMaxFails-1; i++ {
		postLogin(t, ts, "wrong")
	}
	if res := postLogin(t, ts, "secret"); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("status %d", res.StatusCode)
	}
	for i := 0; i < loginMaxFails-1; i++ {
		if res := postLogin(t, ts, "wrong"); res.StatusCode != 200 {
			t.Fatalf("status %d", res.StatusCode)
		}
	}
}

func TestCrossSitePostRefused(t *testing.T) {
	_, ts := newTestServer(t)
	for name, hdr := range map[string]http.Header{
		"origin":     {"Origin": {"https://evil.example"}},
		"fetch-site": {"Sec-Fetch-Site": {"cross-site"}},
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/login", strings.NewReader("username=admin&password=secret"))
		req.Header = hdr.Clone()
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s: status %d", name, res.StatusCode)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/login", strings.NewReader("username=admin&password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", ts.URL)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("same-origin status %d", res.StatusCode)
	}
}

func uploadRequest(t *testing.T, filename string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("image", filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(body)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := parseMaybeMultipart(req); err != nil {
		t.Fatal(err)
	}
	return req
}

var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

func TestSaveUploadChecksContents(t *testing.T) {
	srv, _ := newTestServer(t)

	name, ok, err := srv.saveUpload(uploadRequest(t, "cover.jpg", pngHead), "image")
	if err != nil || !ok || !strings.HasSuffix(name, ".png") {
		t.Fatalf("png named .jpg: %q %v %v", name, ok, err)
	}

	_, _, err = srv.saveUpload(uploadRequest(t, "cover.png", []byte("<html><script>alert(1)</script></html>")), "image")
	if err == nil {
		t.Fatal("html named .png accepted")
	}
}

func TestSaveUploadRejectsOversize(t *testing.T) {
	srv, _ := newTestServer(t)
	big := append(append([]byte{}, pngHead...), make([]byte, maxUpload)...)
	if _, _, err := srv.saveUpload(uploadRequest(t, "big.png", big), "image"); err == nil {
		t.Fatal("oversize accepted")
	}
	entries, _ := filepath.Glob(filepath.Join(srv.imagesDir, "*"))
	if len(entries) != 0 {
		t.Fatalf("leftover files %v", entries)
	}
}

func TestResponsesCarryBrowserProtections(t *testing.T) {
	_, ts := newTestServer(t)
	for _, path := range []string{"/", "/login", "/static/style.css", "/nope"} {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
			if res.Header.Get(h) == "" {
				t.Errorf("%s: missing %s", path, h)
			}
		}
	}
}
