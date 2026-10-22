package coversource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"maps"
	"net/http"
	"net/url"
	"path"
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
// Kobo sometimes files one series under several series pages, each listing
// only some volumes (Rent-A-Girlfriend has rent-a-girlfriend with 1 to 16,
// -2 with 16 to 27, and -3 with 19 to 39). When volumes are missing, the
// page of a found volume next to the gap is opened and its series page read,
// a few times at most.
//
// Series pages also give each book's number in the series ("Book 15 -"). It
// is used for books whose name does not match, like rent-a-girlfirend-15,
// but only when the name holds the same number. Kobo's numbers alone are not
// enough, since side stories get one too (No Game No Life Practical War Game
// is "Book 12").
//
// Some editions cut each volume into parts sold as separate books, like
// Higehiro's KADOKAWA light novel (vol-1-part-1 to vol-1-part-6). Shelves
// count whole volumes, so links to parts get ErrPart, and whole-volume links
// skip part covers because their names do not match.
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
	if koboPart.MatchString(slug) {
		return nil, ErrPart
	}
	cdn := "https://cdn.kobo.com"
	if k.Base != "" {
		cdn = k.Base
	}
	body, err := f.Get(ctx, page.String(), MaxPage, koboHeaders)
	if err != nil {
		return nil, err
	}
	// Covers matched by name win over ones matched by Kobo's book number.
	named, numbered := map[string]Cover{}, map[string]Cover{}
	add := func(to map[string]Cover, covers []Cover) {
		for _, c := range covers {
			if _, ok := to[c.Volume]; !ok {
				to[c.Volume] = c
			}
		}
	}
	add(named, koboCovers(body, slug, cdn))
	read := map[string]bool{}
	readSeries := func(from *url.URL, body []byte) error {
		m := koboSeriesLink.FindSubmatch(body)
		if m == nil {
			return nil
		}
		series, err := from.Parse(html.UnescapeString(string(m[1])))
		if err != nil || read[series.String()] {
			return err
		}
		read[series.String()] = true
		pages, err := koboSeriesPages(ctx, f, series)
		if err != nil && !errors.Is(err, errNoSeriesPage) {
			return err
		}
		for _, p := range pages {
			add(named, koboCovers(p, slug, cdn))
			add(numbered, koboNumbered(p, slug, cdn))
		}
		return nil
	}
	if err := readSeries(page, body); err != nil {
		return nil, err
	}
	all := func() map[string]Cover {
		out := maps.Clone(numbered)
		maps.Copy(out, named)
		return out
	}
	opened := map[string]bool{slug: true}
	for range koboMaxSeriesEntries {
		name, ok := koboNextBook(all(), opened)
		if !ok {
			break
		}
		opened[name] = true
		book := *page
		book.RawQuery, book.Fragment = "", ""
		book.Path = path.Join(path.Dir(page.Path), name)
		body, err := f.Get(ctx, book.String(), MaxPage, koboHeaders)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			continue // the cover's name is not always the page's
		}
		if err := readSeries(&book, body); err != nil {
			return nil, err
		}
	}
	out := slices.Collect(maps.Values(all()))
	slices.SortFunc(out, func(a, b Cover) int {
		x, _ := strconv.Atoi(a.Volume)
		y, _ := strconv.Atoi(b.Volume)
		return x - y
	})
	return out, nil
}

// koboMaxSeriesEntries caps how many found volumes' pages are opened to look
// for more series pages.
const koboMaxSeriesEntries = 3

// koboNextBook picks the book whose page to open next: the found volume
// nearest the first missing one, below it when there is a choice.
func koboNextBook(found map[string]Cover, opened map[string]bool) (string, bool) {
	vols := map[int]string{}
	highest := 0
	for v, c := range found {
		n, _ := strconv.Atoi(v)
		vols[n] = strings.TrimSuffix(path.Base(c.URL), ".jpg")
		highest = max(highest, n)
	}
	gap := 0
	for n := 1; n < highest; n++ {
		if _, ok := vols[n]; !ok {
			gap = n
			break
		}
	}
	if gap == 0 {
		return "", false
	}
	for d := 1; d < highest; d++ {
		for _, n := range []int{gap - d, gap + d} {
			if name, ok := vols[n]; ok && !opened[name] {
				return name, true
			}
		}
	}
	return "", false
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
	koboPart       = regexp.MustCompile(`-vol(?:ume)?-[0-9]+-part-[0-9]+`)
	koboSequence   = regexp.MustCompile(`class="sequenced-name-prefix">\s*Book ([0-9]+) -`)
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

// koboNumbered picks books out of a series page by Kobo's book number, for
// names that do not match slug's. The name must hold the same number.
func koboNumbered(page []byte, slug, cdn string) []Cover {
	if _, _, ok := koboVolume(slug); !ok {
		return nil
	}
	var out []Cover
	for _, item := range bytes.Split(page, []byte(`<li class="item-wrapper book"`))[1:] {
		seq := koboSequence.FindSubmatch(item)
		hit := koboImage.FindSubmatch(item)
		if seq == nil || hit == nil {
			continue
		}
		id, name := string(hit[1]), string(hit[2])
		if _, n, ok := koboVolume(name); !ok || strconv.Itoa(n) != string(seq[1]) {
			continue
		}
		out = append(out, Cover{Volume: string(seq[1]), URL: fmt.Sprintf("%s/book-images/%s/0/0/100/False/%s.jpg", cdn, id, name)})
	}
	return out
}
