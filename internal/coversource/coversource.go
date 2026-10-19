// Package coversource finds volume covers on store sites.
//
// Each site is a Source in its own file, listed in Sources. A source only
// reads pages. Every request goes through a Fetcher, which keeps to the hosts
// the source names, refuses private and reserved addresses, and caps how big
// and how slow a response may be. To add a site, write a Source and add it to
// Sources.
package coversource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"waifuwebshelf/internal/netguard"
)

// UserAgent names only the tool.
const UserAgent = "WaifuWebShelf-covers"

const (
	// MaxPage caps a store page. Kobo's run about 1.5 MB.
	MaxPage = 8 << 20
	// MaxImage matches the upload limit.
	MaxImage = 25 << 20
)

// Cover is one volume's cover picture.
type Cover struct {
	Volume string `json:"volume"`
	URL    string `json:"url"`
}

// Source reads one store site.
type Source interface {
	// Name is the site's name as the owner knows it, like "Kobo".
	Name() string
	// Hosts lists every host the source fetches from, pages and pictures.
	Hosts() []string
	// Match reports whether a link is a page this source reads.
	Match(u *url.URL) bool
	// Covers lists the covers for the series the page belongs to.
	Covers(ctx context.Context, f *Fetcher, page *url.URL) ([]Cover, error)
}

// Sources are the sites covers can come from.
var Sources = []Source{Kobo{}}

// ErrNoSource means no source reads the link.
var ErrNoSource = errors.New("no cover source for this link")

// Client finds covers with a set of sources.
type Client struct {
	sources []Source
	http    *http.Client
}

// New returns a client for sources. Private and loopback addresses are
// refused unless allowPrivate is set, which only tests need.
func New(sources []Source, allowPrivate bool) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = netguard.PublicOnly
	}
	return &Client{
		sources: sources,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext:         dialer.DialContext,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// Names lists the sites the client reads, for telling the owner which links work.
func (c *Client) Names() []string {
	out := make([]string, len(c.sources))
	for i, s := range c.sources {
		out[i] = s.Name()
	}
	return out
}

// Source returns the source that reads the link and a fetcher limited to it.
func (c *Client) Source(rawURL string) (Source, *Fetcher, *url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, nil, nil, ErrNoSource
	}
	for _, s := range c.sources {
		if s.Match(u) {
			return s, c.fetcher(s.Hosts()), u, nil
		}
	}
	return nil, nil, nil, ErrNoSource
}

// Find lists the covers for the series a page belongs to.
func (c *Client) Find(ctx context.Context, rawURL string) (Source, []Cover, error) {
	s, f, u, err := c.Source(rawURL)
	if err != nil {
		return nil, nil, err
	}
	covers, err := s.Covers(ctx, f, u)
	if err != nil {
		return s, nil, fmt.Errorf("%s: %w", s.Name(), err)
	}
	return s, covers, nil
}

// Download fetches one cover found on the page at link.
func (c *Client) Download(ctx context.Context, link, coverURL string) ([]byte, error) {
	_, f, _, err := c.Source(link)
	if err != nil {
		return nil, err
	}
	return f.Get(ctx, coverURL, MaxImage, nil)
}

func (c *Client) fetcher(hosts []string) *Fetcher {
	client := *c.http
	f := &Fetcher{hosts: hosts}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return f.allowed(req.URL)
	}
	f.client = &client
	return f
}

// Fetcher makes a source's requests, only to the hosts it named.
type Fetcher struct {
	client *http.Client
	hosts  []string
}

func (f *Fetcher) allowed(u *url.URL) error {
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("%s is not a web link", u)
	}
	if !slices.Contains(f.hosts, strings.ToLower(u.Host)) && !slices.Contains(f.hosts, strings.ToLower(u.Hostname())) {
		return fmt.Errorf("%s is not one of this source's sites", u.Host)
	}
	return nil
}

// Get fetches rawURL with the extra headers and returns at most limit bytes.
func (f *Fetcher) Get(ctx context.Context, rawURL string, limit int64, header http.Header) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := f.allowed(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("User-Agent", UserAgent)
	res, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u.Redacted(), res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s is too big", u.Redacted())
	}
	return body, nil
}
