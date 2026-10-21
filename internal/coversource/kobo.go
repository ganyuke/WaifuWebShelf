package coversource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Kobo reads book pages on kobo.com, like /ww/en/ebook/komi-can-t-communicate-vol-14.
//
// A volume page links to its series page, which lists every volume a few
// pages at a time. On the worldwide store (/ww/) the volume page also lists
// most of the series among its recommendations, but regional stores like
// /us/ show only a handful there, so the series pages are read too. Volumes
// are picked by name: the page's own name with its volume number swapped for
// any number. The worldwide store links some series pages that are not there
// (Komi's is 404 on /ww/ and fine on /us/), so then the volume page is used
// alone.
//
// Kobo keeps names unique by adding -1, -2 and so on, unevenly within one
// series (no-game-no-life-vol-7-light-novel-2, -vol-8-light-novel-1,
// -vol-9-light-novel), so a trailing number after the volume is ignored.
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
	pages := [][]byte{body}
	if m := koboSeriesLink.FindSubmatch(body); m != nil {
		series, err := page.Parse(html.UnescapeString(string(m[1])))
		if err != nil {
			return nil, err
		}
		more, err := koboSeriesPages(ctx, f, series)
		if err != nil && !errors.Is(err, errNoSeriesPage) {
			return nil, err
		}
		pages = append(pages, more...)
	}
	cdn := "https://cdn.kobo.com"
	if k.Base != "" {
		cdn = k.Base
	}
	return koboCovers(bytes.Join(pages, []byte("\n")), slug, cdn), nil
}

// koboMaxSeriesPages caps how many series pages are read. Each shows 12 books.
const koboMaxSeriesPages = 20

// errNoSeriesPage means the first page of a series listing could not be read.
var errNoSeriesPage = errors.New("no series page")

// koboSeriesPages fetches every page of a series listing.
func koboSeriesPages(ctx context.Context, f *Fetcher, series *url.URL) ([][]byte, error) {
	var out [][]byte
	last := 1
	for n := 1; n <= last && n <= koboMaxSeriesPages; n++ {
		u := *series
		if n > 1 {
			u.RawQuery = url.Values{"pageNumber": {strconv.Itoa(n)}}.Encode()
		}
		body, err := f.Get(ctx, u.String(), MaxPage, koboHeaders)
		if err != nil {
			if n == 1 && ctx.Err() == nil {
				return nil, fmt.Errorf("%w: %w", errNoSeriesPage, err)
			}
			return nil, err
		}
		out = append(out, body)
		for _, m := range koboPageNumber.FindAllSubmatch(body, -1) {
			if p, err := strconv.Atoi(string(m[1])); err == nil && p > last {
				last = p
			}
		}
	}
	return out, nil
}

var (
	koboImage      = regexp.MustCompile(`book-images/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/[^"'\s\\]*?([A-Za-z0-9._-]+)\.(?:jpg|jpeg|png|webp)`)
	koboSeriesLink = regexp.MustCompile(`href="((?:https?://[^/"]+)?/[a-z]{2}/[a-z]{2}/series/[^"?#]+)"`)
	koboPageNumber = regexp.MustCompile(`/series/[^"?#]+\?pageNumber=([0-9]+)`)
	// koboVolNumber finds the number after vol- or volume-.
	koboVolNumber = regexp.MustCompile(`^(.*-vol(?:ume)?-)([0-9]+)(.*)$`)
	// koboLastNumber is for names without vol-, like k-on-shuffle-3.
	koboLastNumber = regexp.MustCompile(`^(.*[^0-9])([0-9]+)([^0-9]*)$`)
	koboUniqueTail = regexp.MustCompile(`-[0-9]+$`)
)

// koboVolume splits a book's name into its series, the part every volume
// shares, and its volume number.
func koboVolume(name string) (series string, vol int, ok bool) {
	if m := koboVolNumber.FindStringSubmatch(name); m != nil {
		n, err := strconv.Atoi(m[2])
		return m[1] + "#" + koboUniqueTail.ReplaceAllString(m[3], ""), n, err == nil && n > 0
	}
	if m := koboLastNumber.FindStringSubmatch(name); m != nil {
		n, err := strconv.Atoi(m[2])
		return m[1] + "#" + m[3], n, err == nil && n > 0
	}
	return "", 0, false
}

// koboCovers picks the covers of slug's series out of a page, in volume order.
func koboCovers(page []byte, slug, cdn string) []Cover {
	want, _, ok := koboVolume(slug)
	if !ok {
		return nil
	}
	seen := map[int]bool{}
	var out []Cover
	for _, hit := range koboImage.FindAllSubmatch(page, -1) {
		id, name := string(hit[1]), string(hit[2])
		series, n, ok := koboVolume(name)
		if !ok || series != want || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, Cover{Volume: strconv.Itoa(n), URL: fmt.Sprintf("%s/book-images/%s/0/0/100/False/%s.jpg", cdn, id, name)})
	}
	slices.SortFunc(out, func(a, b Cover) int {
		x, _ := strconv.Atoi(a.Volume)
		y, _ := strconv.Atoi(b.Volume)
		return x - y
	})
	return out
}
