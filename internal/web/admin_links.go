package web

import (
	"net/http"
	"strings"

	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/links"
)

func (s *Server) linkCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ownerType := r.FormValue("owner_type")
	switch ownerType {
	case "series", "book_line", "volume", "volume_number", "merch":
	default:
		http.Error(w, "invalid owner", http.StatusBadRequest)
		return
	}
	ownerID, err := parseID(r.FormValue("owner_id"))
	if err != nil {
		http.Error(w, "invalid owner id", http.StatusBadRequest)
		return
	}
	ownerKey := strings.TrimSpace(r.FormValue("owner_key"))
	if ownerType == "volume_number" {
		if ownerKey == "" {
			http.Error(w, "invalid volume number", http.StatusBadRequest)
			return
		}
	} else {
		ownerKey = ""
	}
	label := strings.TrimSpace(r.FormValue("label"))
	raw := strings.TrimSpace(r.FormValue("url"))
	if label == "" || raw == "" {
		http.Redirect(w, r, redirectAfterLink(r), http.StatusSeeOther)
		return
	}
	u, err := links.NormalizeURL(raw)
	if err != nil || u == "" {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}
	if _, err := s.db.CreateLink(ownerType, ownerID, ownerKey, label, u); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, appendOK(redirectAfterLink(r), "linked"), http.StatusSeeOther)
}

func (s *Server) linkDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.db.GetLink(id); err != nil {
		if db.IsNotFound(err) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	if err := s.db.DeleteLink(id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, appendOK(redirectAfterLink(r), "deleted"), http.StatusSeeOther)
}

func redirectAfterLink(r *http.Request) string {
	next := strings.TrimSpace(r.FormValue("next"))
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		return next
	}
	return "/admin"
}

func appendOK(path, code string) string {
	if strings.Contains(path, "?") {
		return path + "&ok=" + code
	}
	return path + "?ok=" + code
}
