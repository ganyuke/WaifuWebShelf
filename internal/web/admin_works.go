package web

import (
	"database/sql"
	"net/http"
	"sort"

	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
	"waifuwebshelf/internal/names"
)

// WorkRow is a work on its series' admin page, with its editions.
type WorkRow struct {
	db.Work
	Heading  string
	Editions []db.Edition
}

func (s *Server) workRows(seriesID int64) ([]WorkRow, error) {
	works, err := s.db.ListWorks(seriesID)
	if err != nil {
		return nil, err
	}
	editions, err := s.db.ListSeriesEditions(seriesID)
	if err != nil {
		return nil, err
	}
	out := make([]WorkRow, len(works))
	for i, wk := range works {
		out[i] = WorkRow{Work: wk, Heading: names.WorkHeading(wk.Title, wk.Kind)}
		for _, ed := range editions {
			if ed.WorkID == wk.ID {
				out[i].Editions = append(out[i].Editions, ed)
			}
		}
	}
	return out, nil
}

type workFormData struct {
	ID         int64
	SeriesID   int64
	SeriesName string
	SeriesSlug string
	Slug       string
	Title      string
	Kind       string
	New        bool
	// Language and EditionName are for the first edition of a new work.
	Language    string
	EditionName string
	Editions    []db.Edition
	Links       []LinkView
}

// Heading names the work on its own pages.
func (d workFormData) Heading() string { return names.WorkHeading(d.Title, d.Kind) }

func (s *Server) workNewForm(w http.ResponseWriter, r *http.Request) {
	ser, ok := s.seriesFromPath(w, r)
	if !ok {
		return
	}
	s.render(w, r, "admin_work_form", "New work", workFormData{
		New: true, SeriesID: ser.ID, SeriesName: ser.Name, SeriesSlug: ser.Slug, Kind: "manga", Language: "en",
	})
}

// workSaveNew makes a work with its first edition, since a work is shown
// through its editions.
func (s *Server) workSaveNew(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
	ser, ok := s.seriesFromPath(w, r)
	if !ok {
		return
	}
	data := workFormData{
		New: true, SeriesID: ser.ID, SeriesName: ser.Name, SeriesSlug: ser.Slug,
		Title: r.FormValue("title"), Kind: r.FormValue("kind"),
		Language: r.FormValue("language"), EditionName: r.FormValue("edition_name"),
	}
	in, msg := parseWorkWrite(r)
	if msg == "" && !names.ValidLanguage(data.Language) {
		msg = "Pick a language."
	}
	if msg != "" {
		s.renderErr(w, r, "admin_work_form", "New work", msg, data)
		return
	}
	workID, err := s.db.CreateWork(ser.ID, in)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	edID, err := s.db.CreateEdition(workID, db.EditionWrite{Language: data.Language, Name: data.EditionName, DefaultCurrency: money.Default})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/editions/"+formatID(edID)+"/edit?ok=created", http.StatusSeeOther)
}

func (s *Server) seriesFromPath(w http.ResponseWriter, r *http.Request) (db.Series, bool) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return db.Series{}, false
	}
	ser, err := s.db.GetSeries(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return db.Series{}, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return db.Series{}, false
	}
	return ser, true
}

func (s *Server) loadWorkForm(id int64) (workFormData, error) {
	wk, err := s.db.GetWork(id)
	if err != nil {
		return workFormData{}, err
	}
	ser, err := s.db.GetSeries(wk.SeriesID)
	if err != nil {
		return workFormData{}, err
	}
	editions, err := s.db.ListEditions(id)
	if err != nil {
		return workFormData{}, err
	}
	ll, err := s.db.ListLinks("work", id)
	if err != nil {
		return workFormData{}, err
	}
	return workFormData{
		ID: wk.ID, SeriesID: ser.ID, SeriesName: ser.Name, SeriesSlug: ser.Slug,
		Slug: wk.Slug, Title: wk.Title, Kind: wk.Kind, Editions: editions, Links: s.linkViews(ll),
	}, nil
}

// workFromPath loads the work form for the {id} in the path, answering the
// request itself when it cannot.
func (s *Server) workFromPath(w http.ResponseWriter, r *http.Request) (workFormData, bool) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return workFormData{}, false
	}
	data, err := s.loadWorkForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return workFormData{}, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return workFormData{}, false
	}
	return data, true
}

func (s *Server) workEdit(w http.ResponseWriter, r *http.Request) {
	data, ok := s.workFromPath(w, r)
	if !ok {
		return
	}
	s.render(w, r, "admin_work_form", "Edit "+data.Heading(), data)
}

func (s *Server) workSaveEdit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
	data, ok := s.workFromPath(w, r)
	if !ok {
		return
	}
	data.Title, data.Kind = r.FormValue("title"), r.FormValue("kind")
	in, msg := parseWorkWrite(r)
	if msg != "" {
		s.renderErr(w, r, "admin_work_form", "Edit work", msg, data)
		return
	}
	if err := s.db.UpdateWork(data.ID, in); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/works/"+formatID(data.ID)+"/edit?ok=updated", http.StatusSeeOther)
}

func (s *Server) workDeleteForm(w http.ResponseWriter, r *http.Request) {
	data, ok := s.workFromPath(w, r)
	if !ok {
		return
	}
	s.render(w, r, "admin_confirm_delete", "Delete work", map[string]any{
		"Title":  data.Heading(),
		"Action": "/admin/works/" + formatID(data.ID) + "/delete",
		"Cancel": "/admin/works/" + formatID(data.ID) + "/edit",
		"Body":   "This deletes the work, every edition of it, and every volume in them.",
	})
}

func (s *Server) workDelete(w http.ResponseWriter, r *http.Request) {
	data, ok := s.workFromPath(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteWork(data.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/series/"+formatID(data.SeriesID)+"/edit?ok=deleted", http.StatusSeeOther)
}

type editionFormData struct {
	ID              int64
	WorkID          int64
	SeriesID        int64
	SeriesName      string
	WorkHeading     string
	Language        string
	EditionName     string
	TotalVolumes    string
	DefaultCurrency string
	DefaultMSRP     string
	New             bool
	Links           []LinkView
	Volumes         []db.Volume
	// SpareCovers are covers stored for volumes that are not owned.
	SpareCovers []SpareCover
	// CoverSites names the sites covers can be fetched from.
	CoverSites string
	names.Naming
}

type SpareCover struct {
	Number string
	Image  string
}

func (s *Server) editionNewForm(w http.ResponseWriter, r *http.Request) {
	work, ok := s.workFromPath(w, r)
	if !ok {
		return
	}
	s.render(w, r, "admin_edition_form", "New edition", editionFormData{
		New: true, WorkID: work.ID, SeriesID: work.SeriesID, SeriesName: work.SeriesName,
		WorkHeading: work.Heading(), Language: "en", DefaultCurrency: money.Default,
	})
}

func (s *Server) editionSaveNew(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
	work, ok := s.workFromPath(w, r)
	if !ok {
		return
	}
	data := editionFormData{
		New: true, WorkID: work.ID, SeriesID: work.SeriesID, SeriesName: work.SeriesName, WorkHeading: work.Heading(),
	}
	data.readForm(r)
	in, msg := parseEditionWrite(r)
	if msg != "" {
		s.renderErr(w, r, "admin_edition_form", "New edition", msg, data)
		return
	}
	id, err := s.db.CreateEdition(work.ID, in)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/editions/"+formatID(id)+"/edit?ok=created", http.StatusSeeOther)
}

func (d *editionFormData) readForm(r *http.Request) {
	d.Language = r.FormValue("language")
	d.EditionName = r.FormValue("edition_name")
	d.TotalVolumes = r.FormValue("total_volumes")
	d.DefaultCurrency = r.FormValue("default_currency")
	d.DefaultMSRP = r.FormValue("default_msrp")
}

func (s *Server) loadEditionForm(id int64) (editionFormData, error) {
	ed, err := s.db.GetEdition(id)
	if err != nil {
		return editionFormData{}, err
	}
	ll, err := s.db.ListLinks("edition", id)
	if err != nil {
		return editionFormData{}, err
	}
	vols, err := s.db.ListVolumes(id)
	if err != nil {
		return editionFormData{}, err
	}
	covers, err := s.db.EditionCovers(id)
	if err != nil {
		return editionFormData{}, err
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
	return editionFormData{
		ID:              ed.ID,
		WorkID:          ed.WorkID,
		SeriesID:        ed.SeriesID,
		SeriesName:      ed.SeriesName,
		WorkHeading:     ed.WorkHeading(),
		Language:        ed.Language,
		EditionName:     ed.EditionName,
		TotalVolumes:    totalVolumesValue(ed.TotalVolumes),
		DefaultCurrency: ed.DefaultCurrency,
		DefaultMSRP:     moneyVal(ed.DefaultMSRPCents, ed.DefaultCurrency),
		Links:           s.linkViews(ll),
		Volumes:         vols,
		SpareCovers:     spare,
		CoverSites:      joinOr(s.covers.Names()),
		Naming:          ed.Naming,
	}, nil
}

// editionFromPath loads the edition form for the {id} in the path, answering
// the request itself when it cannot.
func (s *Server) editionFromPath(w http.ResponseWriter, r *http.Request) (editionFormData, bool) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return editionFormData{}, false
	}
	data, err := s.loadEditionForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return editionFormData{}, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return editionFormData{}, false
	}
	return data, true
}

func (s *Server) editionEdit(w http.ResponseWriter, r *http.Request) {
	data, ok := s.editionFromPath(w, r)
	if !ok {
		return
	}
	s.render(w, r, "admin_edition_form", "Edit edition", data)
}

func (s *Server) editionSaveEdit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
	data, ok := s.editionFromPath(w, r)
	if !ok {
		return
	}
	data.readForm(r)
	in, msg := parseEditionWrite(r)
	if msg != "" {
		s.renderErr(w, r, "admin_edition_form", "Edit edition", msg, data)
		return
	}
	if err := s.db.UpdateEdition(data.ID, in); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/editions/"+formatID(data.ID)+"/edit?ok=updated", http.StatusSeeOther)
}

func (s *Server) editionDeleteForm(w http.ResponseWriter, r *http.Request) {
	data, ok := s.editionFromPath(w, r)
	if !ok {
		return
	}
	s.render(w, r, "admin_confirm_delete", "Delete edition", map[string]any{
		"Title":  data.WorkHeading + ", " + data.Edition(),
		"Action": "/admin/editions/" + formatID(data.ID) + "/delete",
		"Cancel": "/admin/editions/" + formatID(data.ID) + "/edit",
		"Body":   "This deletes the edition and every volume in it.",
	})
}

func (s *Server) editionDelete(w http.ResponseWriter, r *http.Request) {
	data, ok := s.editionFromPath(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteEdition(data.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/works/"+formatID(data.WorkID)+"/edit?ok=deleted", http.StatusSeeOther)
}

func (s *Server) editionCoverDelete(w http.ResponseWriter, r *http.Request) {
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
	http.Redirect(w, r, "/admin/editions/"+formatID(id)+"/edit?ok=deleted", http.StatusSeeOther)
}
