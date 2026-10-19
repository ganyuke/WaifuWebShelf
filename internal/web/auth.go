package web

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	sessionCookie = "shelf_session"
	sessionTTL    = 30 * 24 * time.Hour
)

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoginEnabled() {
		s.notFound(w, r)
		return
	}
	if s.loggedIn(r) {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", "Log in", map[string]any{
		"Next": r.URL.Query().Get("next"),
	})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LoginEnabled() {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "Reload the page and try again.")
		return
	}
	next := safeNext(r.FormValue("next"))
	user := r.FormValue("username")
	pass := r.FormValue("password")
	ip := s.proxies.clientIP(r)
	if wait := s.throttle.wait(ip, time.Now()); wait > 0 {
		log.Printf("login refused ip=%s", ip)
		s.refuseLoginWait(w, r, r.FormValue("next"), wait)
		return
	}
	if !secureEqual(user, s.cfg.AdminUser) || !secureEqual(pass, s.cfg.AdminPass) {
		s.throttle.fail(ip, time.Now())
		log.Printf("login failed user=%q ip=%s", logSafeUser(user), ip)
		s.renderErr(w, r, "login", "Log in", "Wrong username or password.", map[string]any{
			"Next":     r.FormValue("next"),
			"Username": user,
		})
		return
	}
	s.throttle.clear(ip)
	token, err := newSessionToken()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	exp := time.Now().Add(sessionTTL)
	s.sessionMu.Lock()
	s.sessions[token] = exp
	s.sessionMu.Unlock()
	http.SetCookie(w, s.sessionCookieValue(r, token, exp))
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		s.sessionMu.Lock()
		delete(s.sessions, c.Value)
		s.sessionMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.proxies.https(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) loggedIn(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	now := time.Now()
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	exp, ok := s.sessions[c.Value]
	if !ok || now.After(exp) {
		delete(s.sessions, c.Value)
		return false
	}
	s.sessions[c.Value] = now.Add(sessionTTL)
	return true
}

func (s *Server) sessionCookieValue(r *http.Request, token string, exp time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.proxies.https(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
		MaxAge:   int(sessionTTL.Seconds()),
	}
}

func newSessionToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func safeNext(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/admin"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "/admin"
	}
	out := u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}
