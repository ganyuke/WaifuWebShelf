package web

import (
	"database/sql"
	"net/http"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
)

type volumeFormData struct {
	ID           int64
	BookLineID   int64
	SeriesID     int64
	SeriesName   string
	SeriesSlug   string
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
		http.Error(w, "could not read form", http.StatusBadRequest)
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
		LineTitle:    catalog.LineTitle(line.Kind, line.Label),
		VolumeNumber: r.FormValue("volume_number"), PurchasedAt: r.FormValue("purchased_at"),
		Store: r.FormValue("store"), Currency: r.FormValue("currency"),
		MSRP: r.FormValue("msrp"), PriceEx: r.FormValue("price_ex_tax"), PriceInc: r.FormValue("price_inc_tax"),
		Notes: r.FormValue("notes"),
	}
	number := normalizeVolumeNumber(data.VolumeNumber)
	if number == "" {
		s.renderErr(w, r, "admin_volume_form", "Add volume", "Volume number is required.", data)
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
	return volumeFormData{
		ID:           v.ID,
		BookLineID:   v.BookLineID,
		SeriesID:     ser.ID,
		SeriesName:   ser.Name,
		SeriesSlug:   ser.Slug,
		LineTitle:    catalog.LineTitle(line.Kind, line.Label),
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
		VolumeLinks:  toLinkViews(volumeLinks),
		CopyLinks:    toLinkViews(copyLinks),
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
		http.Error(w, "could not read form", http.StatusBadRequest)
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
		s.renderErr(w, r, "admin_volume_form", "Edit volume", "Volume number is required.", data)
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
	if err := s.db.UpdateVolume(id, number, volumeSortOrder(number), p); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.SetVolumeCover(v.BookLineID, number, cover); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/series/"+data.SeriesSlug+"?ok=updated", http.StatusSeeOther)
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
