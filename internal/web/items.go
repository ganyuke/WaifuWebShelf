package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
	"waifuwebshelf/internal/names"
)

type itemForm struct {
	WishlistID int64
	volumeFormData
	Type, SeriesChoice, EditionChoice, SeriesNameNew, Name, MerchKind, Quantity string
	// Kind, WorkTitle, Language and EditionName describe a new edition.
	Kind, WorkTitle, Language, EditionName string
	Series                                 []db.Series
	Editions                               []db.Edition
	Errors                                 map[string]string
	AnotherCopy                            bool
	// Trip is set when the item is being added to a trip, which supplies its date, store and currency.
	Trip itemTripFields
}

func (s *Server) itemForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := itemForm{Type: q.Get("type"), SeriesChoice: q.Get("series"), EditionChoice: q.Get("edition"), Kind: "manga", Language: "en", Quantity: "1", Errors: map[string]string{}}
	if d.Type != "merch" {
		d.Type = "book"
	}
	d.New = true
	d.Currency = money.Default
	d.VolumeNumber = q.Get("n")
	// Add copy on a volume page comes with copy=1, since that volume is owned.
	d.AnotherCopy = q.Get("copy") == "1"
	if d.Type == "merch" {
		if id, e := parseID(q.Get("wish")); e == nil {
			var m merchFormData
			m.SeriesID, _ = parseID(d.SeriesChoice)
			s.applyWishToMerchForm(&m, id)
			if m.WishlistID > 0 {
				d.WishlistID = m.WishlistID
				d.Name = m.Name
				d.MerchKind = m.Kind
				d.Quantity = m.Quantity
				d.Currency = m.Currency
				d.MSRP = m.MSRP
				d.Notes = m.Notes
			}
		}
	}
	if id, e := parseID(d.EditionChoice); e == nil {
		if ed, e := s.db.GetEdition(id); e == nil {
			d.SeriesChoice = formatID(ed.SeriesID)
			d.Currency = ed.DefaultCurrency
			d.MSRP = moneyVal(ed.DefaultMSRPCents, d.Currency)
		}
	}
	if id, e := parseID(q.Get("trip")); e == nil {
		if !s.setItemFormTrip(&d, id, q.Get("receipt_line"), !q.Has("receipt_line")) {
			s.notFound(w, r)
			return
		}
	}
	if q.Get("again") == "1" {
		d.PurchasedAt = q.Get("date")
		d.Store = q.Get("store")
		if q.Get("currency") != "" {
			d.Currency = q.Get("currency")
		}
	}
	s.renderItem(w, r, d, "")
}

func (s *Server) renderItem(w http.ResponseWriter, r *http.Request, d itemForm, message string) {
	var err error
	d.Series, err = s.db.ListSeries()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Editions, err = s.db.ListAllEditions()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if id, e := parseID(d.SeriesChoice); e == nil {
		if ser, e := s.db.GetSeries(id); e == nil {
			d.SeriesSlug = ser.Slug
		}
	}
	if message != "" {
		s.renderErr(w, r, "item_form", "Add item", message, d)
	} else {
		s.render(w, r, "item_form", "Add item", d)
	}
}

func (s *Server) itemSave(w http.ResponseWriter, r *http.Request) {
	if err := parseMaybeMultipart(r); err != nil {
		s.badRequest(w, r, "Use photos under 25 MB each.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f := r.FormValue
	d := itemForm{Type: f("type"), SeriesChoice: f("series"), EditionChoice: f("edition"), SeriesNameNew: f("series_name"), Kind: f("kind"), WorkTitle: f("work_title"), Language: f("language"), EditionName: f("edition_name"), Name: f("name"), MerchKind: f("merch_kind"), Quantity: f("quantity"), AnotherCopy: f("another_copy") == "1", Errors: map[string]string{}}
	d.New = true
	d.VolumeNumber = f("volume_number")
	d.PurchasedAt = f("purchased_at")
	d.Store = f("store")
	d.Currency = f("currency")
	d.MSRP = f("msrp")
	d.PriceEx = f("price_ex_tax")
	d.PriceInc = f("price_inc_tax")
	d.Notes = f("notes")
	d.WishlistID, _ = parseID(f("wishlist_id"))
	if id, e := parseID(f("trip_id")); e == nil {
		if !s.setItemFormTrip(&d, id, f("receipt_line"), false) {
			s.notFound(w, r)
			return
		}
	}
	if f("action") == "defaults" {
		if id, e := parseID(d.EditionChoice); e == nil {
			if ed, e := s.db.GetEdition(id); e == nil && formatID(ed.SeriesID) == d.SeriesChoice {
				// On a trip the currency is the trip's, and a list price in another currency doesn't fit.
				if d.Trip.TripID == 0 {
					d.Currency = ed.DefaultCurrency
				}
				if d.Currency == ed.DefaultCurrency {
					d.MSRP = moneyVal(ed.DefaultMSRPCents, d.Currency)
				}
			}
		}
		s.renderItem(w, r, d, "")
		return
	}
	in := db.ItemWrite{
		Merch: d.Type == "merch", SeriesName: strings.TrimSpace(d.SeriesNameNew), Number: normalizeVolumeNumber(d.VolumeNumber),
		Name: strings.TrimSpace(d.Name), MerchKind: strings.TrimSpace(d.MerchKind), AnotherCopy: d.AnotherCopy,
		NewEdition: db.NewEdition{Kind: d.Kind, Title: d.WorkTitle, Language: d.Language, Name: d.EditionName},
	}
	if d.Type != "book" && d.Type != "merch" {
		d.Errors["type"] = "Choose Books or Merch."
	}
	if d.SeriesChoice == "new" {
		if in.SeriesName == "" {
			d.Errors["series_name"] = "Enter a series name."
		}
	} else {
		id, e := parseID(d.SeriesChoice)
		if e != nil {
			d.Errors["series"] = "Choose a series or create one."
		} else if _, e = s.db.GetSeries(id); e != nil {
			d.Errors["series"] = "Series not found. Choose another."
		} else {
			in.SeriesID = id
		}
	}
	if d.WishlistID > 0 {
		wish, e := s.db.GetWishlist(d.WishlistID)
		if e != nil || !in.Merch || wish.Kind != "merch" || wish.SeriesID != in.SeriesID || wish.Status != "open" {
			d.Errors["series"] = "Wishlist item not found in this series."
		} else {
			in.WishlistID = d.WishlistID
		}
	}
	if !in.Merch {
		if d.EditionChoice == "new" {
			if !names.ValidKind(d.Kind) {
				d.Errors["kind"] = "Choose a medium."
			}
			if !names.ValidLanguage(d.Language) {
				d.Errors["language"] = "Choose a language."
			}
		} else {
			id, e := parseID(d.EditionChoice)
			if e != nil {
				d.Errors["edition"] = "Choose an edition or create one."
			} else if ed, e := s.db.GetEdition(id); e != nil || ed.SeriesID != in.SeriesID {
				d.Errors["edition"] = "Choose an edition belonging to this series."
			} else {
				in.EditionID = id
				if d.Currency == "" {
					d.Currency = ed.DefaultCurrency
					r.Form.Set("currency", d.Currency)
				}
				// An absent field means no-JavaScript entry without explicitly supplying a value.
				if _, present := r.Form["msrp"]; !present {
					d.MSRP = moneyVal(ed.DefaultMSRPCents, d.Currency)
					r.Form.Set("msrp", d.MSRP)
				}
			}
		}
		if in.Number == "" {
			d.Errors["volume_number"] = "Enter a volume number."
		}
		in.SortOrder = volumeSortOrder(in.Number)
	} else {
		if in.Name == "" {
			d.Errors["name"] = "Enter an item name."
		}
		q, e := parseQuantity(d.Quantity)
		if e != nil {
			d.Errors["quantity"] = e.Error()
		} else {
			in.Quantity = q
		}
	}
	if d.PurchasedAt != "" {
		if _, e := time.Parse("2006-01-02", d.PurchasedAt); e != nil {
			d.Errors["purchased_at"] = "Use a valid purchase date."
		}
	}
	p, e := s.parsePurchase(r)
	if e != nil {
		key := "msrp"
		if strings.HasPrefix(e.Error(), "Paid without") {
			key = "price_ex_tax"
		}
		if strings.HasPrefix(e.Error(), "Paid with tax") {
			key = "price_inc_tax"
		}
		d.Errors[key] = e.Error()
	}
	in.Purchase = p
	if d.Trip.TripID != 0 {
		in.TripID = d.Trip.TripID
		in.ReceiptLineID, _ = parseID(d.Trip.ReceiptLine)
	}
	fail := func() {
		s.renderItem(w, r, d, "Check the highlighted fields. If you selected a photo, select it again.")
	}
	if len(d.Errors) > 0 {
		fail()
		return
	}
	// Validate before uploads; newly uploaded files are removed if saving fails.
	var uploaded []sql.NullString
	cleanup := func() {
		for _, img := range uploaded {
			s.deleteImage(img)
		}
	}
	for _, field := range []string{"image", "cover"} {
		if field == "cover" && in.Merch {
			continue
		}
		name, ok, e := s.saveUpload(r, field)
		if e != nil {
			cleanup()
			d.Errors[field] = e.Error()
			fail()
			return
		}
		if ok {
			img := sql.NullString{String: name, Valid: true}
			uploaded = append(uploaded, img)
			if field == "image" {
				in.Purchase.ImagePath = img
			} else {
				in.Cover = img
			}
		}
	}
	sid, lid, slug, e := s.db.CreateItem(in)
	if e != nil {
		cleanup()
		if errors.Is(e, db.ErrOwned) {
			d.Errors["another_copy"] = e.Error()
			fail()
		} else if errors.Is(e, db.ErrWrongTrip) {
			d.Errors["receipt_line"] = e.Error()
			fail()
		} else {
			s.serverError(w, r, e)
		}
		return
	}
	code := "book-added"
	if in.Merch {
		code = "merch-added"
	}
	dest := "/series/" + slug + "?ok=" + code
	if d.Trip.TripID != 0 {
		dest = "/trips/" + formatID(d.Trip.TripID) + "?ok=" + code
	}
	if f("action") == "again" {
		q := url.Values{"type": {d.Type}, "series": {strconv.FormatInt(sid, 10)}, "edition": {strconv.FormatInt(lid, 10)}, "again": {"1"}, "date": {d.PurchasedAt}, "store": {d.Store}, "currency": {p.Currency}, "ok": {code}}
		if d.Trip.TripID != 0 {
			q.Set("trip", formatID(d.Trip.TripID))
			q.Set("receipt_line", d.Trip.ReceiptLine)
		}
		dest = "/admin/items/new?" + q.Encode()
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// setItemFormTrip fills in the trip an item is being added to, on the receipt line
// in choice. With pickLine it starts on the first line with items still to enter.
func (s *Server) setItemFormTrip(d *itemForm, tripID int64, choice string, pickLine bool) bool {
	t, err := s.db.GetTrip(tripID)
	if err != nil {
		return false
	}
	lines, err := s.db.ReceiptLines(tripID)
	if err != nil {
		return false
	}
	d.Trip = itemTripFields{TripID: t.ID, TripTitle: tripTitle(t), ReceiptLines: lines, ReceiptLine: choice}
	d.Currency = t.Currency
	if pickLine {
		d.Trip.ReceiptLine = openLine(lines, d.Type != "merch")
	}
	return true
}
