package web

import (
	"database/sql"
	"net/http"
	"strings"

	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/slug"
)

func (s *Server) adminHome(w http.ResponseWriter, r *http.Request) {
	cards, err := s.db.SeriesCards()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	stores, err := s.db.ListStores()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_home", "Admin", map[string]any{"Series": cards, "Stores": stores})
}

type seriesFormData struct {
	ID       int64
	Name     string
	Slug     string
	Notes    string
	Image    string
	New      bool
	Works    []WorkRow
	Merch    []db.Merch
	Wishlist []WishView
	Links    []LinkView
}

func (s *Server) seriesForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "admin_series_form", "New series", seriesFormData{New: true})
}

func (s *Server) seriesSaveNew(w http.ResponseWriter, r *http.Request) {
	if err := parseMaybeMultipart(r); err != nil {
		s.renderErr(w, r, "admin_series_form", "New series", "Reload the page and try again.", seriesFormData{New: true})
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.renderErr(w, r, "admin_series_form", "New series", "Enter a name.", seriesFormData{New: true, Name: name, Slug: r.FormValue("slug"), Notes: r.FormValue("notes")})
		return
	}
	want := strings.TrimSpace(r.FormValue("slug"))
	if want == "" {
		want = slug.Make(name)
	} else {
		want = slug.Make(want)
	}
	exists := func(candidate string) bool {
		ok, err := s.db.SlugExists(candidate, 0)
		return err == nil && ok
	}
	want = slug.Unique(want, exists)
	img, err := s.applyImage(r, sql.NullString{})
	if err != nil {
		s.renderErr(w, r, "admin_series_form", "New series", err.Error(), seriesFormData{New: true, Name: name, Slug: want, Notes: r.FormValue("notes")})
		return
	}
	id, err := s.db.CreateSeries(name, want, strings.TrimSpace(r.FormValue("notes")), img)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/series/"+formatID(id)+"/edit?ok=created", http.StatusSeeOther)
}

func (s *Server) loadSeriesForm(id int64) (seriesFormData, error) {
	ser, err := s.db.GetSeries(id)
	if err != nil {
		return seriesFormData{}, err
	}
	works, err := s.workRows(id)
	if err != nil {
		return seriesFormData{}, err
	}
	merch, err := s.db.ListMerch(id)
	if err != nil {
		return seriesFormData{}, err
	}
	ll, err := s.db.ListLinks("series", id)
	if err != nil {
		return seriesFormData{}, err
	}
	wants, err := s.db.ListOpenWishlistForSeries(id)
	if err != nil {
		return seriesFormData{}, err
	}
	wishItems, err := s.activeWishViews(wants)
	if err != nil {
		return seriesFormData{}, err
	}
	return seriesFormData{
		ID:       ser.ID,
		Name:     ser.Name,
		Slug:     ser.Slug,
		Notes:    ser.Notes,
		Image:    nullImage(ser.ImagePath),
		Works:    works,
		Merch:    merch,
		Wishlist: wishItems,
		Links:    s.linkViews(ll),
	}, nil
}

func (s *Server) seriesEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadSeriesForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_series_form", "Edit "+data.Name, data)
}

func (s *Server) seriesSaveEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		s.badRequest(w, r, "Use photos under 25 MB each.")
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
	name := strings.TrimSpace(r.FormValue("name"))
	data, _ := s.loadSeriesForm(id)
	data.Name = name
	data.Slug = r.FormValue("slug")
	data.Notes = r.FormValue("notes")
	if name == "" {
		s.renderErr(w, r, "admin_series_form", "Edit series", "Enter a name.", data)
		return
	}
	want := strings.TrimSpace(r.FormValue("slug"))
	if want == "" {
		want = slug.Make(name)
	} else {
		want = slug.Make(want)
	}
	exists := func(candidate string) bool {
		ok, err := s.db.SlugExists(candidate, id)
		return err == nil && ok
	}
	want = slug.Unique(want, exists)
	img, err := s.applyImage(r, ser.ImagePath)
	if err != nil {
		s.renderErr(w, r, "admin_series_form", "Edit "+name, err.Error(), data)
		return
	}
	if err := s.db.UpdateSeries(id, name, want, strings.TrimSpace(r.FormValue("notes")), img); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/series/"+formatID(id)+"/edit?ok=updated", http.StatusSeeOther)
}

func (s *Server) seriesDeleteForm(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
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
	s.render(w, r, "admin_confirm_delete", "Delete "+ser.Name, map[string]any{
		"Title":  ser.Name,
		"Action": "/admin/series/" + r.PathValue("id") + "/delete",
		"Cancel": "/admin/series/" + r.PathValue("id") + "/edit",
		"Body":   "This deletes the series, every work, edition and volume, and all merch attached to it.",
	})
}

func (s *Server) seriesDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.db.DeleteSeries(id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin?ok=deleted", http.StatusSeeOther)
}
