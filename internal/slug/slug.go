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

// ForWork is a work's slug: its title, or its medium when it has none.
func ForWork(title, kind string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		switch kind {
		case "light_novel":
			title = "light novel"
		case "artbook", "manga":
			title = kind
		default:
			title = "other"
		}
	}
	return orDefault(Make(title), "work")
}

// ForEdition is an edition's slug from its language name and its own name,
// like "english" or "english-3-in-1".
func ForEdition(languageName, name string) string {
	return orDefault(Make(strings.TrimSpace(languageName+" "+name)), "edition")
}

func orDefault(s, def string) string {
	if s == "series" {
		return def
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
