package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/db"
)

func TestItemEntryFlow(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, err := New(config.Config{DataDir: dir, AdminUser: "admin", AdminPass: "secret"}, store)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	login := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", strings.NewReader("username=admin&password=secret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(login, req)
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set cookie")
	}
	call := func(method, path string, form url.Values, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if auth {
			for _, c := range cookies {
				r.AddCookie(c)
			}
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), "template error") {
			t.Fatal(w.Body.String())
		}
		return w
	}
	for _, method := range []string{"GET", "POST"} {
		w := call(method, "/admin/items/new", nil, false)
		if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "/login") {
			t.Fatalf("unauthenticated %s: %d", method, w.Code)
		}
	}
	w := call("GET", "/admin/items/new", nil, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Save and add another") {
		t.Fatal(w.Body.String())
	}
	checked := `name="another_copy" type="checkbox" value="1" checked`
	if w := call("GET", "/admin/items/new?n=3&copy=1", nil, true); !strings.Contains(w.Body.String(), checked) {
		t.Fatal("Add copy should open with Add another copy checked")
	}
	if strings.Contains(w.Body.String(), checked) {
		t.Fatal("a plain new item should not have Add another copy checked")
	}
	// No scripts or intermediate writes are required for a new series and format.
	form := url.Values{"type": {"book"}, "series": {"new"}, "series_name": {"New shelf"}, "line": {"new"}, "kind": {"manga"}, "volume_number": {"1"}, "currency": {"CAD"}, "price_inc_tax": {"bad"}, "store": {"Book shop"}, "notes": {"Keep this"}}
	w = call("POST", "/admin/items/new", form, true)
	if !strings.Contains(w.Body.String(), `id="price_inc_tax-error"`) || !strings.Contains(w.Body.String(), "Keep this") {
		t.Fatal(w.Body.String())
	}
	series, _ := store.ListSeries()
	if len(series) != 0 {
		t.Fatal("invalid input created series")
	}
	form.Set("price_inc_tax", "12.34")
	form.Set("purchased_at", "2026-10-06")
	form.Set("action", "again")
	w = call("POST", "/admin/items/new", form, true)
	if w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	again := w.Header().Get("Location")
	w = call("GET", again, nil, true)
	for _, want := range []string{`value="Book shop"`, `value="2026-10-06"`, `value="CAD"`, `name="volume_number" type="text" value=""`, `name="price_inc_tax" type="text" value=""`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("missing %s: %s", want, w.Body.String())
		}
	}
	series, _ = store.ListSeries()
	if len(series) != 1 {
		t.Fatal(series)
	}
	lines, _ := store.ListBookLines(series[0].ID)
	if len(lines) != 1 {
		t.Fatal(lines)
	}
	form.Set("series", formatID(series[0].ID))
	form.Set("line", formatID(lines[0].ID))
	form.Set("action", "save")
	w = call("POST", "/admin/items/new", form, true)
	if !strings.Contains(w.Body.String(), "already owned") {
		t.Fatal(w.Body.String())
	}
	vols, _ := store.ListVolumes(lines[0].ID)
	if len(vols) != 1 {
		t.Fatal("duplicate was saved")
	}
	form.Set("another_copy", "1")
	w = call("POST", "/admin/items/new", form, true)
	if w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	vols, _ = store.ListVolumes(lines[0].ID)
	if len(vols) != 2 {
		t.Fatal("explicit extra copy missing")
	}
	// Cross-series format IDs must not create anything.
	form.Set("series", "new")
	form.Set("series_name", "Wrong parent")
	w = call("POST", "/admin/items/new", form, true)
	if !strings.Contains(w.Body.String(), "belonging to this series") {
		t.Fatal(w.Body.String())
	}
	series, _ = store.ListSeries()
	if len(series) != 1 {
		t.Fatal("bad relationship created series")
	}
	// Format defaults are available on the legacy entry URL and without JS.
	price := int64(1499)
	err = store.UpdateBookLine(lines[0].ID, db.BookLineWrite{Kind: "manga", DefaultCurrency: "CAD", DefaultMSRPCents: &price})
	if err != nil {
		t.Fatal(err)
	}
	w = call("GET", "/admin/book-lines/"+formatID(lines[0].ID)+"/volumes/new?n=2", nil, true)
	if !strings.Contains(w.Body.String(), `value="14.99"`) || !strings.Contains(w.Body.String(), `value="2"`) {
		t.Fatal(w.Body.String())
	}
	form.Set("series", formatID(series[0].ID))
	form.Set("action", "defaults")
	form.Set("currency", "USD")
	form.Set("msrp", "")
	w = call("POST", "/admin/items/new", form, true)
	if !strings.Contains(w.Body.String(), `value="14.99"`) {
		t.Fatal(w.Body.String())
	}
	// Successful writes invalidate anonymous cached collection pages.
	call("GET", "/merch", nil, false)
	merch := url.Values{"type": {"merch"}, "series": {formatID(series[0].ID)}, "name": {"Figure"}, "quantity": {"2"}, "currency": {"USD"}}
	w = call("POST", "/admin/items/new", merch, true)
	if w.Code != http.StatusSeeOther {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/merch", nil, false)
	if !strings.Contains(w.Body.String(), "Figure") {
		t.Fatal("stale collection cache")
	}
	// Reading/cancelling forms does not write records.
	call("GET", "/admin/items/new?series=new", nil, true)
	series, _ = store.ListSeries()
	if len(series) != 1 {
		t.Fatal("GET created series")
	}
	// Wishlist purchases use the same form and atomically mark the wish bought.
	wishID, err := store.CreateWishlist(db.WishlistWrite{Kind: "merch", SeriesID: series[0].ID, Name: "Wanted figure", Quantity: 1, Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	w = call("GET", "/admin/series/"+formatID(series[0].ID)+"/merch/new?wish="+formatID(wishID), nil, true)
	if !strings.Contains(w.Body.String(), `value="Wanted figure"`) || !strings.Contains(w.Body.String(), `name="wishlist_id"`) {
		t.Fatal(w.Body.String())
	}
	merch.Set("wishlist_id", formatID(wishID))
	merch.Set("name", "Wanted figure")
	w = call("POST", "/admin/items/new", merch, true)
	if w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	wish, err := store.GetWishlist(wishID)
	if err != nil || wish.Status != "bought" {
		t.Fatalf("wishlist: %v %v", wish, err)
	}
	// An already-purchased wish cannot create a second item on re-submit.
	w = call("POST", "/admin/items/new", merch, true)
	if !strings.Contains(w.Body.String(), "wishlist item is unavailable") {
		t.Fatal(w.Body.String())
	}
	// Editing preserves the expanded purchase form and returns to the series.
	edit := url.Values{"volume_number": {"1"}, "currency": {"CAD"}, "price_inc_tax": {"bad"}}
	w = call("POST", "/admin/volumes/"+formatID(vols[0].ID)+"/edit", edit, true)
	if !strings.Contains(w.Body.String(), `aria-invalid="true"`) || !strings.Contains(w.Body.String(), `<details open>`) {
		t.Fatal(w.Body.String())
	}
	edit.Set("price_inc_tax", "10.00")
	w = call("POST", "/admin/volumes/"+formatID(vols[0].ID)+"/edit", edit, true)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/series/") {
		t.Fatal(w.Header(), w.Body.String())
	}
}
