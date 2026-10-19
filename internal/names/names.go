// Package names builds the names and addresses shown for works, editions and
// volumes, so every page names a volume the same way.
//
// A series is a franchise. A work is one title in it with a medium, like the
// K-On! Shuffle manga. An edition is one printing of a work with its own
// volume numbers, like the English one or a 3-in-1. A volume number can have
// its own title, like the books in Rascal Does Not Dream.
package names

import (
	"net/url"
	"strings"
)

// Kinds are the media a work can be, in the order forms list them.
var Kinds = []string{"manga", "light_novel", "artbook", "other"}

func ValidKind(kind string) bool {
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func KindName(kind string) string {
	switch kind {
	case "manga":
		return "Manga"
	case "light_novel":
		return "Light novel"
	case "artbook":
		return "Artbook"
	default:
		return "Other"
	}
}

// Language is an edition language, stored by its code.
type Language struct {
	Code string
	Name string
}

// Languages are the edition languages forms offer. An empty code means not set.
var Languages = []Language{
	{"en", "English"},
	{"ja", "Japanese"},
	{"zh", "Chinese"},
	{"ko", "Korean"},
	{"fr", "French"},
	{"de", "German"},
	{"es", "Spanish"},
	{"it", "Italian"},
	{"pt", "Portuguese"},
}

func ValidLanguage(code string) bool {
	return code == "" || LanguageName(code) != ""
}

func LanguageName(code string) string {
	for _, l := range Languages {
		if l.Code == code {
			return l.Name
		}
	}
	return ""
}

// Naming holds what a volume's name and address are built from.
type Naming struct {
	SeriesName  string
	SeriesSlug  string
	WorkTitle   string
	Medium      string
	WorkSlug    string
	Language    string
	EditionName string
	EditionSlug string
}

// Work is the work's own title, or the series name when it has none.
func (n Naming) Work() string {
	if t := strings.TrimSpace(n.WorkTitle); t != "" {
		return t
	}
	return n.SeriesName
}

// WorkHeading names a work among the others in its series: its title, or its
// medium when it has none, like "Manga".
func (n Naming) WorkHeading() string {
	return WorkHeading(n.WorkTitle, n.Medium)
}

func WorkHeading(title, kind string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return KindName(kind)
}

// Edition names an edition among the others of its work, like "English" or
// "English 3-in-1".
func (n Naming) Edition() string {
	return EditionLabel(n.Language, n.EditionName)
}

func EditionLabel(language, name string) string {
	parts := []string{}
	if l := LanguageName(language); l != "" {
		parts = append(parts, l)
	}
	if name = strings.TrimSpace(name); name != "" {
		parts = append(parts, name)
	}
	if len(parts) == 0 {
		return "Edition"
	}
	return strings.Join(parts, " ")
}

// Caption is the line under a volume's name: language and medium, like
// "English manga".
func (n Naming) Caption() string {
	kind := KindName(n.Medium)
	if n.Medium == "other" {
		kind = ""
	}
	lang := LanguageName(n.Language)
	switch {
	case lang != "" && kind != "":
		return lang + " " + strings.ToLower(kind)
	case lang != "":
		return lang
	case kind != "":
		return kind
	}
	return ""
}

// Volume is a volume's name. A volume with its own title goes by that title.
// Otherwise it is the work, the edition's name when the edition has one
// because its numbers differ, and the number: "Fullmetal Alchemist 3-in-1 vol. 3".
func (n Naming) Volume(number, title string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return n.Numbered() + " vol. " + number
}

// Numbered is what comes before "vol. N" in a volume's name: the work, plus
// the edition's name when it has one.
func (n Naming) Numbered() string {
	name := n.Work()
	if e := strings.TrimSpace(n.EditionName); e != "" {
		name += " " + e
	}
	return name
}

// VolumeCaption is the line under Volume. The number moves here when the
// title took its place.
func (n Naming) VolumeCaption(number, title string) string {
	c := n.Caption()
	if strings.TrimSpace(title) == "" {
		return c
	}
	if c == "" {
		return "vol. " + number
	}
	return "vol. " + number + ", " + c
}

// VolumeURL is a volume's public page.
func (n Naming) VolumeURL(number string) string {
	return "/series/" + n.SeriesSlug + "/" + n.WorkSlug + "/" + n.EditionSlug + "/" + url.PathEscape(number)
}

// Anchor is the edition's place on its series page.
func (n Naming) Anchor() string {
	return n.WorkSlug + "-" + n.EditionSlug
}

// EditionURL is the edition's part of its series page.
func (n Naming) EditionURL() string {
	return "/series/" + n.SeriesSlug + "#" + n.Anchor()
}

// Describe names an edition within its series for lists, like "English manga"
// or "K-On! Shuffle, English 3-in-1 manga".
func (n Naming) Describe() string {
	return Describe(n.WorkTitle, n.Medium, n.Language, n.EditionName)
}

func Describe(title, kind, language, editionName string) string {
	desc := strings.TrimSpace(LanguageName(language) + " " + strings.TrimSpace(editionName))
	switch {
	case kind == "other" && desc == "":
		desc = KindName(kind)
	case kind == "other":
	case desc == "":
		desc = KindName(kind)
	default:
		desc += " " + strings.ToLower(KindName(kind))
	}
	if t := strings.TrimSpace(title); t != "" {
		return t + ", " + desc
	}
	return desc
}
