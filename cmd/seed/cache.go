package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// cachingTransport keeps every answer from MangaDex and Open Library on disk,
// so running the seed again sends no requests for what it already has.
// Requests it does send are spaced out per host to stay inside each site's
// limits: Open Library allows about 100 cover lookups by ISBN per 5 minutes,
// and MangaDex about 5 requests a second.
type cachingTransport struct {
	dir  string
	next http.RoundTripper

	mu   sync.Mutex
	last map[string]time.Time
}

// cachedAnswer is what is kept for one URL. Only answers that will not change
// on a retry are kept: found, not found, and redirects.
type cachedAnswer struct {
	Status   int    `json:"status"`
	Location string `json:"location,omitempty"`
	Type     string `json:"type,omitempty"`
	Body     []byte `json:"body,omitempty"`
}

var hostGap = map[string]time.Duration{
	"covers.openlibrary.org": 3 * time.Second,
}

const defaultGap = 250 * time.Millisecond

func newCachingTransport() *cachingTransport {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "waifuwebshelf-seed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	log.Printf("Download cache: %s", dir)
	return &cachingTransport{dir: dir, next: http.DefaultTransport, last: map[string]time.Time{}}
}

func (t *cachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	sum := sha256.Sum256([]byte(req.URL.String()))
	path := filepath.Join(t.dir, hex.EncodeToString(sum[:])+".json")
	if b, err := os.ReadFile(path); err == nil {
		var a cachedAnswer
		if json.Unmarshal(b, &a) == nil {
			return a.response(req), nil
		}
	}

	t.wait(req.URL.Host)
	res, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	keep := res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNotFound ||
		(res.StatusCode >= 300 && res.StatusCode < 400 && res.Header.Get("Location") != "")
	if !keep {
		return res, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	res.Body.Close()
	if err != nil {
		return nil, err
	}
	a := cachedAnswer{Status: res.StatusCode, Location: res.Header.Get("Location"), Type: res.Header.Get("Content-Type"), Body: body}
	if b, err := json.Marshal(a); err == nil {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, path)
		}
	}
	return a.response(req), nil
}

// wait holds a request until the gap since the last one to host has passed.
func (t *cachingTransport) wait(host string) {
	gap, ok := hostGap[host]
	if !ok {
		gap = defaultGap
	}
	t.mu.Lock()
	next := t.last[host].Add(gap)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	t.last[host] = next
	t.mu.Unlock()
	time.Sleep(time.Until(next))
}

func (a cachedAnswer) response(req *http.Request) *http.Response {
	h := http.Header{}
	if a.Location != "" {
		h.Set("Location", a.Location)
	}
	if a.Type != "" {
		h.Set("Content-Type", a.Type)
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", a.Status, http.StatusText(a.Status)),
		StatusCode:    a.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(strings.NewReader(string(a.Body))),
		ContentLength: int64(len(a.Body)),
		Request:       req,
	}
}
