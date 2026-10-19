package coversource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestKoboCovers(t *testing.T) {
	page, err := os.ReadFile("testdata/kobo-volume.html")
	if err != nil {
		t.Fatal(err)
	}
	got := koboCovers(page, "komi-can-t-communicate-vol-14", "https://cdn.kobo.com")
	want := []Cover{
		{"1", "https://cdn.kobo.com/book-images/eae87c75-501e-4bc2-b680-e7d05a072b2a/0/0/100/False/komi-can-t-communicate-vol-1.jpg"},
		{"2", "https://cdn.kobo.com/book-images/9c89684c-2582-4708-b151-0ebec9910a10/0/0/100/False/komi-can-t-communicate-vol-2.jpg"},
		{"10", "https://cdn.kobo.com/book-images/38a0c8f6-7bf9-40d4-aa40-f57c20a73de2/0/0/100/False/komi-can-t-communicate-vol-10.jpg"},
		{"14", "https://cdn.kobo.com/book-images/acb86372-9831-4c82-a6a1-9198f1073bb0/0/0/100/False/komi-can-t-communicate-vol-14.jpg"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d covers, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cover %d: got %v want %v", i, got[i], want[i])
		}
	}
	if got := koboCovers(page, "komi-can-t-communicate", "https://cdn.kobo.com"); len(got) != 0 {
		t.Errorf("page name without a number: got %v", got)
	}
}

func TestKoboMatch(t *testing.T) {
	c := New(Sources, false)
	for link, want := range map[string]bool{
		"https://www.kobo.com/ww/en/ebook/komi-can-t-communicate-vol-14":         true,
		"https://www.kobo.com/us/en/audiobook/komi-can-t-communicate-vol-14?x=1": true,
		"https://kobo.com/ww/en/ebook/k-on-shuffle-vol-3":                        true,
		"https://www.kobo.com/ww/en/series/k-on-shuffle":                         false,
		"https://www.kobo.com/ww/en/ebook/":                                      false,
		"https://evil.example/ww/en/ebook/komi-can-t-communicate-vol-14":         false,
		"ftp://www.kobo.com/ww/en/ebook/komi-can-t-communicate-vol-14":           false,
		"not a link": false,
	} {
		_, _, _, err := c.Source(link)
		if got := err == nil; got != want {
			t.Errorf("%s: matched=%v want %v", link, got, want)
		}
	}
}

// fakeKobo serves the sample page and one cover from a local server.
func fakeKobo(t *testing.T) (*Client, string) {
	t.Helper()
	page, err := os.ReadFile("testdata/kobo-volume.html")
	if err != nil {
		t.Fatal(err)
	}
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/ebook/komi-can-t-communicate-vol-14"):
			if r.Header.Get("Sec-Fetch-Mode") != "navigate" || r.Header.Get("User-Agent") != UserAgent {
				http.Error(w, "challenged", http.StatusForbidden)
				return
			}
			// The page names the real CDN, so point it at this server.
			w.Write([]byte(strings.ReplaceAll(string(page), "https://cdn.kobo.com", ts.URL)))
		case r.URL.Path == "/book-images/eae87c75-501e-4bc2-b680-e7d05a072b2a/0/0/100/False/komi-can-t-communicate-vol-1.jpg":
			w.Write([]byte("cover one"))
		case r.URL.Path == "/elsewhere":
			http.Redirect(w, r, "https://evil.example/x.jpg", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return New([]Source{Kobo{Base: ts.URL}}, true), ts.URL
}

func TestFindAndDownload(t *testing.T) {
	c, base := fakeKobo(t)
	link := base + "/ww/en/ebook/komi-can-t-communicate-vol-14"
	src, covers, err := c.Find(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	if src.Name() != "Kobo" || len(covers) != 4 || covers[0].Volume != "1" {
		t.Fatalf("got %s %v", src.Name(), covers)
	}
	body, err := c.Download(context.Background(), link, covers[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "cover one" {
		t.Fatalf("got %q", body)
	}
}

func TestDownloadStaysOnSourceHosts(t *testing.T) {
	c, base := fakeKobo(t)
	link := base + "/ww/en/ebook/komi-can-t-communicate-vol-14"
	for _, u := range []string{
		"https://evil.example/cover.jpg",
		"file:///etc/passwd",
		base + "/elsewhere",
	} {
		if _, err := c.Download(context.Background(), link, u); err == nil {
			t.Errorf("%s: fetched", u)
		}
	}
	if _, err := c.Download(context.Background(), "https://evil.example/ebook/x-1", base+"/x.jpg"); !errors.Is(err, ErrNoSource) {
		t.Errorf("link from an unknown site: %v", err)
	}
}

func TestRefusesPrivateAddresses(t *testing.T) {
	_, base := fakeKobo(t)
	c := New([]Source{Kobo{Base: base}}, false)
	if _, _, err := c.Find(context.Background(), base+"/ww/en/ebook/komi-can-t-communicate-vol-14"); err == nil {
		t.Fatal("fetched from a loopback address")
	}
}
