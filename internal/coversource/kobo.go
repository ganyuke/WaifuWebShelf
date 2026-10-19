package coversource

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Kobo reads book pages on kobo.com, like /ww/en/ebook/komi-can-t-communicate-vol-14.
//
// A volume page lists the covers of every volume in its series among its
// recommendations, so one page is enough. Volumes are picked by name: the
// page's own name with its last number swapped for any number.
//
// Covers are on cdn.kobo.com as book-images/ID/WIDTH/HEIGHT/QUALITY/GREYSCALE/NAME.EXT.
// WIDTH upscales past the source, and 0/0 returns the stored size. Quality 100
// keeps JPEG re-encoding loss down.
type Kobo struct {
	// Base replaces https://www.kobo.com and https://cdn.kobo.com in tests.
	Base string
}

func (Kobo) Name() string { return "Kobo" }

func (k Kobo) Hosts() []string {
	if k.Base != "" {
		u, _ := url.Parse(k.Base)
		return []string{u.Host}
	}
	return []string{"www.kobo.com", "kobo.com", "cdn.kobo.com"}
}

func (k Kobo) Match(u *url.URL) bool {
	if !slices.Contains(k.Hosts(), strings.ToLower(u.Host)) {
		return false
	}
	_, ok := koboSlug(u)
	return ok
}

// koboSlug is the book's name from a /ebook/NAME or /audiobook/NAME path.
func koboSlug(u *url.URL) (string, bool) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if (parts[i] == "ebook" || parts[i] == "audiobook") && parts[i+1] != "" {
			return parts[i+1], true
		}
	}
	return "", false
}

// koboHeaders are what a browser sends when opening a page. Kobo's
// Cloudflare setup challenges requests without them.
var koboHeaders = http.Header{
	"Accept":         {"text/html,application/xhtml+xml"},
	"Sec-Fetch-Dest": {"document"},
	"Sec-Fetch-Mode": {"navigate"},
	"Sec-Fetch-Site": {"none"},
	"Sec-Fetch-User": {"?1"},
}

func (k Kobo) Covers(ctx context.Context, f *Fetcher, page *url.URL) ([]Cover, error) {
	slug, ok := koboSlug(page)
	if !ok {
		return nil, ErrNoSource
	}
	body, err := f.Get(ctx, page.String(), MaxPage, koboHeaders)
	if err != nil {
		return nil, err
	}
	cdn := "https://cdn.kobo.com"
	if k.Base != "" {
		cdn = k.Base
	}
	return koboCovers(body, slug, cdn), nil
}

var (
	koboImage   = regexp.MustCompile(`book-images/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/[^"'\s\\]*?([A-Za-z0-9._-]+)\.(?:jpg|jpeg|png|webp)`)
	koboVolName = regexp.MustCompile(`^(.*[^0-9])([0-9]+)([^0-9]*)$`)
)

// koboCovers picks the covers of slug's series out of a page, in volume order.
func koboCovers(page []byte, slug, cdn string) []Cover {
	m := koboVolName.FindStringSubmatch(slug)
	if m == nil {
		return nil
	}
	prefix, suffix := m[1], m[3]
	seen := map[string]bool{}
	var out []Cover
	for _, hit := range koboImage.FindAllSubmatch(page, -1) {
		id, name := string(hit[1]), string(hit[2])
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) || len(name) <= len(prefix)+len(suffix) {
			continue
		}
		n, err := strconv.Atoi(name[len(prefix) : len(name)-len(suffix)])
		if err != nil || n < 1 {
			continue
		}
		vol := strconv.Itoa(n)
		if seen[vol] {
			continue
		}
		seen[vol] = true
		out = append(out, Cover{Volume: vol, URL: fmt.Sprintf("%s/book-images/%s/0/0/100/False/%s.jpg", cdn, id, name)})
	}
	slices.SortFunc(out, func(a, b Cover) int {
		x, _ := strconv.Atoi(a.Volume)
		y, _ := strconv.Atoi(b.Volume)
		return x - y
	})
	return out
}
