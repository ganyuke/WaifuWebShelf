package web

import (
	"testing"

	"waifuwebshelf/internal/config"
)

func TestTemplatesParse(t *testing.T) {
	s, err := New(config.Config{DataDir: t.TempDir(), Port: "8080", DisableLogin: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	needed := []string{
		"home", "series_detail", "volume_detail", "merch_list",
		"merch_detail", "stats", "notfound", "admin_home", "admin_series_form",
		"admin_bookline_form", "admin_volume_form", "admin_merch_form", "admin_confirm_delete",
		"login", "header", "footer", "links_list", "links_editor", "purchase_fields", "purchase_facts", "tile", "money_block",
		"wish_money", "wishlist", "wishlist_detail", "admin_wishlist_form",
	}
	for _, name := range needed {
		if s.tmpl.Lookup(name) == nil {
			t.Errorf("missing template %s", name)
		}
	}
}
