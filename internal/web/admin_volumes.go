package web

import (
	"database/sql"
	"net/http"
	"net/url"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
)

type volumeFormData struct {
	ID           int64
	BookLineID   int64
	SeriesID     int64
	SeriesName   string
	SeriesSlug   string
	LineSlug     string
	LineTitle    string
	VolumeNumber string
	PurchasedAt  string
	Store        string
	Currency     string
	MSRP         string
	PriceEx      string
	PriceInc     string
	Notes        string
	Image        string
	Cover        string
	New          bool
	VolumeLinks  []LinkView
	CopyLinks    []LinkView
	Trip         itemTripFields
}

// PageURL is the volume's public page.
func (d volumeFormData) PageURL() string {
	return volumePageURL(d.SeriesSlug, d.LineSlug, d.VolumeNumber)
}

func volumePageURL(seriesSlug, lineSlug, number string) string {
	return "/series/" + seriesSlug + "/" + lineSlug + "/" + url.PathEscape(number)
}

func (s *Server) volumeContext(bookLineID int64) (db.BookLine, db.Series, error) {
	line, err := s.db.GetBookLine(bookLineID)
	if err != nil {
		return line, db.Series{}, err
	}
	ser, err := s.db.GetSeries(line.SeriesID)
	return line, ser, err
}

func (s *Server) volumeNewForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("line", r.PathValue("id"))
	r.URL.RawQuery = q.Encode()
	s.itemForm(w, r)
}

func (s *Server) volumeSaveNew(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		s.badRequest(w, r, "Use photos under 25 MB each.")
		return
	}
	line, ser, err := s.volumeContext(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := volumeFormData{
		New: true, BookLineID: line.ID, SeriesID: ser.ID, SeriesName: ser.Name, SeriesSlug: ser.Slug,
		LineTitle:    catalog.LineFullTitle(line.Kind, line.Label),
		VolumeNumber: r.FormValue("volume_number"), PurchasedAt: r.FormValue("purchased_at"),
		Store: r.FormValue("store"), Currency: r.FormValue("currency"),
		MSRP: r.FormValue("msrp"), PriceEx: r.FormValue("price_ex_tax"), PriceInc: r.FormValue("price_inc_tax"),
		Notes: r.FormValue("notes"),
	}
	number := normalizeVolumeNumber(data.VolumeNumber)
	if number == "" {
		s.renderErr(w, r, "admin_volume_form", "Add volume", "Enter a volume number.", data)
		return
	}
	p, err := s.parsePurchase(r)
	if err != nil {
		s.renderErr(w, r, "admin_volume_form", "Add volume", err.Error(), data)
		return
	}
	existingCover, err := s.db.VolumeCover(line.ID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cover, err := s.applyImageField(r, "cover", "remove_cover", existingCover)
	if err != nil {
		s.renderErr(w, r, "admin_volume_form", "Add volume", err.Error(), data)
		return
	}
	img, err := s.applyImage(r, sql.NullString{})
	if err != nil {
		s.renderErr(w, r, "admin_volume_form", "Add volume", err.Error(), data)
		return
	}
	p.ImagePath = img
	if _, err := s.db.CreateVolume(line.ID, number, volumeSortOrder(number), p); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.SetVolumeCover(line.ID, number, cover); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/book-lines/"+formatID(line.ID)+"/edit?ok=created", http.StatusSeeOther)
}

func (s *Server) loadVolumeForm(id int64) (volumeFormData, error) {
	v, err := s.db.GetVolume(id)
	if err != nil {
		return volumeFormData{}, err
	}
	line, ser, err := s.volumeContext(v.BookLineID)
	if err != nil {
		return volumeFormData{}, err
	}
	copyLinks, err := s.db.ListLinks("volume", id)
	if err != nil {
		return volumeFormData{}, err
	}
	volumeLinks, err := s.db.ListLinksKeyed("volume_number", v.BookLineID, v.VolumeNumber)
	if err != nil {
		return volumeFormData{}, err
	}
	cover, err := s.db.VolumeCover(v.BookLineID, v.VolumeNumber)
	if err != nil {
		return volumeFormData{}, err
	}
	trip, err := s.loadItemTrip(v.TripID, v.ReceiptLineID)
	if err != nil {
		return volumeFormData{}, err
	}
	return volumeFormData{
		Trip:         trip,
		ID:           v.ID,
		BookLineID:   v.BookLineID,
		SeriesID:     ser.ID,
		SeriesName:   ser.Name,
		SeriesSlug:   ser.Slug,
		LineSlug:     line.Slug,
		LineTitle:    catalog.LineFullTitle(line.Kind, line.Label),
		VolumeNumber: v.VolumeNumber,
		PurchasedAt:  dateVal(v.PurchasedAt),
		Store:        v.Store,
		Currency:     v.Currency,
		MSRP:         moneyVal(v.MSRPCents, v.Currency),
		PriceEx:      moneyVal(v.PriceExTaxCents, v.Currency),
		PriceInc:     moneyVal(v.PriceIncTaxCents, v.Currency),
		Notes:        v.Notes,
		Image:        nullImage(v.ImagePath),
		Cover:        nullImage(cover),
		VolumeLinks:  s.linkViews(volumeLinks),
		CopyLinks:    s.linkViews(copyLinks),
	}, nil
}

func (s *Server) volumeEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadVolumeForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_volume_form", "Edit volume "+data.VolumeNumber, data)
}

func (s *Server) volumeSaveEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		s.badRequest(w, r, "Use photos under 25 MB each.")
		return
	}
	v, err := s.db.GetVolume(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data, err := s.loadVolumeForm(id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data.VolumeNumber = r.FormValue("volume_number")
	data.PurchasedAt = r.FormValue("purchased_at")
	data.Store = r.FormValue("store")
	data.Currency = r.FormValue("currency")
	data.MSRP = r.FormValue("msrp")
	data.PriceEx = r.FormValue("price_ex_tax")
	data.PriceInc = r.FormValue("price_inc_tax")
	data.Notes = r.FormValue("notes")
	number := normalizeVolumeNumber(data.VolumeNumber)
	if number == "" {
		s.renderErr(w, r, "admin_volume_form", "Edit volume", "Enter a volume number.", data)
		return
	}
	p, err := s.parsePurchase(r)
	if err != nil {
		s.renderErr(w, r, "admin_volume_form", "Edit volume", err.Error(), data)
		return
	}
	existingCover, err := s.db.VolumeCover(v.BookLineID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cover, err := s.applyImageField(r, "cover", "remove_cover", existingCover)
	if err != nil {
		s.renderErr(w, r, "admin_volume_form", "Edit volume", err.Error(), data)
		return
	}
	img, err := s.applyImage(r, v.ImagePath)
	if err != nil {
		s.renderErr(w, r, "admin_volume_form", "Edit volume", err.Error(), data)
		return
	}
	p.ImagePath = img
	if v.TripID.Valid {
		keepTripPurchase(&p, v.PurchasedAt, v.Store, v.ReceiptLineID, v.PriceExTaxCents, v.PriceIncTaxCents)
	}
	if err := s.db.UpdateVolume(id, number, volumeSortOrder(number), p); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.saveItemReceiptLine(r, "volume", id, v.TripID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.SetVolumeCover(v.BookLineID, number, cover); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, volumePageURL(data.SeriesSlug, data.LineSlug, number)+"?ok=updated", http.StatusSeeOther)
}

func (s *Server) volumeDeleteForm(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadVolumeForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_confirm_delete", "Delete volume", map[string]any{
		"Title":  data.SeriesName + " vol. " + data.VolumeNumber,
		"Action": "/admin/volumes/" + r.PathValue("id") + "/delete",
		"Cancel": "/admin/volumes/" + r.PathValue("id") + "/edit",
		"Body":   "Remove this volume from the shelf.",
	})
}

func (s *Server) volumeDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadVolumeForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.DeleteVolume(id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/book-lines/"+formatID(data.BookLineID)+"/edit?ok=deleted", http.StatusSeeOther)
}
