package links

import (
	"net/url"
	"strings"
)

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
