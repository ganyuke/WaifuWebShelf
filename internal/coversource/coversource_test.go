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

func TestKoboCoversIgnoreUniqueNumbers(t *testing.T) {
	page := []byte(`
book-images/11111111-1111-1111-1111-111111111111/1/1/90/False/no-game-no-life-vol-1-light-novel-2.jpg
book-images/22222222-2222-2222-2222-222222222222/1/1/90/False/no-game-no-life-vol-8-light-novel-1.jpg
book-images/33333333-3333-3333-3333-333333333333/1/1/90/False/no-game-no-life-vol-10-light-novel.jpg
book-images/44444444-4444-4444-4444-444444444444/1/1/90/False/no-game-no-life-vol-2-2.jpg
book-images/55555555-5555-5555-5555-555555555555/1/1/90/False/no-game-no-life-please-vol-3.jpg
book-images/66666666-6666-6666-6666-666666666666/1/1/90/False/no-game-no-life-practical-war-game.jpg
book-images/77777777-7777-7777-7777-777777777777/1/1/90/False/k-on-shuffle-3.jpg
book-images/88888888-8888-8888-8888-888888888888/1/1/90/False/k-on-shuffle-1.jpg
`)
	for slug, want := range map[string]string{
		"no-game-no-life-vol-1-light-novel-2": "1 8 10",
		"no-game-no-life-vol-10-light-novel":  "1 8 10",
		"no-game-no-life-vol-1-2":             "2",
		"no-game-no-life-please-vol-1-1":      "3",
		"k-on-shuffle-1":                      "1 3",
	} {
		var vols []string
		for _, c := range koboCovers(page, slug, "https://cdn.kobo.com") {
			vols = append(vols, c.Volume)
		}
		if got := strings.Join(vols, " "); got != want {
			t.Errorf("%s: got volumes %q want %q", slug, got, want)
		}
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
	read := func(name string) string {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	page := read("kobo-volume.html")
	usPage := read("kobo-us-volume.html")
	series := []string{read("kobo-series-1.html"), read("kobo-series-2.html")}
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/ebook/komi-can-t-communicate-vol-14") && r.URL.RawQuery == "with=series":
			p := strings.Replace(page, "<ul class=\"series\">", `<a href="/ww/en/series/komi-can-t-communicate">Komi</a><ul class="series">`, 1)
			w.Write([]byte(strings.ReplaceAll(p, "https://cdn.kobo.com", ts.URL)))
		case strings.HasSuffix(r.URL.Path, "/ebook/komi-can-t-communicate-vol-14"):
			if r.Header.Get("Sec-Fetch-Mode") != "navigate" || r.Header.Get("User-Agent") != UserAgent {
				http.Error(w, "challenged", http.StatusForbidden)
				return
			}
			// The page names the real CDN, so point it at this server.
			w.Write([]byte(strings.ReplaceAll(page, "https://cdn.kobo.com", ts.URL)))
		case r.URL.Path == "/us/en/ebook/no-game-no-life-vol-1-light-novel-2":
			w.Write([]byte(usPage))
		case r.URL.Path == "/us/en/series/no-game-no-life":
			switch r.URL.RawQuery {
			case "":
				w.Write([]byte(series[0]))
			case "pageNumber=2":
				w.Write([]byte(series[1]))
			default:
				http.NotFound(w, r)
			}
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

func TestFindReadsSeriesPages(t *testing.T) {
	c, base := fakeKobo(t)
	_, covers, err := c.Find(context.Background(), base+"/us/en/ebook/no-game-no-life-vol-1-light-novel-2")
	if err != nil {
		t.Fatal(err)
	}
	var vols []string
	for _, c := range covers {
		vols = append(vols, c.Volume)
	}
	if got := strings.Join(vols, " "); got != "1 2 7 8 9 12" {
		t.Fatalf("got volumes %q", got)
	}
}

func TestFindWithoutSeriesPage(t *testing.T) {
	c, base := fakeKobo(t)
	// The fake server has no Komi series page, like the real www store.
	_, covers, err := c.Find(context.Background(), base+"/ww/en/ebook/komi-can-t-communicate-vol-14?with=series")
	if err != nil {
		t.Fatal(err)
	}
	if len(covers) != 4 {
		t.Fatalf("got %v", covers)
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
