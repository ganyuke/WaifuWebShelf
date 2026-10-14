package web

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
)

const maxUpload = 8 << 20

func parseMaybeMultipart(r *http.Request) error {
	err := r.ParseMultipartForm(maxUpload)
	if err == http.ErrNotMultipart {
		return r.ParseForm()
	}
	return err
}

func (s *Server) parsePurchase(r *http.Request) (db.Purchase, error) {
	currency := money.NormalizeCurrency(r.FormValue("currency"))
	msrp, err := money.Parse(r.FormValue("msrp"), currency)
	if err != nil {
		return db.Purchase{}, fmt.Errorf("MSRP: %w", err)
	}
	ex, err := money.Parse(r.FormValue("price_ex_tax"), currency)
	if err != nil {
		return db.Purchase{}, fmt.Errorf("price without tax: %w", err)
	}
	inc, err := money.Parse(r.FormValue("price_inc_tax"), currency)
	if err != nil {
		return db.Purchase{}, fmt.Errorf("price with tax: %w", err)
	}
	return db.Purchase{
		PurchasedAt:      strings.TrimSpace(r.FormValue("purchased_at")),
		Store:            strings.TrimSpace(r.FormValue("store")),
		Currency:         currency,
		MSRPCents:        msrp,
		PriceExTaxCents:  ex,
		PriceIncTaxCents: inc,
		Notes:            strings.TrimSpace(r.FormValue("notes")),
	}, nil
}

func (s *Server) applyImage(r *http.Request, existing sql.NullString) (sql.NullString, error) {
	return s.applyImageField(r, "image", "remove_image", existing)
}

func (s *Server) applyImageField(r *http.Request, fileField, removeField string, existing sql.NullString) (sql.NullString, error) {
	return s.applyImageFieldIn(s.imagesDir, r, fileField, removeField, existing)
}

// applyImageFieldIn is applyImageField for pictures kept in dir instead of the public images folder.
func (s *Server) applyImageFieldIn(dir string, r *http.Request, fileField, removeField string, existing sql.NullString) (sql.NullString, error) {
	if r.FormValue(removeField) == "1" {
		deleteFileIn(dir, existing)
		return sql.NullString{}, nil
	}
	name, ok, err := saveUploadIn(dir, r, fileField)
	if err != nil {
		return existing, err
	}
	if !ok {
		return existing, nil
	}
	deleteFileIn(dir, existing)
	return sql.NullString{String: name, Valid: true}, nil
}

func (s *Server) saveUpload(r *http.Request, field string) (string, bool, error) {
	return saveUploadIn(s.imagesDir, r, field)
}

func saveUploadIn(dir string, r *http.Request, field string) (string, bool, error) {
	file, header, err := r.FormFile(field)
	if err == http.ErrMissingFile || err == http.ErrNotMultipart {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	if header.Size > maxUpload {
		return "", false, fmt.Errorf("image is larger than 8MB")
	}

	// The file's own bytes decide the type. The name and the browser's claim are ignored.
	var head [512]byte
	n, err := io.ReadFull(file, head[:])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", false, err
	}
	var ext string
	switch http.DetectContentType(head[:n]) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		return "", false, fmt.Errorf("unsupported image type (use jpg, png, webp, or gif)")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", false, err
	}

	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", false, err
	}
	name := hex.EncodeToString(buf[:]) + ext
	dest := filepath.Join(dir, name)
	out, err := os.Create(dest)
	if err != nil {
		return "", false, err
	}
	defer out.Close()
	written, err := io.Copy(out, io.LimitReader(file, maxUpload+1))
	if err == nil && written > maxUpload {
		err = fmt.Errorf("image is larger than 8MB")
	}
	if err != nil {
		_ = os.Remove(dest)
		return "", false, err
	}
	return name, true, nil
}

func (s *Server) deleteImage(existing sql.NullString) {
	deleteFileIn(s.imagesDir, existing)
}

func deleteFileIn(dir string, existing sql.NullString) {
	if !existing.Valid || existing.String == "" {
		return
	}
	name := filepath.Base(existing.String)
	_ = os.Remove(filepath.Join(dir, name))
}

func (s *Server) imageServer() http.Handler {
	fs := http.FileServer(http.Dir(s.imagesDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if name == "." || name == string(filepath.Separator) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		r.URL.Path = name
		fs.ServeHTTP(w, r)
	})
}

func normalizeVolumeNumber(s string) string {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return strconv.Itoa(n)
	}
	return s
}

func volumeSortOrder(number string) int {
	if n, err := strconv.Atoi(number); err == nil {
		return n * 100
	}
	if f, err := strconv.ParseFloat(number, 64); err == nil && f > 0 {
		return int(f * 100)
	}
	return 100000
}

func parseBookLineWrite(r *http.Request) (db.BookLineWrite, string, error) {
	kind := r.FormValue("kind")
	label := strings.TrimSpace(r.FormValue("label"))
	total, err := parseOptionalInt(r.FormValue("total_volumes"))
	if err != nil {
		return db.BookLineWrite{}, "Total volumes " + err.Error() + ".", err
	}
	currency := money.NormalizeCurrency(r.FormValue("default_currency"))
	msrp, err := money.Parse(r.FormValue("default_msrp"), currency)
	if err != nil {
		return db.BookLineWrite{}, "Default MSRP: " + err.Error(), err
	}
	return db.BookLineWrite{
		Kind:             kind,
		Label:            label,
		TotalVolumes:     total,
		DefaultMSRPCents: msrp,
		DefaultCurrency:  currency,
	}, "", nil
}

func parseOptionalInt(s string) (*int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("must be a whole number")
	}
	if n < 1 {
		return nil, nil
	}
	return &n, nil
}

func parseQuantity(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("quantity must be at least 1")
	}
	return n, nil
}

func totalVolumesValue(n sql.NullInt64) string {
	if !n.Valid {
		return ""
	}
	return strconv.FormatInt(n.Int64, 10)
}
