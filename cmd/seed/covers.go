package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"waifuwebshelf/internal/photos"
)

const (
	dexMain = "32fdfe9b-6e11-4a13-9e36-dcd8ea77b4e4"
	dexShy  = "bd2dd893-6bbc-4d6c-8c06-921318ef9ac0"
	dexAnth = "53549b45-6aa8-48fc-97c0-525c15906c29"
)

type dexCoverPage struct {
	Data []struct {
		Attributes struct {
			Volume   string `json:"volume"`
			FileName string `json:"fileName"`
			Locale   string `json:"locale"`
		} `json:"attributes"`
	} `json:"data"`
	Total int `json:"total"`
}

func newHTTP() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: newCachingTransport()}
}

func getJSON(client *http.Client, rawURL string, dest any) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "WaifuWebShelf-seed/1.0")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s: %s %s", rawURL, res.Status, body)
	}
	return json.NewDecoder(res.Body).Decode(dest)
}

func fetchDexCovers(client *http.Client, mangaID string) (map[string]map[string]string, error) {
	// locale -> volume -> fileName
	out := map[string]map[string]string{}
	for offset := 0; ; offset += 100 {
		u := fmt.Sprintf("https://api.mangadex.org/cover?manga[]=%s&limit=100&offset=%d&order[volume]=asc", url.QueryEscape(mangaID), offset)
		var page dexCoverPage
		if err := getJSON(client, u, &page); err != nil {
			return nil, err
		}
		for _, item := range page.Data {
			vol := strings.TrimSpace(item.Attributes.Volume)
			if vol == "" {
				continue
			}
			loc := item.Attributes.Locale
			if loc == "" {
				loc = "ja"
			}
			if out[loc] == nil {
				out[loc] = map[string]string{}
			}
			if _, ok := out[loc][vol]; !ok {
				out[loc][vol] = item.Attributes.FileName
			}
		}
		if offset+100 >= page.Total || len(page.Data) == 0 {
			break
		}
	}
	return out, nil
}

func pickCover(covers map[string]map[string]string, volume string, locales ...string) (string, bool) {
	for _, loc := range locales {
		if fn, ok := covers[loc][volume]; ok {
			return fn, true
		}
	}
	return "", false
}

func dexCoverURL(mangaID, fileName string) string {
	return fmt.Sprintf("https://uploads.mangadex.org/covers/%s/%s.512.jpg", mangaID, fileName)
}

// download fetches a picture and saves it in imagesDir as base plus the
// extension the app picks, along with the small version, the same way an
// upload is saved. It returns the saved name.
func download(client *http.Client, rawURL, imagesDir, base string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "WaifuWebShelf-seed/1.0")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", rawURL, res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return savePicture(imagesDir, base, data)
}

func savePicture(imagesDir, base string, data []byte) (string, error) {
	pic, err := photos.Process(data, true)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return "", err
	}
	name := base + pic.Ext
	if err := os.WriteFile(filepath.Join(imagesDir, name), pic.Full, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(imagesDir, photos.SmallName(name)), pic.Small, 0o644); err != nil {
		return "", err
	}
	return name, nil
}

func saveCover(client *http.Client, imagesDir, base, mangaID, fileName string) (string, error) {
	return download(client, dexCoverURL(mangaID, fileName), imagesDir, base)
}

func volKey(n int) string {
	return strconv.Itoa(n)
}
