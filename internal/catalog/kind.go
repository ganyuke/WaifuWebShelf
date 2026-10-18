package catalog

import "strings"

func KindName(kind string) string {
	switch kind {
	case "manga":
		return "Manga"
	case "light_novel":
		return "Light novel"
	case "artbook":
		return "Artbook"
	default:
		if kind == "" {
			return "Other"
		}
		return strings.ToUpper(kind[:1]) + kind[1:]
	}
}

func LineTitle(kind, label string) string {
	label = strings.TrimSpace(label)
	if label != "" {
		return label
	}
	return KindName(kind)
}

func ValidBookKind(kind string) bool {
	switch kind {
	case "manga", "light_novel", "artbook", "other":
		return true
	default:
		return false
	}
}

// LineFullTitle names a book line with its kind, e.g. "English manga".
// LineTitle alone gives "English", which only reads well next to the kind.
func LineFullTitle(kind, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return KindName(kind)
	}
	if kind == "other" || kind == "" || strings.EqualFold(label, KindName(kind)) {
		return label
	}
	return label + " " + strings.ToLower(KindName(kind))
}
