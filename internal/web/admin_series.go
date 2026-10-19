package web

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
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
	items, trips, err := s.db.UngroupedPurchases()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_home", "Admin", map[string]any{
		"Series": cards, "Stores": stores, "Ungrouped": items, "UngroupedTrips": trips,
	})
}

type seriesFormData struct {
	ID       int64
	Name     string
	Slug     string
	Notes    string
	Image    string
	New      bool
	Lines    []db.BookLine
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
	lines, err := s.db.ListBookLines(id)
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
		Lines:    lines,
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
		"Body":   "This deletes the series, every format and volume, and all merch attached to it.",
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

type bookLineFormData struct {
	ID              int64
	SeriesID        int64
	SeriesName      string
	SeriesSlug      string
	Kind            string
	Label           string
	TotalVolumes    string
	DefaultCurrency string
	DefaultMSRP     string
	New             bool
	Links           []LinkView
	Volumes         []db.Volume
	// SpareCovers are covers stored for volumes that are not owned.
	SpareCovers []SpareCover
}

type SpareCover struct {
	Number string
	Image  string
}

func (s *Server) bookLineNewForm(w http.ResponseWriter, r *http.Request) {
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
	s.render(w, r, "admin_bookline_form", "New format", bookLineFormData{
		New:             true,
		SeriesID:        ser.ID,
		SeriesName:      ser.Name,
		SeriesSlug:      ser.Slug,
		Kind:            "manga",
		DefaultCurrency: money.Default,
	})
}

func (s *Server) bookLineSaveNew(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
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
	data := bookLineFormData{
		New: true, SeriesID: ser.ID, SeriesName: ser.Name, SeriesSlug: ser.Slug,
		Kind: r.FormValue("kind"), Label: r.FormValue("label"), TotalVolumes: r.FormValue("total_volumes"),
		DefaultCurrency: r.FormValue("default_currency"), DefaultMSRP: r.FormValue("default_msrp"),
	}
	in, msg, err := parseBookLineWrite(r)
	if err != nil {
		s.renderErr(w, r, "admin_bookline_form", "New format", msg, data)
		return
	}
	data.Kind = in.Kind
	if !catalog.ValidBookKind(in.Kind) {
		s.renderErr(w, r, "admin_bookline_form", "New format", "Pick a book format.", data)
		return
	}
	lineID, err := s.db.CreateBookLine(ser.ID, in)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/book-lines/"+formatID(lineID)+"/edit?ok=created", http.StatusSeeOther)
}

func (s *Server) bookLineEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadBookLineForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_bookline_form", "Edit format", data)
}

func (s *Server) loadBookLineForm(id int64) (bookLineFormData, error) {
	line, err := s.db.GetBookLine(id)
	if err != nil {
		return bookLineFormData{}, err
	}
	ser, err := s.db.GetSeries(line.SeriesID)
	if err != nil {
		return bookLineFormData{}, err
	}
	ll, err := s.db.ListLinks("book_line", id)
	if err != nil {
		return bookLineFormData{}, err
	}
	vols, err := s.db.ListVolumes(id)
	if err != nil {
		return bookLineFormData{}, err
	}
	covers, err := s.db.LineCovers(id)
	if err != nil {
		return bookLineFormData{}, err
	}
	for _, v := range vols {
		delete(covers, v.VolumeNumber)
	}
	spare := make([]SpareCover, 0, len(covers))
	for number, image := range covers {
		spare = append(spare, SpareCover{Number: number, Image: image})
	}
	sort.Slice(spare, func(i, j int) bool {
		return volumeSortOrder(spare[i].Number) < volumeSortOrder(spare[j].Number)
	})
	return bookLineFormData{
		SpareCovers:     spare,
		ID:              line.ID,
		SeriesID:        ser.ID,
		SeriesName:      ser.Name,
		SeriesSlug:      ser.Slug,
		Kind:            line.Kind,
		Label:           line.Label,
		TotalVolumes:    totalVolumesValue(line.TotalVolumes),
		DefaultCurrency: line.DefaultCurrency,
		DefaultMSRP:     moneyVal(line.DefaultMSRPCents, line.DefaultCurrency),
		Links:           s.linkViews(ll),
		Volumes:         vols,
	}, nil
}

// bookLineCoverSave stores a cover for a volume number, owned or not.
func (s *Server) bookLineCoverSave(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
	data, err := s.loadBookLineForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	number := normalizeVolumeNumber(r.FormValue("volume_number"))
	if number == "" {
		s.renderErr(w, r, "admin_bookline_form", "Edit format", "Enter a volume number.", data)
		return
	}
	existing, err := s.db.VolumeCover(id, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cover, err := s.applyImageField(r, "cover", "", existing)
	if err != nil {
		s.renderErr(w, r, "admin_bookline_form", "Edit format", err.Error(), data)
		return
	}
	if cover == existing {
		s.renderErr(w, r, "admin_bookline_form", "Edit format", "Choose a picture for the cover.", data)
		return
	}
	if err := s.db.SetVolumeCover(id, number, cover); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/book-lines/"+formatID(id)+"/edit?ok=updated", http.StatusSeeOther)
}

func (s *Server) bookLineCoverDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
	number := normalizeVolumeNumber(r.FormValue("volume_number"))
	existing, err := s.db.VolumeCover(id, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.SetVolumeCover(id, number, sql.NullString{}); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.deleteImage(existing)
	http.Redirect(w, r, "/admin/book-lines/"+formatID(id)+"/edit?ok=deleted", http.StatusSeeOther)
}

func (s *Server) bookLineSaveEdit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadBookLineForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data.Kind = r.FormValue("kind")
	data.Label = r.FormValue("label")
	data.TotalVolumes = r.FormValue("total_volumes")
	data.DefaultCurrency = r.FormValue("default_currency")
	data.DefaultMSRP = r.FormValue("default_msrp")
	in, msg, err := parseBookLineWrite(r)
	if err != nil {
		s.renderErr(w, r, "admin_bookline_form", "Edit format", msg, data)
		return
	}
	data.Kind = in.Kind
	if !catalog.ValidBookKind(in.Kind) {
		s.renderErr(w, r, "admin_bookline_form", "Edit format", "Pick a book format.", data)
		return
	}
	if err := s.db.UpdateBookLine(id, in); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/book-lines/"+formatID(id)+"/edit?ok=updated", http.StatusSeeOther)
}

func (s *Server) bookLineDeleteForm(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadBookLineForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_confirm_delete", "Delete format", map[string]any{
		"Title":  catalog.LineFullTitle(data.Kind, data.Label),
		"Action": "/admin/book-lines/" + r.PathValue("id") + "/delete",
		"Cancel": "/admin/book-lines/" + r.PathValue("id") + "/edit",
		"Body":   "This deletes the format and every volume on it.",
	})
}

func (s *Server) bookLineDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	line, err := s.db.GetBookLine(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	ser, err := s.db.GetSeries(line.SeriesID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.DeleteBookLine(id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/series/"+formatID(ser.ID)+"/edit?ok=deleted", http.StatusSeeOther)
}
