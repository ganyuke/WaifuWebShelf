package slug

import (
	"regexp"
	"strings"
	"unicode"
)

var dashes = regexp.MustCompile(`-+`)

func Make(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '_' || r == '-' || r == '/' || r == '&':
			b.WriteByte('-')
		}
	}
	out := dashes.ReplaceAllString(b.String(), "-")
	out = strings.Trim(out, "-")
	if out == "" {
		return "series"
	}
	return out
}

func ForBookLine(kind, label string) string {
	title := strings.TrimSpace(label)
	if title == "" {
		switch kind {
		case "light_novel":
			title = "light novel"
		case "artbook":
			title = "artbook"
		case "manga":
			title = "manga"
		default:
			title = "other"
		}
	}
	s := Make(title)
	if s == "series" {
		s = "line"
	}
	return s
}

func Unique(base string, exists func(string) bool) string {
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := base + "-" + itoa(i)
		if !exists(candidate) {
			return candidate
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
