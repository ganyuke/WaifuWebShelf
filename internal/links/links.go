package links

import (
	"net/url"
	"strings"
)

type Info struct {
	Label string
	URL   string
	Icon  string
}

func Icon(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "generic"
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")

	switch {
	case hostEq(host, "myanimelist.net"):
		return "mal"
	case hostEq(host, "anilist.co"):
		return "anilist"
	case hostEq(host, "myfigurecollection.net"):
		return "mfc"
	case hostEq(host, "anidb.net"):
		return "anidb"
	case hostEq(host, "vndb.org"):
		return "vndb"
	case hostEq(host, "bookwalker.jp"), hostEq(host, "global.bookwalker.jp"):
		return "bookwalker"
	case hostEq(host, "amazon.ca"), hostEq(host, "amazon.com"), hostEq(host, "amazon.co.jp"), hostEq(host, "amazon.co.uk"):
		return "amazon"
	case hostEq(host, "isbnsearch.org"), hostEq(host, "worldcat.org"), strings.Contains(host, "openlibrary.org"):
		return "isbn"
	case hostEq(host, "amiami.com"), hostEq(host, "www.amiami.com"):
		return "shop"
	case hostEq(host, "rightstufanime.com"), hostEq(host, "crunchyroll.com"):
		return "shop"
	default:
		return "generic"
	}
}

func hostEq(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errScheme
	}
	if u.Host == "" {
		return "", errEmptyHost
	}
	return u.String(), nil
}

var (
	errEmptyHost = errString("url is missing a host")
	errScheme    = errString("use a web address starting with http or https")
)

type errString string

func (e errString) Error() string { return string(e) }
