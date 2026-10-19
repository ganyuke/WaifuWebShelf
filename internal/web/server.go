package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/coversource"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/favicon"
)

type Server struct {
	cfg       config.Config
	db        *db.DB
	tmpl      *template.Template
	assets    staticAssets
	imagesDir string
	favicons  *favicon.Store
	covers    *coversource.Client
	// fetchIcons is off until StartFavicons, so tests never reach the network.
	fetchIcons atomic.Bool
	// receiptsDir is only served to the signed-in owner.
	receiptsDir string
	sessions    map[string]time.Time
	sessionMu   sync.Mutex
	proxies     proxyList
	throttle    *loginThrottle
	pageMu      sync.Mutex
	pageGen     uint64
	pageHTML    map[string][]byte
}

type View struct {
	Title        string
	Path         string
	Flash        string
	Err          string
	LoginEnabled bool
	SignedIn     bool
	Preview      Preview
	Data         any
}

// Preview is what chat apps show when a page link is shared.
type Preview struct {
	Text  string
	Image string
}

func New(cfg config.Config, store *db.DB) (*Server, error) {
	tmplDir, err := findDir("templates")
	if err != nil {
		return nil, fmt.Errorf("templates directory not found; run from the repo root or place templates next to the binary")
	}
	staticDir, err := findDir("static")
	if err != nil {
		return nil, fmt.Errorf("static directory not found; run from the repo root or place static next to the binary")
	}
	assets, err := loadStaticAssets(staticDir)
	if err != nil {
		return nil, fmt.Errorf("load static assets: %w", err)
	}
	funcs := funcMap()
	funcs["assetURL"] = assets.URL
	tmpl, err := template.New("").Funcs(funcs).ParseGlob(filepath.Join(tmplDir, "*.html"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.ImagesDir(), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.ReceiptsDir(), 0o700); err != nil {
		return nil, err
	}
	// An empty password would let anyone in.
	if cfg.LoginEnabled() && cfg.AdminPass == "" {
		return nil, fmt.Errorf("login is on but there is no admin password")
	}
	icons, err := favicon.New(cfg.ImagesDir(), false)
	if err != nil {
		return nil, err
	}
	trusted := cfg.TrustedProxies
	if trusted == "" {
		trusted = config.DefaultTrustedProxies
	}
	proxies, err := parseProxies(trusted)
	if err != nil {
		return nil, err
	}
	return &Server{
		proxies:     proxies,
		throttle:    newLoginThrottle(),
		cfg:         cfg,
		db:          store,
		tmpl:        tmpl,
		assets:      assets,
		imagesDir:   cfg.ImagesDir(),
		favicons:    icons,
		covers:      coversource.New(coversource.Sources, false),
		receiptsDir: cfg.ReceiptsDir(),
		sessions:    map[string]time.Time{},
		pageHTML:    map[string][]byte{},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", s.assets)
	mux.Handle("GET /images/", http.StripPrefix("/images/", s.imageServer()))
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		s.assets.serve(w, r, "favicon.svg")
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /", s.notFound)
	mux.HandleFunc("GET /series", s.seriesList)
	mux.HandleFunc("GET /series/{slug}", s.seriesDetail)
	mux.HandleFunc("GET /series/{slug}/{workSlug}/{editionSlug}/{number}", s.volumePage)
	mux.HandleFunc("GET /merch", s.merchList)
	mux.HandleFunc("GET /merch/{id}", s.merchDetail)
	mux.HandleFunc("GET /wishlist", s.wishlist)
	mux.HandleFunc("GET /wishlist/{id}", s.wishlistDetail)
	mux.HandleFunc("GET /stats", s.statsPage)
	mux.HandleFunc("GET /trips/{id}", s.tripPage)

	mux.HandleFunc("GET /preview/series/{slug}", s.previewSeries)
	mux.HandleFunc("GET /preview/series/{slug}/{workSlug}/{editionSlug}/{number}", s.previewVolume)
	mux.HandleFunc("GET /preview/merch/{id}", s.previewMerch)
	mux.HandleFunc("GET /preview/wishlist/{id}", s.previewWishlist)
	mux.HandleFunc("GET /preview/trips/{id}", s.previewTrip)

	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("POST /logout", s.logout)

	mux.HandleFunc("GET /admin", s.requireAdmin(s.adminHome))
	mux.HandleFunc("GET /admin/items/new", s.requireAdmin(s.itemForm))
	mux.HandleFunc("POST /admin/items/new", s.requireAdmin(s.itemSave))
	mux.HandleFunc("GET /admin/{$}", s.requireAdmin(s.adminHome))

	mux.HandleFunc("GET /admin/series/new", s.requireAdmin(s.seriesForm))
	mux.HandleFunc("POST /admin/series/new", s.requireAdmin(s.seriesSaveNew))
	mux.HandleFunc("GET /admin/series/{id}/edit", s.requireAdmin(s.seriesEdit))
	mux.HandleFunc("POST /admin/series/{id}/edit", s.requireAdmin(s.seriesSaveEdit))
	mux.HandleFunc("GET /admin/series/{id}/delete", s.requireAdmin(s.seriesDeleteForm))
	mux.HandleFunc("POST /admin/series/{id}/delete", s.requireAdmin(s.seriesDelete))

	mux.HandleFunc("GET /admin/series/{id}/works/new", s.requireAdmin(s.workNewForm))
	mux.HandleFunc("POST /admin/series/{id}/works/new", s.requireAdmin(s.workSaveNew))
	mux.HandleFunc("GET /admin/works/{id}/edit", s.requireAdmin(s.workEdit))
	mux.HandleFunc("POST /admin/works/{id}/edit", s.requireAdmin(s.workSaveEdit))
	mux.HandleFunc("GET /admin/works/{id}/delete", s.requireAdmin(s.workDeleteForm))
	mux.HandleFunc("POST /admin/works/{id}/delete", s.requireAdmin(s.workDelete))
	mux.HandleFunc("GET /admin/works/{id}/editions/new", s.requireAdmin(s.editionNewForm))
	mux.HandleFunc("POST /admin/works/{id}/editions/new", s.requireAdmin(s.editionSaveNew))
	mux.HandleFunc("GET /admin/editions/{id}/edit", s.requireAdmin(s.editionEdit))
	mux.HandleFunc("POST /admin/editions/{id}/edit", s.requireAdmin(s.editionSaveEdit))
	mux.HandleFunc("GET /admin/editions/{id}/delete", s.requireAdmin(s.editionDeleteForm))
	mux.HandleFunc("POST /admin/editions/{id}/delete", s.requireAdmin(s.editionDelete))
	mux.HandleFunc("POST /admin/editions/{id}/covers", s.requireAdmin(s.editionCoverSave))
	mux.HandleFunc("POST /admin/editions/{id}/covers/find", s.requireAdmin(s.editionCoverFind))
	mux.HandleFunc("POST /admin/editions/{id}/covers/get", s.requireAdmin(s.editionCoverGet))
	mux.HandleFunc("POST /admin/editions/{id}/covers/delete", s.requireAdmin(s.editionCoverDelete))

	mux.HandleFunc("GET /admin/editions/{id}/volumes/new", s.requireAdmin(s.volumeNewForm))
	mux.HandleFunc("POST /admin/editions/{id}/volumes/new", s.requireAdmin(s.volumeSaveNew))
	mux.HandleFunc("GET /admin/volumes/{id}/edit", s.requireAdmin(s.volumeEdit))
	mux.HandleFunc("POST /admin/volumes/{id}/edit", s.requireAdmin(s.volumeSaveEdit))
	mux.HandleFunc("GET /admin/volumes/{id}/delete", s.requireAdmin(s.volumeDeleteForm))
	mux.HandleFunc("POST /admin/volumes/{id}/delete", s.requireAdmin(s.volumeDelete))

	mux.HandleFunc("GET /admin/series/{id}/merch/new", s.requireAdmin(s.merchNewForm))
	mux.HandleFunc("POST /admin/series/{id}/merch/new", s.requireAdmin(s.merchSaveNew))
	mux.HandleFunc("GET /admin/merch/{id}/edit", s.requireAdmin(s.merchEdit))
	mux.HandleFunc("POST /admin/merch/{id}/edit", s.requireAdmin(s.merchSaveEdit))
	mux.HandleFunc("GET /admin/merch/{id}/delete", s.requireAdmin(s.merchDeleteForm))
	mux.HandleFunc("POST /admin/merch/{id}/delete", s.requireAdmin(s.merchDelete))

	mux.HandleFunc("GET /admin/series/{id}/wishlist/new", s.requireAdmin(s.wishlistNewForm))
	mux.HandleFunc("POST /admin/series/{id}/wishlist/new", s.requireAdmin(s.wishlistSaveNew))
	mux.HandleFunc("GET /admin/wishlist/{id}/edit", s.requireAdmin(s.wishlistEdit))
	mux.HandleFunc("POST /admin/wishlist/{id}/edit", s.requireAdmin(s.wishlistSaveEdit))
	mux.HandleFunc("GET /admin/wishlist/{id}/delete", s.requireAdmin(s.wishlistDeleteForm))
	mux.HandleFunc("POST /admin/wishlist/{id}/delete", s.requireAdmin(s.wishlistDelete))

	mux.HandleFunc("GET /admin/trips/new", s.requireAdmin(s.tripNewForm))
	mux.HandleFunc("POST /admin/trips/new", s.requireAdmin(s.tripSaveNew))
	mux.HandleFunc("GET /admin/trips/{id}/edit", s.requireAdmin(s.tripEdit))
	mux.HandleFunc("POST /admin/trips/{id}/edit", s.requireAdmin(s.tripSaveEdit))
	mux.HandleFunc("GET /admin/trips/{id}/delete", s.requireAdmin(s.tripDeleteForm))
	mux.HandleFunc("POST /admin/trips/{id}/delete", s.requireAdmin(s.tripDelete))
	mux.HandleFunc("POST /admin/trips/{id}/books", s.requireAdmin(s.tripAddBooks))
	mux.HandleFunc("GET /admin/trips/{id}/receipt", s.requireAdmin(s.tripReceipt))
	mux.HandleFunc("POST /admin/trips/{id}/tags", s.requireAdmin(s.tagAdd))
	mux.HandleFunc("POST /admin/tags/{id}/delete", s.requireAdmin(s.tagDelete))
	mux.HandleFunc("POST /admin/volumes/{id}/take-off", s.requireAdmin(s.itemTakeOff("volume")))
	mux.HandleFunc("POST /admin/merch/{id}/take-off", s.requireAdmin(s.itemTakeOff("merch")))
	mux.HandleFunc("GET /admin/stores/{id}/edit", s.requireAdmin(s.storeEdit))
	mux.HandleFunc("POST /admin/stores/{id}/edit", s.requireAdmin(s.storeSaveEdit))

	mux.HandleFunc("POST /admin/links", s.requireAdmin(s.linkCreate))
	mux.HandleFunc("POST /admin/links/{id}/delete", s.requireAdmin(s.linkDelete))

	// The mux answers a wrong method with a plain-text page, so it gets the
	// not-found page instead. HEAD is matched by GET patterns.
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern == "" {
			s.renderStatus(w, r, http.StatusMethodNotAllowed, "notfound", "Not found", nil)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return withHeaders(s.guardWrites(s.withPageCache(routes)))
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.LoginEnabled() {
			s.notFound(w, r)
			return
		}
		if !s.loggedIn(r) {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func secureEqual(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}

func (s *Server) logRequest(r *http.Request, cache string) {
	log.Printf("%s %s ip=%s cache=%s", r.Method, r.URL.Path, s.proxies.clientIP(r), cache)
}

func logSafeUser(user string) string {
	user = strings.ReplaceAll(strings.ReplaceAll(user, "\n", " "), "\r", "")
	if len(user) > 64 {
		return user[:64]
	}
	return user
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	s.renderErr(w, r, name, title, "", data)
}

func (s *Server) renderErr(w http.ResponseWriter, r *http.Request, name, title, formErr string, data any) {
	s.renderView(w, r, name, title, formErr, Preview{}, data)
}

func (s *Server) renderPreview(w http.ResponseWriter, r *http.Request, name, title string, preview Preview, data any) {
	s.renderView(w, r, name, title, "", preview, data)
}

func (s *Server) renderView(w http.ResponseWriter, r *http.Request, name, title, formErr string, preview Preview, data any) {
	// Chat apps need a full address for pictures, so there is none without SITE_URL.
	if preview.Image != "" && s.cfg.SiteURL != "" {
		preview.Image = s.cfg.SiteURL + "/images/" + preview.Image
	} else {
		preview.Image = ""
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := View{
		Title:        title,
		Path:         r.URL.Path,
		Flash:        r.URL.Query().Get("ok"),
		Err:          formErr,
		LoginEnabled: s.cfg.LoginEnabled(),
		SignedIn:     s.loggedIn(r),
		Preview:      preview,
		Data:         data,
	}
	if err := s.tmpl.ExecuteTemplate(w, name, view); err != nil {
		log.Printf("template %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderStatus(w, r, http.StatusNotFound, "notfound", "Not found", nil)
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("error: %v", err)
	s.errorPage(w, r, http.StatusInternalServerError, "Something went wrong", "Please try again later.")
}

// badRequest is for posts that can't be read at all. Mistakes in what was
// typed go back to the form instead.
func (s *Server) badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	s.errorPage(w, r, http.StatusBadRequest, "Bad request", msg)
}

// errorPage shows msg in the site layout, with a link back to the page the
// visitor came from when it is on this site.
func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	back := "/"
	if ref, err := url.Parse(r.Referer()); err == nil && ref.Host == r.Host && r.Host != "" {
		back = safeNext(ref.RequestURI())
	}
	s.renderStatus(w, r, status, "error", title, map[string]string{"Message": msg, "Back": back})
}

func (s *Server) renderStatus(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, r, name, title, data)
}

func findDir(name string) (string, error) {
	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	seen := map[string]bool{}
	for _, start := range starts {
		dir := start
		for i := 0; i < 6; i++ {
			if seen[dir] {
				break
			}
			seen[dir] = true
			candidate := filepath.Join(dir, name)
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				return candidate, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fs.ErrNotExist
}
