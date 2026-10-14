package web

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type staticAsset struct {
	body []byte
	hash string
}

type staticAssets map[string]staticAsset

// Snapshot assets at startup so the URL hash always describes the bytes served,
// even if a deployment replaces files while this process is still running.
func loadStaticAssets(dir string) (staticAssets, error) {
	assets := staticAssets{}
	err := fs.WalkDir(os.DirFS(dir), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		assets[name] = staticAsset{body: body, hash: fmt.Sprintf("%x", sha256.Sum256(body))}
		return nil
	})
	return assets, err
}

func (a staticAssets) URL(name string) string {
	u := url.URL{Path: "/static/" + name}
	if asset, ok := a[name]; ok {
		u.RawQuery = "v=" + asset.hash
	}
	return u.String()
}

func (a staticAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.serve(w, r, strings.TrimPrefix(r.URL.Path, "/static/"))
}

func (a staticAssets) serve(w http.ResponseWriter, r *http.Request, name string) {
	asset, ok := a[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("ETag", `"`+asset.hash+`"`)
	if r.URL.Query().Get("v") == asset.hash {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// Legacy or stale version URLs must revalidate, never pin newer bytes to an old hash.
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(asset.body))
}
