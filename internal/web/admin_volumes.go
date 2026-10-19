package web

import (
	"database/sql"
	"net/http"

	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/names"
)

type volumeFormData struct {
	ID           int64
	EditionID    int64
	SeriesID     int64
	VolumeNumber string
	// VolumeTitle is the volume number's own title, shared by its copies.
	VolumeTitle string
	PurchasedAt string
	Store       string
	Currency    string
	MSRP        string
	PriceEx     string
	PriceInc    string
	Notes       string
	Image       string
	Cover       string
	New         bool
	VolumeLinks []LinkView
	CopyLinks   []LinkView
	Trip        itemTripFields
	names.Naming
}

// PublicURL is the volume's page on the public site.
func (d volumeFormData) PublicURL() string {
	return d.VolumeURL(d.VolumeNumber)
}

func (s *Server) volumeNewForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("edition", r.PathValue("id"))
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
	ed, err := s.db.GetEdition(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := volumeFormData{
		New: true, EditionID: ed.ID, SeriesID: ed.SeriesID, Naming: ed.Naming,
		VolumeNumber: r.FormValue("volume_number"), VolumeTitle: r.FormValue("volume_title"), PurchasedAt: r.FormValue("purchased_at"),
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
	existingCover, err := s.db.VolumeCover(ed.ID, number)
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
	if _, err := s.db.CreateVolume(ed.ID, number, volumeSortOrder(number), p); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.SetVolumeCover(ed.ID, number, cover); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.SetVolumeTitle(ed.ID, number, data.VolumeTitle); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/editions/"+formatID(ed.ID)+"/edit?ok=created", http.StatusSeeOther)
}

func (s *Server) loadVolumeForm(id int64) (volumeFormData, error) {
	v, err := s.db.GetVolume(id)
	if err != nil {
		return volumeFormData{}, err
	}
	ed, err := s.db.GetEdition(v.EditionID)
	if err != nil {
		return volumeFormData{}, err
	}
	copyLinks, err := s.db.ListLinks("volume", id)
	if err != nil {
		return volumeFormData{}, err
	}
	volumeLinks, err := s.db.ListLinksKeyed("volume_number", v.EditionID, v.VolumeNumber)
	if err != nil {
		return volumeFormData{}, err
	}
	info, err := s.db.GetVolumeInfo(v.EditionID, v.VolumeNumber)
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
		EditionID:    v.EditionID,
		SeriesID:     ed.SeriesID,
		Naming:       ed.Naming,
		VolumeNumber: v.VolumeNumber,
		VolumeTitle:  info.Title,
		PurchasedAt:  dateVal(v.PurchasedAt),
		Store:        v.Store,
		Currency:     v.Currency,
		MSRP:         moneyVal(v.MSRPCents, v.Currency),
		PriceEx:      moneyVal(v.PriceExTaxCents, v.Currency),
		PriceInc:     moneyVal(v.PriceIncTaxCents, v.Currency),
		Notes:        v.Notes,
		Image:        nullImage(v.ImagePath),
		Cover:        nullImage(info.Cover),
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
	data.VolumeTitle = r.FormValue("volume_title")
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
	existingCover, err := s.db.VolumeCover(v.EditionID, number)
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
	if err := s.db.SetVolumeCover(v.EditionID, number, cover); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.SetVolumeTitle(v.EditionID, number, data.VolumeTitle); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, data.VolumeURL(number)+"?ok=updated", http.StatusSeeOther)
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
		"Title":  data.Volume(data.VolumeNumber, data.VolumeTitle),
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
	http.Redirect(w, r, "/admin/editions/"+formatID(data.EditionID)+"/edit?ok=deleted", http.StatusSeeOther)
}
