package web

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"waifuwebshelf/internal/db"
)

type merchFormData struct {
	ID          int64
	SeriesID    int64
	SeriesName  string
	SeriesSlug  string
	Name        string
	Kind        string
	Quantity    string
	PurchasedAt string
	Store       string
	Currency    string
	MSRP        string
	PriceEx     string
	PriceInc    string
	Notes       string
	Image       string
	New         bool
	Links       []LinkView
	WishlistID  int64
	Trip        itemTripFields
}

func (s *Server) merchNewForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("type", "merch")
	q.Set("series", r.PathValue("id"))
	r.URL.RawQuery = q.Encode()
	s.itemForm(w, r)
}

func (s *Server) merchSaveNew(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		http.Error(w, "Could not read the form. Photos must be under 25 MB each.", http.StatusBadRequest)
		return
	}
	ser, err := s.db.GetSeries(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := merchFormData{
		New: true, SeriesID: ser.ID, SeriesName: ser.Name, SeriesSlug: ser.Slug,
		Name: r.FormValue("name"), Kind: r.FormValue("kind"), Quantity: r.FormValue("quantity"),
		PurchasedAt: r.FormValue("purchased_at"), Store: r.FormValue("store"), Currency: r.FormValue("currency"),
		MSRP: r.FormValue("msrp"), PriceEx: r.FormValue("price_ex_tax"), PriceInc: r.FormValue("price_inc_tax"),
		Notes: r.FormValue("notes"),
	}
	if wishID, err := parseID(r.FormValue("wishlist_id")); err == nil {
		data.WishlistID = wishID
	}
	if strings.TrimSpace(data.Name) == "" {
		s.renderErr(w, r, "admin_merch_form", "Add merch", "Name is required.", data)
		return
	}
	qty, err := parseQuantity(data.Quantity)
	if err != nil {
		s.renderErr(w, r, "admin_merch_form", "Add merch", err.Error(), data)
		return
	}
	p, err := s.parsePurchase(r)
	if err != nil {
		s.renderErr(w, r, "admin_merch_form", "Add merch", err.Error(), data)
		return
	}
	img, err := s.applyImage(r, sql.NullString{})
	if err != nil {
		s.renderErr(w, r, "admin_merch_form", "Add merch", err.Error(), data)
		return
	}
	p.ImagePath = img
	if _, err := s.db.CreateMerch(ser.ID, strings.TrimSpace(data.Name), strings.TrimSpace(data.Kind), qty, p); err != nil {
		s.serverError(w, r, err)
		return
	}
	if wishID, err := parseID(r.FormValue("wishlist_id")); err == nil && wishID > 0 {
		if wish, err := s.db.GetWishlist(wishID); err == nil && wish.SeriesID == ser.ID && wish.Kind == "merch" && wish.Status == "open" {
			_ = s.db.MarkWishlistBought(wishID)
		}
	}
	http.Redirect(w, r, "/admin/series/"+formatID(ser.ID)+"/edit?ok=created", http.StatusSeeOther)
}

func (s *Server) applyWishToMerchForm(data *merchFormData, wishID int64) {
	wish, err := s.db.GetWishlist(wishID)
	if err != nil || wish.SeriesID != data.SeriesID || wish.Kind != "merch" || wish.Status != "open" {
		return
	}
	data.WishlistID = wish.ID
	data.Name = wish.Name
	data.Kind = wish.MerchKind
	data.Quantity = strconv.Itoa(wish.Quantity)
	data.Currency = wish.Currency
	data.MSRP = moneyVal(wish.MSRPCents, wish.Currency)
	data.Notes = wish.Notes
}

func (s *Server) loadMerchForm(id int64) (merchFormData, error) {
	m, err := s.db.GetMerch(id)
	if err != nil {
		return merchFormData{}, err
	}
	ser, err := s.db.GetSeries(m.SeriesID)
	if err != nil {
		return merchFormData{}, err
	}
	ll, err := s.db.ListLinks("merch", id)
	if err != nil {
		return merchFormData{}, err
	}
	trip, err := s.loadItemTrip(m.TripID, m.ReceiptLineID)
	if err != nil {
		return merchFormData{}, err
	}
	return merchFormData{
		Trip:        trip,
		ID:          m.ID,
		SeriesID:    m.SeriesID,
		SeriesName:  ser.Name,
		SeriesSlug:  ser.Slug,
		Name:        m.Name,
		Kind:        m.Kind,
		Quantity:    strconv.Itoa(m.Quantity),
		PurchasedAt: dateVal(m.PurchasedAt),
		Store:       m.Store,
		Currency:    m.Currency,
		MSRP:        moneyVal(m.MSRPCents, m.Currency),
		PriceEx:     moneyVal(m.PriceExTaxCents, m.Currency),
		PriceInc:    moneyVal(m.PriceIncTaxCents, m.Currency),
		Notes:       m.Notes,
		Image:       nullImage(m.ImagePath),
		Links:       s.linkViews(ll),
	}, nil
}

func (s *Server) merchEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadMerchForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_merch_form", "Edit "+data.Name, data)
}

func (s *Server) merchSaveEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		http.Error(w, "Could not read the form. Photos must be under 25 MB each.", http.StatusBadRequest)
		return
	}
	m, err := s.db.GetMerch(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data, err := s.loadMerchForm(id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data.Name = r.FormValue("name")
	data.Kind = r.FormValue("kind")
	data.Quantity = r.FormValue("quantity")
	data.PurchasedAt = r.FormValue("purchased_at")
	data.Store = r.FormValue("store")
	data.Currency = r.FormValue("currency")
	data.MSRP = r.FormValue("msrp")
	data.PriceEx = r.FormValue("price_ex_tax")
	data.PriceInc = r.FormValue("price_inc_tax")
	data.Notes = r.FormValue("notes")
	if strings.TrimSpace(data.Name) == "" {
		s.renderErr(w, r, "admin_merch_form", "Edit merch", "Name is required.", data)
		return
	}
	qty, err := parseQuantity(data.Quantity)
	if err != nil {
		s.renderErr(w, r, "admin_merch_form", "Edit merch", err.Error(), data)
		return
	}
	p, err := s.parsePurchase(r)
	if err != nil {
		s.renderErr(w, r, "admin_merch_form", "Edit merch", err.Error(), data)
		return
	}
	img, err := s.applyImage(r, m.ImagePath)
	if err != nil {
		s.renderErr(w, r, "admin_merch_form", "Edit merch", err.Error(), data)
		return
	}
	p.ImagePath = img
	if m.TripID.Valid {
		keepTripPurchase(&p, m.PurchasedAt, m.Store, m.ReceiptLineID, m.PriceExTaxCents, m.PriceIncTaxCents)
	}
	if err := s.db.UpdateMerch(id, strings.TrimSpace(data.Name), strings.TrimSpace(data.Kind), qty, p); err != nil {
		s.serverError(w, r, err)
		return
	}
	// Saving the line again also prices a changed quantity at the line's price.
	if err := s.saveItemReceiptLine(r, "merch", id, m.TripID); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/merch/"+formatID(id)+"?ok=updated", http.StatusSeeOther)
}

func (s *Server) merchDeleteForm(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadMerchForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_confirm_delete", "Delete merch", map[string]any{
		"Title":  data.Name,
		"Action": "/admin/merch/" + r.PathValue("id") + "/delete",
		"Cancel": "/admin/merch/" + r.PathValue("id") + "/edit",
		"Body":   "Remove this merch from the shelf.",
	})
}

func (s *Server) merchDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadMerchForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.DeleteMerch(id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/series/"+formatID(data.SeriesID)+"/edit?ok=deleted", http.StatusSeeOther)
}
