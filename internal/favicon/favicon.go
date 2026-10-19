// Package favicon downloads the icon a website declares for itself and keeps
// it next to the other images, so visitors' browsers never contact the linked
// sites. Each site is fetched once.
package favicon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"waifuwebshelf/internal/netguard"
)

// UserAgent names only the tool.
const UserAgent = "WaifuWebShelf-favicons"

const (
	maxPage = 1 << 20
	maxIcon = 256 << 10
	prefix  = "favicon-"
)

// Only raster images are kept. An SVG served from this site could run script
// if someone opened it directly.
var exts = map[string]string{
	"image/x-icon":             ".ico",
	"image/vnd.microsoft.icon": ".ico",
	"image/png":                ".png",
	"image/gif":                ".gif",
	"image/jpeg":               ".jpg",
	"image/webp":               ".webp",
	"image/bmp":                ".bmp",
}

type Store struct {
	dir    string
	client *http.Client

	mu      sync.Mutex
	files   map[string]string   // host to file name in dir
	pending map[string]struct{} // hosts being fetched or that failed this run
}

// New lists the icons already saved in dir. The client refuses private and
// loopback addresses unless allowPrivate is set, which only tests need.
func New(dir string, allowPrivate bool) (*Store, error) {
	s := &Store{dir: dir, files: map[string]string{}, pending: map[string]struct{}{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		host := strings.TrimSuffix(strings.TrimPrefix(name, prefix), filepath.Ext(name))
		s.files[host] = name
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		dialer.Control = netguard.PublicOnly
	}
	s.client = &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	return s, nil
}

var hostRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// Host is the key an icon is saved under: the lowercase host name without a
// leading "www.", or "" when the URL has none worth fetching.
func Host(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if !hostRE.MatchString(h) || !strings.Contains(h, ".") {
		return ""
	}
	return h
}

// File is the saved icon's file name for the link's site, or "".
func (s *Store) File(rawURL string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[Host(rawURL)]
}

// Fetch downloads the icon for the link's site unless it is saved already or
// was tried this run. It reports whether a new icon was saved.
func (s *Store) Fetch(ctx context.Context, rawURL string) bool {
	host := Host(rawURL)
	if host == "" {
		return false
	}
	s.mu.Lock()
	_, have := s.files[host]
	_, busy := s.pending[host]
	if have || busy {
		s.mu.Unlock()
		return false
	}
	s.pending[host] = struct{}{}
	s.mu.Unlock()

	u, _ := url.Parse(rawURL)
	site := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}
	name, err := s.download(ctx, site, host)
	if err != nil {
		log.Printf("icon for %s: %v", host, err)
		return false
	}
	s.mu.Lock()
	s.files[host] = name
	delete(s.pending, host)
	s.mu.Unlock()
	return true
}

func (s *Store) download(ctx context.Context, site *url.URL, host string) (string, error) {
	var candidates []string
	if page, final, err := s.get(ctx, site.String(), maxPage); err == nil {
		candidates = iconLinks(page, final)
	}
	candidates = append(candidates, site.ResolveReference(&url.URL{Path: "/favicon.ico"}).String())

	var lastErr error
	for _, c := range candidates {
		body, _, err := s.get(ctx, c, maxIcon)
		if err != nil {
			lastErr = err
			continue
		}
		ext, ok := exts[http.DetectContentType(body)]
		if !ok {
			lastErr = fmt.Errorf("%s is not a picture", c)
			continue
		}
		name := prefix + host + ext
		tmp, err := os.CreateTemp(s.dir, ".favicon-*")
		if err != nil {
			return "", err
		}
		_, werr := tmp.Write(body)
		if werr == nil {
			werr = tmp.Chmod(0o644)
		}
		cerr := tmp.Close()
		if werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Rename(tmp.Name(), filepath.Join(s.dir, name))
		}
		if werr != nil {
			_ = os.Remove(tmp.Name())
			return "", werr
		}
		return name, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no icon found")
	}
	return "", lastErr
}

func (s *Store) get(ctx context.Context, rawURL string, limit int64) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(body)) > limit {
		return nil, nil, fmt.Errorf("%s is too big", rawURL)
	}
	return body, resp.Request.URL, nil
}

// iconLinks lists the icons a page declares in its <head>, the one nearest
// 32 pixels first, since links show them at 16 pixels on high-density screens.
// Apple touch icons come last because they are large.
func iconLinks(page []byte, base *url.URL) []string {
	type icon struct {
		href  string
		score int
	}
	var icons []icon
	z := html.NewTokenizer(strings.NewReader(string(page)))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		if t.Data == "body" {
			break
		}
		if t.Data != "link" {
			continue
		}
		var rel, href, sizes, typ string
		for _, a := range t.Attr {
			switch strings.ToLower(a.Key) {
			case "rel":
				rel = strings.ToLower(a.Val)
			case "href":
				href = strings.TrimSpace(a.Val)
			case "sizes":
				sizes = strings.ToLower(a.Val)
			case "type":
				typ = strings.ToLower(a.Val)
			}
		}
		if href == "" || typ == "image/svg+xml" || strings.HasSuffix(strings.ToLower(href), ".svg") {
			continue
		}
		fields := strings.Fields(rel)
		score := -1
		for _, f := range fields {
			switch f {
			case "icon":
				score = 1000 - sizeDistance(sizes)
			case "apple-touch-icon", "apple-touch-icon-precomposed":
				if score < 0 {
					score = 0
				}
			}
		}
		if score < 0 {
			continue
		}
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref)
		if abs.Scheme != "http" && abs.Scheme != "https" {
			continue
		}
		icons = append(icons, icon{abs.String(), score})
	}
	sort.SliceStable(icons, func(a, b int) bool { return icons[a].score > icons[b].score })
	out := make([]string, len(icons))
	for i, ic := range icons {
		out[i] = ic.href
	}
	return out
}

// sizeDistance is how far a sizes attribute is from 32x32, with no size
// counting as close.
func sizeDistance(sizes string) int {
	first, _, _ := strings.Cut(sizes, " ")
	w, _, ok := strings.Cut(first, "x")
	n, err := strconv.Atoi(w)
	if !ok || err != nil {
		return 8
	}
	d := n - 32
	if d < 0 {
		d = -d * 2
	}
	return min(d, 999)
}
