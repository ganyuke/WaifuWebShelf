package db

import (
	"database/sql"

	"waifuwebshelf/internal/money"
)

type Wishlist struct {
	ID         int64
	Kind       string
	SeriesID   int64
	BookLineID sql.NullInt64
	VolFrom    string
	VolTo      string
	Name       string
	MerchKind  string
	Quantity   int
	MSRPCents  sql.NullInt64
	Currency   string
	Notes      string
	ImagePath  sql.NullString
	Status     string
	CreatedAt  string
	UpdatedAt  string
}

type WishlistWrite struct {
	Kind       string
	SeriesID   int64
	BookLineID sql.NullInt64
	VolFrom    string
	VolTo      string
	Name       string
	MerchKind  string
	Quantity   int
	MSRPCents  *int64
	Currency   string
	Notes      string
	ImagePath  sql.NullString
}

type WishlistRow struct {
	Wishlist
	SeriesName string
	SeriesSlug string
	LineKind   string
	LineLabel  string
	LineSlug   string
}

const wishlistCols = `id, kind, series_id, book_line_id, vol_from, vol_to, name, merch_kind, quantity,
	msrp_cents, currency, notes, image_path, status, created_at, updated_at`

func scanWishlist(w *Wishlist) []any {
	return []any{
		&w.ID, &w.Kind, &w.SeriesID, &w.BookLineID, &w.VolFrom, &w.VolTo, &w.Name, &w.MerchKind, &w.Quantity,
		&w.MSRPCents, &w.Currency, &w.Notes, &w.ImagePath, &w.Status, &w.CreatedAt, &w.UpdatedAt,
	}
}

func (d *DB) CreateWishlist(in WishlistWrite) (int64, error) {
	now := nowUTC()
	res, err := d.SQL.Exec(`
		INSERT INTO wishlist (
			kind, series_id, book_line_id, vol_from, vol_to, name, merch_kind, quantity,
			msrp_cents, currency, notes, image_path, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`,
		in.Kind, in.SeriesID, in.BookLineID, in.VolFrom, in.VolTo, in.Name, in.MerchKind, in.Quantity,
		nullInt64Ptr(in.MSRPCents), money.NormalizeCurrency(in.Currency), in.Notes, in.ImagePath, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateWishlist(id int64, in WishlistWrite) error {
	_, err := d.SQL.Exec(`
		UPDATE wishlist SET
			kind = ?, series_id = ?, book_line_id = ?, vol_from = ?, vol_to = ?, name = ?, merch_kind = ?,
			quantity = ?, msrp_cents = ?, currency = ?, notes = ?, image_path = ?, updated_at = ?
		WHERE id = ?`,
		in.Kind, in.SeriesID, in.BookLineID, in.VolFrom, in.VolTo, in.Name, in.MerchKind,
		in.Quantity, nullInt64Ptr(in.MSRPCents), money.NormalizeCurrency(in.Currency),
		in.Notes, in.ImagePath, nowUTC(), id,
	)
	return err
}

func (d *DB) GetWishlist(id int64) (Wishlist, error) {
	var w Wishlist
	err := d.SQL.QueryRow(`SELECT `+wishlistCols+` FROM wishlist WHERE id = ?`, id).Scan(scanWishlist(&w)...)
	return w, err
}

func (d *DB) GetWishlistRow(id int64) (WishlistRow, error) {
	var r WishlistRow
	err := d.SQL.QueryRow(`
		SELECT w.id, w.kind, w.series_id, w.book_line_id, w.vol_from, w.vol_to, w.name, w.merch_kind, w.quantity,
			w.msrp_cents, w.currency, w.notes, w.image_path, w.status, w.created_at, w.updated_at,
			s.name, s.slug, COALESCE(bl.kind, ''), COALESCE(bl.label, ''), COALESCE(bl.slug, '')
		FROM wishlist w
		JOIN series s ON s.id = w.series_id
		LEFT JOIN book_lines bl ON bl.id = w.book_line_id
		WHERE w.id = ?`, id).Scan(append(scanWishlist(&r.Wishlist), &r.SeriesName, &r.SeriesSlug, &r.LineKind, &r.LineLabel, &r.LineSlug)...)
	return r, err
}

func (d *DB) ListOpenWishlist() ([]WishlistRow, error) {
	return d.listWishlist(`w.status = 'open'`, nil)
}

func (d *DB) ListOpenWishlistForSeries(seriesID int64) ([]WishlistRow, error) {
	return d.listWishlist(`w.status = 'open' AND w.series_id = ?`, []any{seriesID})
}

func (d *DB) ListOpenWishlistForLine(bookLineID int64) ([]Wishlist, error) {
	rows, err := d.SQL.Query(`SELECT `+wishlistCols+` FROM wishlist WHERE status = 'open' AND kind = 'volumes' AND book_line_id = ? ORDER BY id`, bookLineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Wishlist
	for rows.Next() {
		var w Wishlist
		if err := rows.Scan(scanWishlist(&w)...); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (d *DB) listWishlist(where string, args []any) ([]WishlistRow, error) {
	q := `
		SELECT w.id, w.kind, w.series_id, w.book_line_id, w.vol_from, w.vol_to, w.name, w.merch_kind, w.quantity,
			w.msrp_cents, w.currency, w.notes, w.image_path, w.status, w.created_at, w.updated_at,
			s.name, s.slug, COALESCE(bl.kind, ''), COALESCE(bl.label, ''), COALESCE(bl.slug, '')
		FROM wishlist w
		JOIN series s ON s.id = w.series_id
		LEFT JOIN book_lines bl ON bl.id = w.book_line_id
		WHERE ` + where + `
		ORDER BY s.name COLLATE NOCASE, w.kind, w.id`
	rows, err := d.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WishlistRow
	for rows.Next() {
		var r WishlistRow
		if err := rows.Scan(append(scanWishlist(&r.Wishlist), &r.SeriesName, &r.SeriesSlug, &r.LineKind, &r.LineLabel, &r.LineSlug)...); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) MarkWishlistBought(id int64) error {
	_, err := d.SQL.Exec(`UPDATE wishlist SET status = 'bought', updated_at = ? WHERE id = ?`, nowUTC(), id)
	return err
}

func (d *DB) DeleteWishlist(id int64) error {
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'wishlist' AND owner_id = ?`, id); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM wishlist WHERE id = ?`, id)
	return err
}

func (d *DB) LineOwnedKeys(bookLineID int64) (map[string]bool, error) {
	rows, err := d.SQL.Query(`SELECT DISTINCT volume_number FROM volumes WHERE book_line_id = ?`, bookLineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}
