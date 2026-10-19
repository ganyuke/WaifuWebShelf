package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"waifuwebshelf/internal/coversource"
	"waifuwebshelf/internal/db"
)

// Covers are saved per format and volume number, owned or not. A picture
// replaces the volume's cover, and the old one is copied to
// backups/covers/FORMAT-ID/ in the data folder first.
//
// covers.js sends pictures and Kobo covers one per request and asks for JSON,
// so a whole series stays under the upload limit and each request ends well
// inside the server's timeouts. Without the script, the forms send everything
// in one request and get the format page back.

// coverResult is the JSON answer covers.js reads.
type coverResult struct {
	Saved  int                 `json:"saved"`
	Errors []string            `json:"errors,omitempty"`
	Source string              `json:"source,omitempty"`
	Covers []coversource.Cover `json:"covers,omitempty"`
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// bookLineCoverSave stores uploaded covers. One picture goes to the volume
// number given, and otherwise each picture goes to the number in its file name.
func (s *Server) bookLineCoverSave(w http.ResponseWriter, r *http.Request) {
	id, data, ok := s.coverForm(w, r)
	if !ok {
		return
	}
	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		files = r.MultipartForm.File["cover"]
	}
	if len(files) == 0 {
		s.coverDone(w, r, id, data, coverResult{Errors: []string{"Choose a picture for the cover."}})
		return
	}
	number := normalizeVolumeNumber(r.FormValue("volume_number"))
	if number != "" && len(files) > 1 {
		s.coverDone(w, r, id, data, coverResult{Errors: []string{"Leave the volume number empty to add several covers."}})
		return
	}
	var res coverResult
	for _, fh := range files {
		vol := number
		if vol == "" {
			vol = volumeFromFileName(fh.Filename)
		}
		if vol == "" {
			res.Errors = append(res.Errors, fh.Filename+": Put the volume number in the file name, like vol-14.jpg.")
			continue
		}
		pic, err := readUpload(fh)
		if err == nil {
			err = s.setCover(id, vol, pic)
		}
		if err != nil {
			res.Errors = append(res.Errors, fh.Filename+": "+err.Error())
			continue
		}
		res.Saved++
	}
	s.coverDone(w, r, id, data, res)
}

// bookLineCoverFind lists the covers for the series a store page belongs to.
// Without the script it also saves them.
func (s *Server) bookLineCoverFind(w http.ResponseWriter, r *http.Request) {
	id, data, ok := s.coverForm(w, r)
	if !ok {
		return
	}
	link := strings.TrimSpace(r.FormValue("link"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	src, covers, err := s.covers.Find(ctx, link)
	switch {
	case errors.Is(err, coversource.ErrNoSource):
		msg := "Paste a link to a book on " + joinOr(s.covers.Names()) + "."
		s.coverDone(w, r, id, data, coverResult{Errors: []string{msg}})
		return
	case err != nil:
		log.Printf("covers from %s: %v", link, err)
		msg := "Could not open that page on " + src.Name() + ". Try again later."
		s.coverDone(w, r, id, data, coverResult{Errors: []string{msg}})
		return
	case len(covers) == 0:
		s.coverDone(w, r, id, data, coverResult{Errors: []string{"No volume covers on that page."}})
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, coverResult{Source: src.Name(), Covers: covers})
		return
	}
	var res coverResult
	for _, c := range covers {
		if err := s.fetchCover(r.Context(), id, link, c); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		res.Saved++
	}
	s.coverDone(w, r, id, data, res)
}

// bookLineCoverGet saves one cover found by bookLineCoverFind.
func (s *Server) bookLineCoverGet(w http.ResponseWriter, r *http.Request) {
	id, data, ok := s.coverForm(w, r)
	if !ok {
		return
	}
	c := coversource.Cover{Volume: normalizeVolumeNumber(r.FormValue("volume")), URL: r.FormValue("url")}
	if c.Volume == "" {
		s.coverDone(w, r, id, data, coverResult{Errors: []string{"Reload the page and try again."}})
		return
	}
	if err := s.fetchCover(r.Context(), id, r.FormValue("link"), c); err != nil {
		s.coverDone(w, r, id, data, coverResult{Errors: []string{err.Error()}})
		return
	}
	s.coverDone(w, r, id, data, coverResult{Saved: 1})
}

// coverForm reads the format and form shared by the cover handlers.
func (s *Server) coverForm(w http.ResponseWriter, r *http.Request) (int64, bookLineFormData, bool) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return 0, bookLineFormData{}, false
	}
	if err := parseMaybeMultipart(r); err != nil {
		msg := "Reload the page and try again."
		if errors.As(err, new(*http.MaxBytesError)) {
			msg = "Add fewer covers at a time."
		}
		if wantsJSON(r) {
			writeJSON(w, http.StatusBadRequest, coverResult{Errors: []string{msg}})
		} else {
			s.badRequest(w, r, msg)
		}
		return 0, bookLineFormData{}, false
	}
	data, err := s.loadBookLineForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return 0, bookLineFormData{}, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return 0, bookLineFormData{}, false
	}
	return id, data, true
}

// coverDone answers a cover request. Pages are refreshed here because the
// page cache only notices saves that end in a redirect.
func (s *Server) coverDone(w http.ResponseWriter, r *http.Request, id int64, data bookLineFormData, res coverResult) {
	if res.Saved > 0 {
		s.bumpPages()
	}
	if wantsJSON(r) {
		status := http.StatusOK
		if res.Saved == 0 && len(res.Errors) > 0 {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, res)
		return
	}
	if len(res.Errors) == 0 {
		http.Redirect(w, r, "/admin/book-lines/"+formatID(id)+"/edit?ok=covers-saved", http.StatusSeeOther)
		return
	}
	if res.Saved > 0 {
		if fresh, err := s.loadBookLineForm(id); err == nil {
			data = fresh
		}
		res.Errors = append([]string{fmt.Sprintf("Saved %d %s.", res.Saved, plural(res.Saved, "cover", "covers"))}, res.Errors...)
	}
	w.WriteHeader(http.StatusUnprocessableEntity)
	s.renderErr(w, r, "admin_bookline_form", "Edit format", strings.Join(res.Errors, " "), data)
}

// fetchCover downloads one cover from the source that reads link and saves it.
func (s *Server) fetchCover(ctx context.Context, lineID int64, link string, c coversource.Cover) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pic, err := s.covers.Download(ctx, link, c.URL)
	if err != nil {
		log.Printf("cover for vol. %s from %s: %v", c.Volume, c.URL, err)
		return fmt.Errorf("Could not get the cover for vol. %s.", c.Volume)
	}
	if err := s.setCover(lineID, c.Volume, pic); err != nil {
		return fmt.Errorf("Vol. %s: %w", c.Volume, err)
	}
	return nil
}

// setCover saves pic as the volume's cover, backing up the one it replaces.
func (s *Server) setCover(lineID int64, vol string, pic []byte) error {
	existing, err := s.db.VolumeCover(lineID, vol)
	if err != nil {
		return err
	}
	if err := s.backupCover(lineID, vol, existing); err != nil {
		log.Printf("back up cover %s: %v", existing.String, err)
		return errors.New("Could not back up the old cover, so it was kept.")
	}
	name, err := savePicture(s.imagesDir, pic, true)
	if err != nil {
		return err
	}
	if err := s.db.SetVolumeCover(lineID, vol, sql.NullString{String: name, Valid: true}); err != nil {
		s.deleteImage(sql.NullString{String: name, Valid: true})
		return err
	}
	s.deleteImage(existing)
	return nil
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (s *Server) backupCover(lineID int64, vol string, existing sql.NullString) error {
	if !existing.Valid || existing.String == "" {
		return nil
	}
	name := filepath.Base(existing.String)
	pic, err := os.ReadFile(filepath.Join(s.imagesDir, name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	dir := filepath.Join(s.cfg.DataDir, "backups", "covers", formatID(lineID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "vol-"+unsafeFileChars.ReplaceAllString(vol, "_")+"-"+name), pic, 0o644)
}

var (
	volMarked = regexp.MustCompile(`(?i)(?:^|[^a-z])(?:vol(?:ume)?|v)[ ._-]*([0-9]+(?:\.[0-9]+)?)`)
	anyNumber = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
)

// volumeFromFileName is the volume number in a picture's file name: the one
// after "vol", "volume" or "v" when there is one, and otherwise the last number.
func volumeFromFileName(name string) string {
	name = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if m := volMarked.FindAllStringSubmatch(name, -1); m != nil {
		return normalizeVolumeNumber(m[len(m)-1][1])
	}
	if m := anyNumber.FindAllString(name, -1); m != nil {
		return normalizeVolumeNumber(m[len(m)-1])
	}
	return ""
}

func joinOr(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
