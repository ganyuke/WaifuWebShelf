package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"waifuwebshelf/internal/coversource"
	"waifuwebshelf/internal/db"
)

func TestVolumeFromFileName(t *testing.T) {
	for name, want := range map[string]string{
		"komi-can-t-communicate-vol-14.jpg": "14",
		"k-on-shuffle-vol-2.webp":           "2",
		"Vol 03.png":                        "3",
		"volume_10.5.jpg":                   "10.5",
		"spy-x-family-v7-2023.jpg":          "7",
		"scan 12.jpg":                       "12",
		"cover.jpg":                         "",
	} {
		if got := volumeFromFileName(name); got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
}

// coverTest is a signed-in owner with one empty manga format.
type coverTest struct {
	t      *testing.T
	srv    *Server
	ts     *httptest.Server
	client *http.Client
	line   int64
}

func newCoverTest(t *testing.T) *coverTest {
	t.Helper()
	srv, ts := newTestServer(t)
	ser, err := srv.db.CreateSeries("Komi Can't Communicate", "komi", "", sql.NullString{})
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.db.CreateBookLine(ser, db.BookLineWrite{Kind: "manga", DefaultCurrency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"secret"}})
	if err != nil || res.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: %v %v", err, res)
	}
	res.Body.Close()
	return &coverTest{t, srv, ts, client, line}
}

func (c *coverTest) path(suffix string) string {
	return c.ts.URL + "/admin/book-lines/" + formatID(c.line) + "/covers" + suffix
}

// upload posts files by name with an optional volume number.
func (c *coverTest) upload(number string, asJSON bool, files map[string][]byte) (*http.Response, string) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("volume_number", number)
	for name, body := range files {
		part, _ := mw.CreateFormFile("cover", name)
		part.Write(body)
	}
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, c.path(""), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if asJSON {
		req.Header.Set("Accept", "application/json")
	}
	return c.do(req)
}

func (c *coverTest) post(suffix string, asJSON bool, form url.Values) (*http.Response, string) {
	c.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.path(suffix), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if asJSON {
		req.Header.Set("Accept", "application/json")
	}
	return c.do(req)
}

func (c *coverTest) do(req *http.Request) (*http.Response, string) {
	c.t.Helper()
	res, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res, string(body)
}

func (c *coverTest) covers() map[string]string {
	c.t.Helper()
	m, err := c.srv.db.LineCovers(c.line)
	if err != nil {
		c.t.Fatal(err)
	}
	return m
}

func TestUploadSeveralCovers(t *testing.T) {
	c := newCoverTest(t)
	res, body := c.upload("", false, map[string][]byte{
		"komi-vol-1.png": tinyPNG(),
		"komi-vol-2.png": tinyPNG(),
	})
	if res.StatusCode != http.StatusSeeOther || !strings.HasSuffix(res.Header.Get("Location"), "?ok=covers-saved") {
		t.Fatalf("status %d %s\n%s", res.StatusCode, res.Header.Get("Location"), body)
	}
	got := c.covers()
	if len(got) != 2 || got["1"] == "" || got["2"] == "" {
		t.Fatalf("covers %v", got)
	}

	// A new picture replaces the cover, and the old one goes to backups.
	old := got["1"]
	res, body = c.upload("1", true, map[string][]byte{"anything.png": tinyPNG()})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"saved":1`) {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	if now := c.covers()["1"]; now == old || now == "" {
		t.Fatalf("cover not replaced: %q", now)
	}
	if _, err := os.Stat(filepath.Join(c.srv.imagesDir, old)); !os.IsNotExist(err) {
		t.Errorf("old cover still in images: %v", err)
	}
	backup := filepath.Join(c.srv.cfg.DataDir, "backups", "covers", formatID(c.line), "vol-1-"+old)
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("no backup: %v", err)
	}
}

func TestUploadCoverErrors(t *testing.T) {
	c := newCoverTest(t)
	res, body := c.upload("3", false, map[string][]byte{"a-1.png": tinyPNG(), "a-2.png": tinyPNG()})
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Leave the volume number empty") {
		t.Fatalf("number with several files: %d", res.StatusCode)
	}
	res, body = c.upload("", true, map[string][]byte{"cover.png": tinyPNG(), "vol-4.png": tinyPNG(), "vol-5.png": []byte("not a picture")})
	var got coverResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if got.Saved != 1 || len(got.Errors) != 2 {
		t.Fatalf("got %+v", got)
	}
	if covers := c.covers(); len(covers) != 1 || covers["4"] == "" {
		t.Fatalf("covers %v", covers)
	}
}

// withFakeKobo points the server at a local copy of a Kobo page whose
// covers are all tiny PNGs.
func (c *coverTest) withFakeKobo() string {
	c.t.Helper()
	page, err := os.ReadFile("../coversource/testdata/kobo-volume.html")
	if err != nil {
		c.t.Fatal(err)
	}
	var kobo *httptest.Server
	kobo = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/ebook/"):
			w.Write([]byte(strings.ReplaceAll(string(page), "https://cdn.kobo.com", kobo.URL)))
		case strings.HasPrefix(r.URL.Path, "/book-images/"):
			w.Write(tinyPNG())
		default:
			http.NotFound(w, r)
		}
	}))
	c.t.Cleanup(kobo.Close)
	c.srv.covers = coversource.New([]coversource.Source{coversource.Kobo{Base: kobo.URL}}, true)
	return kobo.URL + "/ww/en/ebook/komi-can-t-communicate-vol-14"
}

func TestGetCoversFromKobo(t *testing.T) {
	c := newCoverTest(t)
	link := c.withFakeKobo()

	// The page's script asks for the list, then for each cover.
	res, body := c.post("/find", true, url.Values{"link": {link}})
	var found coverResult
	if err := json.Unmarshal([]byte(body), &found); err != nil || res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if found.Source != "Kobo" || len(found.Covers) != 4 {
		t.Fatalf("found %+v", found)
	}
	first := found.Covers[0]
	res, body = c.post("/get", true, url.Values{"link": {link}, "volume": {first.Volume}, "url": {first.URL}})
	if res.StatusCode != 200 || !strings.Contains(body, `"saved":1`) {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if got := c.covers(); len(got) != 1 || got["1"] == "" {
		t.Fatalf("covers %v", got)
	}

	// Without the script, one request saves them all.
	res, body = c.post("/find", false, url.Values{"link": {link}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if got := c.covers(); len(got) != 4 || got["14"] == "" {
		t.Fatalf("covers %v", got)
	}
}

func TestGetCoversRefusesOtherSites(t *testing.T) {
	c := newCoverTest(t)
	link := c.withFakeKobo()
	res, body := c.post("/find", true, url.Values{"link": {"https://example.com/ebook/x-vol-1"}})
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Paste a link to a book on Kobo.") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	for _, u := range []string{"https://example.com/cover.jpg", "http://127.0.0.1:1/x.jpg", "file:///etc/passwd"} {
		res, body = c.post("/get", true, url.Values{"link": {link}, "volume": {"1"}, "url": {u}})
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", u, res.StatusCode, body)
		}
	}
	if got := c.covers(); len(got) != 0 {
		t.Fatalf("covers %v", got)
	}
}

func TestCoverRoutesNeedLogin(t *testing.T) {
	c := newCoverTest(t)
	stranger := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, suffix := range []string{"", "/find", "/get"} {
		res, err := stranger.PostForm(c.path(suffix), url.Values{"link": {"https://www.kobo.com/ww/en/ebook/x-vol-1"}})
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/login") {
			t.Errorf("%s: %d %s", suffix, res.StatusCode, res.Header.Get("Location"))
		}
	}
}
