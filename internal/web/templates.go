package web

import (
	"database/sql"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/links"
	"waifuwebshelf/internal/money"
)

func funcMap() template.FuncMap {
	return template.FuncMap{
		"fieldError": func(message, field string) bool {
			m := strings.ToLower(message)
			switch field {
			case "volume_number":
				return strings.HasPrefix(m, "volume number")
			case "name":
				return strings.HasPrefix(m, "name")
			case "quantity":
				return strings.Contains(m, "quantity")
			case "msrp":
				return strings.HasPrefix(m, "msrp:")
			case "price_ex_tax":
				return strings.HasPrefix(m, "price without tax:")
			case "price_inc_tax":
				return strings.HasPrefix(m, "price with tax:")
			case "image", "cover":
				return strings.Contains(m, "image")
			}
			return false
		},
		"moneyInput": moneyVal,
		"kinds":      func() []string { return []string{"manga", "light_novel", "artbook", "other"} },
		"money":      money.DisplayNull,
		"amount":     money.Display,
		"abs": func(n int64) int64 {
			if n < 0 {
				return -n
			}
			return n
		},
		"date":     displayDate,
		"lineFull": catalog.LineFullTitle,
		"volumesOwned": func(owned int, total *int) string {
			return catalog.VolumesOwned(owned, total)
		},
		"percent": func(owned int, total *int) int {
			return catalog.Percent(owned, total)
		},
		"merchSub":        merchSub,
		"amountDelta":     money.FormatAmountDelta,
		"defaultCurrency": func() string { return money.Default },
		"kindName":        catalog.KindName,
		"lineTitle":       catalog.LineTitle,
		"progress": func(owned int, total *int) string {
			return catalog.ProgressLabel(owned, total)
		},
		"imageURL": func(path string) string {
			if path == "" {
				return ""
			}
			return "/images/" + path
		},
		"active": func(path, prefix string) string {
			if prefix == "/" {
				if path == "/" {
					return "active"
				}
				return ""
			}
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return "active"
			}
			return ""
		},
		"flashText": flashText,
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict: odd number of args")
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				k, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key must be string")
				}
				m[k] = values[i+1]
			}
			return m, nil
		},
	}
}

// merchSub is the small line under a merch name: its kind and count.
// The kind is left out when the name already says it.
func merchSub(name, kind string, quantity int) string {
	var parts []string
	if kind != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(kind)) {
		parts = append(parts, kind)
	}
	if quantity > 1 {
		parts = append(parts, "×"+strconv.Itoa(quantity))
	}
	return strings.Join(parts, " · ")
}

// displayDate turns a stored date like 2026-01-09 into "9 Jan 2026".
func displayDate(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("2 Jan 2006")
}

func flashText(code string) string {
	switch code {
	case "book-added":
		return "Book added to your collection."
	case "merch-added":
		return "Merch added to your collection."
	case "created":
		return "Created."
	case "updated":
		return "Saved."
	case "deleted":
		return "Deleted."
	case "linked":
		return "Link added."
	default:
		return ""
	}
}

type LinkView struct {
	db.Link
	Icon string
}

func toLinkViews(ls []db.Link) []LinkView {
	out := make([]LinkView, len(ls))
	for i, l := range ls {
		out[i] = LinkView{Link: l, Icon: links.Icon(l.URL)}
	}
	return out
}

func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func nullImage(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func dateVal(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}

func moneyVal(ns sql.NullInt64, currency string) string {
	if !ns.Valid {
		return ""
	}
	return strings.TrimPrefix(money.Format(ns.Int64, currency), money.NormalizeCurrency(currency)+" ")
}
