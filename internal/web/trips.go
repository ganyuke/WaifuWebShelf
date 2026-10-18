package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
)

// TripTile is one item on a trip page, drawn with the "tile" template.
type TripTile struct {
	Kind, URL, Image, Text, Name, Sub, EditURL string
	// Label names the item on its own, for a tag on the haul photo.
	Label  string
	ID     int64
	Wanted bool
}

type tripLineView struct {
	db.ReceiptLine
	Items []TripTile
}

type lineChoiceView struct {
	ID    int64
	Title string
	Next  string
}

type addBooksForm struct {
	ReceiptLine, BookLine, Numbers string
	// Used when BookLine is "new".
	SeriesName, Kind, Label string
	AnotherCopy             bool
}

type tripPageData struct {
	Trip      db.Trip
	Lines     []tripLineView
	Loose     []TripTile
	ItemCount int
	Subtotal  int64
	Prev      *db.Trip
	Next      *db.Trip
	Tags      []TagView
	Taggable  []TripTile
	Choices   []lineChoiceView
	Series    []string
	Form      addBooksForm
	Errors    map[string]string
}

func tripTitle(t db.Trip) string {
	title := displayDate(t.VisitedOn)
	if t.StoreName != "" {
		title += " · " + t.StoreName
	}
	return title
}

func tripTile(i db.TripItem) TripTile {
	if i.Kind == "merch" {
		return TripTile{Kind: "merch", ID: i.ID, URL: fmt.Sprintf("/merch/%d", i.ID), Image: nullImage(i.Image), Label: i.Name,
			Name: i.Name, Sub: merchSub(i.Name, "", i.Quantity), EditURL: fmt.Sprintf("/admin/merch/%d/edit", i.ID)}
	}
	return TripTile{Kind: "volume", ID: i.ID, URL: "/series/" + i.SeriesSlug + "/" + i.LineSlug + "/" + i.VolumeNumber,
		Image: nullImage(i.Image), Text: "Vol. " + i.VolumeNumber, Name: "Vol. " + i.VolumeNumber,
		Label: i.SeriesName + " " + catalog.LineTitle(i.LineKind, i.LineLabel) + " vol. " + i.VolumeNumber,
		Sub:   i.SeriesName + " · " + catalog.LineTitle(i.LineKind, i.LineLabel), EditURL: fmt.Sprintf("/admin/volumes/%d/edit", i.ID)}
}

type sameTripView struct {
	Trip  db.Trip
	Tiles []TripTile
	Tags  []TagView
	// Tagged is set when the item itself is tagged on the trip's photo.
	Tagged bool
}

// TagView is a tag drawn on a haul photo, placed in whole percent of its size.
// The page's security policy allows no inline styles, so the stylesheet has a class per percent.
type TagView struct {
	ID        int64
	Left, Top int
	Label     string
	URL       string
	Highlight bool
}

// tagViews lists the trip's photo tags. The tag on the item given by kind and id is highlighted.
func (s *Server) tagViews(tripID int64, items []db.TripItem, kind string, id int64) ([]TagView, error) {
	tags, err := s.db.TripTags(tripID)
	if err != nil {
		return nil, err
	}
	tiles := map[string]TripTile{}
	for _, it := range items {
		tiles[it.Kind+":"+formatID(it.ID)] = tripTile(it)
	}
	var out []TagView
	for _, t := range tags {
		tile, ok := tiles[t.Kind+":"+formatID(t.ItemID)]
		if !ok {
			continue
		}
		out = append(out, TagView{ID: t.ID, Left: (t.X + 5) / 10, Top: (t.Y + 5) / 10,
			Label: tile.Label, URL: tile.URL, Highlight: t.Kind == kind && t.ItemID == id})
	}
	return out, nil
}

// sameTrips lists what else was bought on each of the given trips, leaving out the item itself.
func (s *Server) sameTrips(tripIDs []sql.NullInt64, kind string, self map[int64]bool) ([]sameTripView, error) {
	var out []sameTripView
	seen := map[int64]bool{}
	for _, id := range tripIDs {
		if !id.Valid || seen[id.Int64] {
			continue
		}
		seen[id.Int64] = true
		t, err := s.db.GetTrip(id.Int64)
		if err != nil {
			return nil, err
		}
		items, err := s.db.TripItems(id.Int64)
		if err != nil {
			return nil, err
		}
		v := sameTripView{Trip: t}
		if t.PhotoPath.Valid {
			for selfID := range self {
				tags, err := s.tagViews(t.ID, items, kind, selfID)
				if err != nil {
					return nil, err
				}
				for _, tag := range tags {
					if tag.Highlight {
						v.Tags, v.Tagged = tags, true
					}
				}
				if v.Tagged {
					break
				}
			}
		}
		for _, it := range items {
			if it.Kind == kind && self[it.ID] {
				continue
			}
			v.Tiles = append(v.Tiles, tripTile(it))
		}
		out = append(out, v)
	}
	return out, nil
}

// openLine picks the receipt line new items most likely belong on: the first with
// items still to enter whose name says book (or doesn't, for merch), else the first open one.
func openLine(lines []db.ReceiptLine, books bool) string {
	first := ""
	for _, l := range lines {
		if l.Entered >= l.Count {
			continue
		}
		if strings.Contains(strings.ToLower(l.Label), "book") == books {
			return formatID(l.ID)
		}
		if first == "" {
			first = formatID(l.ID)
		}
	}
	return first
}

// nextMissing is the lowest volume number not owned yet, or "" when the line is complete.
func nextMissing(c db.LineChoice) string {
	owned := map[string]bool{}
	for _, n := range c.Owned {
		owned[n] = true
	}
	for n := 1; n < 10000; n++ {
		if c.TotalVolumes.Valid && int64(n) > c.TotalVolumes.Int64 {
			return ""
		}
		if !owned[strconv.Itoa(n)] {
			return strconv.Itoa(n)
		}
	}
	return ""
}

func (s *Server) tripPage(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.renderTrip(w, r, id, addBooksForm{}, nil, "")
}

func (s *Server) renderTrip(w http.ResponseWriter, r *http.Request, id int64, form addBooksForm, errs map[string]string, message string) {
	trip, err := s.db.GetTrip(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	lines, err := s.db.ReceiptLines(id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	items, err := s.db.TripItems(id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d := tripPageData{Trip: trip, Form: form, Errors: errs}
	byLine := map[int64]int{}
	for _, l := range lines {
		byLine[l.ID] = len(d.Lines)
		d.Lines = append(d.Lines, tripLineView{ReceiptLine: l})
		d.Subtotal += l.TotalCents()
	}
	if trip.PhotoPath.Valid {
		if d.Tags, err = s.tagViews(id, items, "", 0); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	for _, it := range items {
		if trip.PhotoPath.Valid {
			d.Taggable = append(d.Taggable, tripTile(it))
		}
		d.ItemCount += it.Quantity
		if i, ok := byLine[it.ReceiptLineID.Int64]; ok && it.ReceiptLineID.Valid {
			d.Lines[i].Items = append(d.Lines[i].Items, tripTile(it))
		} else {
			d.Loose = append(d.Loose, tripTile(it))
		}
	}
	all, err := s.db.ListTrips()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	for i := range all {
		if all[i].ID != id {
			continue
		}
		// Trips are listed newest first, so the earlier trip comes after this one.
		if i+1 < len(all) {
			d.Prev = &all[i+1]
		}
		if i > 0 {
			d.Next = &all[i-1]
		}
	}
	if s.loggedIn(r) {
		choices, err := s.db.LineChoices()
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		for _, c := range choices {
			d.Choices = append(d.Choices, lineChoiceView{ID: c.ID, Title: c.SeriesName + " · " + catalog.LineTitle(c.Kind, c.Label), Next: nextMissing(c)})
		}
		series, err := s.db.ListSeries()
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		for _, se := range series {
			d.Series = append(d.Series, se.Name)
		}
		if d.Form.Kind == "" {
			d.Form.Kind = "manga"
		}
		if d.Form.ReceiptLine == "" && errs == nil {
			d.Form.ReceiptLine = openLine(lines, true)
		}
	}
	if d.Errors == nil {
		d.Errors = map[string]string{}
	}
	title := tripTitle(trip)
	if message != "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderErr(w, r, "trip_detail", title, message, d)
		return
	}
	s.renderPreview(w, r, "trip_detail", title, Preview{Image: nullImage(trip.PhotoPath)}, d)
}

// parseVolumeList reads "14, 15, 17-19" into volume numbers, in the order typed.
func parseVolumeList(s string) ([]string, error) {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if a, b, ok := strings.Cut(part, "-"); ok && a != "" && b != "" {
			from, err1 := strconv.Atoi(a)
			to, err2 := strconv.Atoi(b)
			if err1 != nil || err2 != nil || from < 1 || to < from || to-from > 200 {
				return nil, fmt.Errorf("%q is not a range like 14-16.", part)
			}
			for n := from; n <= to; n++ {
				out = append(out, strconv.Itoa(n))
			}
			continue
		}
		if n := normalizeVolumeNumber(part); n != "" {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("Enter at least one volume number.")
	}
	return out, nil
}

func (s *Server) tripAddBooks(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	form := addBooksForm{ReceiptLine: r.FormValue("receipt_line"), BookLine: r.FormValue("book_line"),
		Numbers: r.FormValue("volumes"), SeriesName: strings.TrimSpace(r.FormValue("series_name")),
		Kind: r.FormValue("kind"), Label: strings.TrimSpace(r.FormValue("label")), AnotherCopy: r.FormValue("another_copy") == "1"}
	errs := map[string]string{}
	var ref db.BookLineRef
	if form.BookLine == "new" {
		ref = db.BookLineRef{SeriesName: form.SeriesName, Kind: form.Kind, Label: form.Label}
		if form.SeriesName == "" {
			errs["series_name"] = "Enter a series name."
		}
		if !catalog.ValidBookKind(form.Kind) {
			errs["kind"] = "Choose a format."
		}
	} else if ref.ID, err = parseID(form.BookLine); err != nil {
		errs["book_line"] = "Choose a series and format."
	} else if _, err := s.db.GetBookLine(ref.ID); err != nil {
		errs["book_line"] = "This format is unavailable."
	}
	var receiptLine int64
	if form.ReceiptLine != "" {
		if receiptLine, err = parseID(form.ReceiptLine); err != nil {
			errs["receipt_line"] = "Choose a receipt line."
		}
	}
	numbers, err := parseVolumeList(form.Numbers)
	if err != nil {
		errs["volumes"] = err.Error()
	}
	if len(errs) > 0 {
		s.renderTrip(w, r, id, form, errs, "Check the highlighted fields.")
		return
	}
	orders := make([]int, len(numbers))
	for i, n := range numbers {
		orders[i] = volumeSortOrder(n)
	}
	err = s.db.AddVolumes(id, receiptLine, ref, numbers, orders, form.AnotherCopy)
	var owned *db.OwnedError
	switch {
	case errors.As(err, &owned):
		errs["volumes"] = owned.Error()
		s.renderTrip(w, r, id, form, errs, "Check the highlighted fields.")
		return
	case errors.Is(err, db.ErrWrongTrip):
		errs["receipt_line"] = err.Error()
		s.renderTrip(w, r, id, form, errs, "Check the highlighted fields.")
		return
	case db.IsNotFound(err):
		s.notFound(w, r)
		return
	case err != nil:
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trips/%d?ok=books-added", id), http.StatusSeeOther)
}

type lineRow struct {
	ID, Label, Price, Count string
}

type tripFormData struct {
	ID                                 int64
	New                                bool
	VisitedOn, StoreChoice             string
	StoreName, StoreCurrency, StoreTax string
	Currency, Paid, Notes              string
	Photo, Receipt                     string
	Lines                              []lineRow
	Stores                             []db.Store
	Labels                             []string
	Errors                             map[string]string
}

// blankLines pads the receipt rows so a few new ones can be typed without scripting.
func blankLines(rows []lineRow, n int) []lineRow {
	for i := 0; i < n; i++ {
		rows = append(rows, lineRow{})
	}
	return rows
}

func (s *Server) renderTripForm(w http.ResponseWriter, r *http.Request, d tripFormData, message string) {
	var err error
	if d.Stores, err = s.db.ListStores(); err != nil {
		s.serverError(w, r, err)
		return
	}
	if d.Labels, err = s.db.ReceiptLabels(); err != nil {
		s.serverError(w, r, err)
		return
	}
	for _, l := range []string{"Book", "Keychain", "Figure"} {
		if !containsFold(d.Labels, l) {
			d.Labels = append(d.Labels, l)
		}
	}
	if d.Errors == nil {
		d.Errors = map[string]string{}
	}
	title := "Add trip"
	if !d.New {
		title = "Edit trip"
	}
	if message != "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	s.renderErr(w, r, "admin_trip_form", title, message, d)
}

func (s *Server) tripNewForm(w http.ResponseWriter, r *http.Request) {
	d := tripFormData{New: true, VisitedOn: time.Now().Format("2006-01-02"), StoreChoice: "new", Lines: blankLines(nil, 3)}
	stores, err := s.db.ListStores()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// The store visited last is the usual one.
	if len(stores) > 0 {
		d.StoreChoice = formatID(stores[0].ID)
		d.Currency = stores[0].Currency
	} else {
		d.Currency = money.Default
		d.StoreCurrency = money.Default
	}
	s.renderTripForm(w, r, d, "")
}

func (s *Server) tripEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	t, err := s.db.GetTrip(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	lines, err := s.db.ReceiptLines(id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d := tripFormData{ID: id, VisitedOn: t.VisitedOn, Currency: t.Currency, Paid: moneyVal(t.PaidCents, t.Currency),
		Notes: t.Notes, Photo: nullImage(t.PhotoPath), Receipt: nullImage(t.ReceiptPath), StoreChoice: "new"}
	if t.StoreID.Valid {
		d.StoreChoice = formatID(t.StoreID.Int64)
	}
	for _, l := range lines {
		d.Lines = append(d.Lines, lineRow{ID: formatID(l.ID), Label: l.Label,
			Price: moneyVal(sql.NullInt64{Int64: l.UnitCents, Valid: true}, t.Currency), Count: strconv.Itoa(l.Count)})
	}
	d.Lines = blankLines(d.Lines, 2)
	s.renderTripForm(w, r, d, "")
}

// parsePercent reads a tax rate like "12" or "9.75" into hundredths of a percent.
func parsePercent(s string) (*int64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if s == "" {
		return nil, nil
	}
	v, err := money.Parse(s, "USD")
	if err != nil || *v > 10000 {
		return nil, errors.New("Enter a percentage like 12 or 9.75.")
	}
	return v, nil
}

func (s *Server) parseTripForm(r *http.Request, d *tripFormData) db.TripWrite {
	f := r.FormValue
	d.VisitedOn, d.StoreChoice = strings.TrimSpace(f("visited_on")), f("store")
	d.StoreName, d.StoreCurrency, d.StoreTax = strings.TrimSpace(f("store_name")), f("store_currency"), f("store_tax")
	d.Currency, d.Paid, d.Notes = strings.TrimSpace(f("currency")), f("paid"), strings.TrimSpace(f("notes"))
	d.Errors = map[string]string{}
	in := db.TripWrite{VisitedOn: d.VisitedOn, Notes: d.Notes}
	if _, err := time.Parse("2006-01-02", d.VisitedOn); err != nil {
		d.Errors["visited_on"] = "Enter the date of the trip."
	}
	if d.StoreChoice == "new" {
		if d.StoreName == "" {
			d.Errors["store_name"] = "Enter the store's name."
		}
		tax, err := parsePercent(d.StoreTax)
		if err != nil {
			d.Errors["store_tax"] = err.Error()
		}
		in.NewStore = &db.StoreWrite{Name: d.StoreName, Currency: money.NormalizeCurrency(d.StoreCurrency), TaxBP: tax}
		if d.Currency == "" {
			d.Currency = in.NewStore.Currency
		}
	} else if id, err := parseID(d.StoreChoice); err != nil {
		d.Errors["store"] = "Choose a store."
	} else if st, err := s.db.GetStore(id); err != nil {
		d.Errors["store"] = "This store is unavailable."
	} else {
		in.StoreID = id
		if d.Currency == "" {
			d.Currency = st.Currency
		}
	}
	in.Currency = money.NormalizeCurrency(d.Currency)
	if paid, err := money.Parse(d.Paid, in.Currency); err != nil {
		d.Errors["paid"] = "Enter an amount like 17.92."
	} else {
		in.PaidCents = paid
	}
	ids, labels, prices, counts := r.Form["line_id"], r.Form["line_label"], r.Form["line_price"], r.Form["line_count"]
	d.Lines = nil
	for i := range labels {
		row := lineRow{Label: strings.TrimSpace(labels[i])}
		if i < len(ids) {
			row.ID = ids[i]
		}
		if i < len(prices) {
			row.Price = strings.TrimSpace(prices[i])
		}
		if i < len(counts) {
			row.Count = strings.TrimSpace(counts[i])
		}
		if row.Label == "" && row.Price == "" && row.Count == "" {
			continue
		}
		d.Lines = append(d.Lines, row)
		w := db.ReceiptLineWrite{Label: row.Label, Count: 1}
		w.ID, _ = parseID(row.ID)
		if row.Label == "" {
			d.Errors["lines"] = "Each receipt line needs a name, like Book or Keychain."
		}
		if p, err := money.Parse(row.Price, in.Currency); err != nil || p == nil {
			d.Errors["lines"] = "Each receipt line needs a price, like 2 or 12.50."
		} else {
			w.UnitCents = *p
		}
		if row.Count != "" {
			if n, err := strconv.Atoi(row.Count); err != nil || n < 1 {
				d.Errors["lines"] = "Counts are whole numbers, 1 or more."
			} else {
				w.Count = n
			}
		}
		in.Lines = append(in.Lines, w)
	}
	return in
}

func (s *Server) tripSaveNew(w http.ResponseWriter, r *http.Request) {
	if err := parseMaybeMultipart(r); err != nil {
		http.Error(w, "Could not read form. Check the file size and try again.", http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	d := tripFormData{New: true}
	in := s.parseTripForm(r, &d)
	if len(d.Errors) > 0 {
		d.Lines = blankLines(d.Lines, 1)
		s.renderTripForm(w, r, d, "Check the highlighted fields. If you selected a photo, select it again.")
		return
	}
	var err error
	if in.Photo, err = s.applyImageField(r, "photo", "remove_photo", sql.NullString{}); err != nil {
		d.Errors["photo"] = err.Error()
	} else if in.Receipt, err = s.applyImageFieldIn(s.receiptsDir, r, "receipt", "remove_receipt", sql.NullString{}); err != nil {
		s.deleteImage(in.Photo)
		d.Errors["receipt"] = err.Error()
	}
	if len(d.Errors) > 0 {
		d.Lines = blankLines(d.Lines, 1)
		s.renderTripForm(w, r, d, "Check the highlighted fields. If you selected a photo, select it again.")
		return
	}
	id, err := s.db.CreateTrip(in)
	if err != nil {
		s.deleteImage(in.Photo)
		deleteFileIn(s.receiptsDir, in.Receipt)
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trips/%d?ok=trip-saved", id), http.StatusSeeOther)
}

func (s *Server) tripSaveEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		http.Error(w, "Could not read form. Check the file size and try again.", http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	t, err := s.db.GetTrip(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d := tripFormData{ID: id, Photo: nullImage(t.PhotoPath), Receipt: nullImage(t.ReceiptPath)}
	in := s.parseTripForm(r, &d)
	if len(d.Errors) > 0 {
		d.Lines = blankLines(d.Lines, 1)
		s.renderTripForm(w, r, d, "Check the highlighted fields. If you selected a photo, select it again.")
		return
	}
	if in.Photo, err = s.applyImageField(r, "photo", "remove_photo", t.PhotoPath); err != nil {
		d.Errors["photo"] = err.Error()
	} else if in.Receipt, err = s.applyImageFieldIn(s.receiptsDir, r, "receipt", "remove_receipt", t.ReceiptPath); err != nil {
		d.Errors["receipt"] = err.Error()
	}
	if len(d.Errors) > 0 {
		d.Lines = blankLines(d.Lines, 1)
		s.renderTripForm(w, r, d, "Check the highlighted fields. If you selected a photo, select it again.")
		return
	}
	if err := s.db.UpdateTrip(id, in); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trips/%d?ok=updated", id), http.StatusSeeOther)
}

func (s *Server) tripDeleteForm(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	t, err := s.db.GetTrip(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_confirm_delete", "Delete trip", map[string]any{
		"Title":  tripTitle(t),
		"Action": fmt.Sprintf("/admin/trips/%d/delete", id),
		"Cancel": fmt.Sprintf("/trips/%d", id),
		"Body":   "Delete this trip and its receipt. The items stay on your shelf.",
	})
}

func (s *Server) tripDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	t, err := s.db.GetTrip(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.DeleteTrip(id); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.deleteImage(t.PhotoPath)
	deleteFileIn(s.receiptsDir, t.ReceiptPath)
	http.Redirect(w, r, "/stats?ok=deleted", http.StatusSeeOther)
}

// itemTakeOff unlinks a volume or merch row from its trip.
func (s *Server) itemTakeOff(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseID(r.PathValue("id"))
		if err != nil {
			s.notFound(w, r)
			return
		}
		if err := s.db.TakeOffTrip(kind, id); err != nil {
			s.serverError(w, r, err)
			return
		}
		path := "volumes"
		if kind == "merch" {
			path = "merch"
		}
		http.Redirect(w, r, fmt.Sprintf("/admin/%s/%d/edit?ok=updated", path, id), http.StatusSeeOther)
	}
}

type storeFormData struct {
	ID                  int64
	Name, Currency, Tax string
	Errors              map[string]string
}

func taxVal(bp sql.NullInt64) string {
	if !bp.Valid {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSuffix(moneyVal(bp, "USD"), "0"), ".0")
}

func (s *Server) storeEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	st, err := s.db.GetStore(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_store_form", "Edit "+st.Name, storeFormData{ID: id, Name: st.Name, Currency: st.Currency, Tax: taxVal(st.TaxBP), Errors: map[string]string{}})
}

func (s *Server) storeSaveEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.db.GetStore(id); db.IsNotFound(err) {
		s.notFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	d := storeFormData{ID: id, Name: strings.TrimSpace(r.FormValue("name")), Currency: r.FormValue("currency"), Tax: r.FormValue("tax"), Errors: map[string]string{}}
	if d.Name == "" {
		d.Errors["name"] = "Enter the store's name."
	}
	tax, err := parsePercent(d.Tax)
	if err != nil {
		d.Errors["tax"] = err.Error()
	}
	if len(d.Errors) == 0 {
		err = s.db.UpdateStore(id, db.StoreWrite{Name: d.Name, Currency: d.Currency, TaxBP: tax})
		if db.IsUniqueErr(err) {
			d.Errors["name"] = "Another store already has this name."
		} else if err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if len(d.Errors) > 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderErr(w, r, "admin_store_form", "Edit store", "Check the highlighted fields.", d)
		return
	}
	http.Redirect(w, r, "/admin?ok=updated", http.StatusSeeOther)
}

func (s *Server) groupTripsForm(w http.ResponseWriter, r *http.Request) {
	items, trips, err := s.db.UngroupedPurchases()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_group_trips", "Group into trips", map[string]int{"Items": items, "Trips": trips})
}

func (s *Server) groupTrips(w http.ResponseWriter, r *http.Request) {
	name := "shelf-before-trips-" + time.Now().Format("20060102-150405") + ".db"
	if err := s.db.Backup(filepath.Join(s.cfg.DataDir, "backups", name)); err != nil {
		s.serverError(w, r, fmt.Errorf("backup before grouping: %w", err))
		return
	}
	if _, _, err := s.db.GroupIntoTrips(); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/stats?ok=grouped", http.StatusSeeOther)
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// itemTripFields is what an item's edit form shows when the item is on a trip.
type itemTripFields struct {
	TripID       int64
	TripTitle    string
	ReceiptLine  string
	ReceiptLines []db.ReceiptLine
	OnLine       bool
}

func (s *Server) loadItemTrip(trip, line sql.NullInt64) (itemTripFields, error) {
	if !trip.Valid {
		return itemTripFields{}, nil
	}
	t, err := s.db.GetTrip(trip.Int64)
	if err != nil {
		return itemTripFields{}, err
	}
	lines, err := s.db.ReceiptLines(t.ID)
	if err != nil {
		return itemTripFields{}, err
	}
	f := itemTripFields{TripID: t.ID, TripTitle: tripTitle(t), ReceiptLines: lines, OnLine: line.Valid}
	if line.Valid {
		f.ReceiptLine = formatID(line.Int64)
	}
	return f, nil
}

// keepTripPurchase puts back what an item on a trip gets from the trip, since its
// edit form leaves those fields out. An item on a receipt line keeps the line's price.
func keepTripPurchase(p *db.Purchase, date sql.NullString, store string, line, ex, inc sql.NullInt64) {
	p.PurchasedAt, p.Store = dateVal(date), store
	if line.Valid {
		p.PriceExTaxCents, p.PriceIncTaxCents = nullPtr(ex), nullPtr(inc)
	}
}

func nullPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

// saveItemReceiptLine applies the receipt line chosen on an item's edit form, if the form had one.
func (s *Server) saveItemReceiptLine(r *http.Request, kind string, id int64, trip sql.NullInt64) error {
	if _, ok := r.Form["receipt_line"]; !ok || !trip.Valid {
		return nil
	}
	lineID, _ := parseID(r.FormValue("receipt_line"))
	return s.db.SetReceiptLine(kind, id, lineID)
}

func (s *Server) tagAdd(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	kind, item, _ := strings.Cut(r.FormValue("item"), ":")
	itemID, err1 := parseID(item)
	x, err2 := strconv.Atoi(r.FormValue("x"))
	y, err3 := strconv.Atoi(r.FormValue("y"))
	if err1 != nil || err2 != nil || err3 != nil {
		s.renderTrip(w, r, id, addBooksForm{}, nil, "Tap the photo where the item is, then choose it.")
		return
	}
	err = s.db.AddTag(id, kind, itemID, x, y)
	if errors.Is(err, db.ErrNotOnTrip) {
		s.renderTrip(w, r, id, addBooksForm{}, nil, err.Error())
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trips/%d", id), http.StatusSeeOther)
}

func (s *Server) tagDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	trip, err := s.db.DeleteTag(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trips/%d", trip), http.StatusSeeOther)
}

// tripReceipt shows a trip's receipt photo to the signed-in owner only.
func (s *Server) tripReceipt(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	t, err := s.db.GetTrip(id)
	if db.IsNotFound(err) || (err == nil && !t.ReceiptPath.Valid) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, filepath.Join(s.receiptsDir, filepath.Base(t.ReceiptPath.String)))
}
