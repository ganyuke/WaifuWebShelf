package db

import (
	"database/sql"

	"waifuwebshelf/internal/money"
	"waifuwebshelf/internal/names"
)

type Wishlist struct {
	ID        int64
	Kind      string
	SeriesID  int64
	EditionID sql.NullInt64
	VolFrom   string
	VolTo     string
	Name      string
	MerchKind string
	Quantity  int
	MSRPCents sql.NullInt64
	Currency  string
	Notes     string
	ImagePath sql.NullString
	Status    string
	CreatedAt string
	UpdatedAt string
}

type WishlistWrite struct {
	Kind      string
	SeriesID  int64
	EditionID sql.NullInt64
	VolFrom   string
	VolTo     string
	Name      string
	MerchKind string
	Quantity  int
	MSRPCents *int64
	Currency  string
	Notes     string
	ImagePath sql.NullString
}

// WishlistRow is a wishlist item with its names. Merch items only have the series.
type WishlistRow struct {
	Wishlist
	names.Naming
}

const wishlistNaming = `s.name, s.slug, COALESCE(n.work_title, ''), COALESCE(n.kind, ''), COALESCE(n.work_slug, ''),
	COALESCE(n.language, ''), COALESCE(n.edition_name, ''), COALESCE(n.edition_slug, '')`

const wishlistCols = `id, kind, series_id, edition_id, vol_from, vol_to, name, merch_kind, quantity,
	msrp_cents, currency, notes, image_path, status, created_at, updated_at`

func scanWishlist(w *Wishlist) []any {
	return []any{
		&w.ID, &w.Kind, &w.SeriesID, &w.EditionID, &w.VolFrom, &w.VolTo, &w.Name, &w.MerchKind, &w.Quantity,
		&w.MSRPCents, &w.Currency, &w.Notes, &w.ImagePath, &w.Status, &w.CreatedAt, &w.UpdatedAt,
	}
}

func (d *DB) CreateWishlist(in WishlistWrite) (int64, error) {
	now := nowUTC()
	res, err := d.SQL.Exec(`
		INSERT INTO wishlist (
			kind, series_id, edition_id, vol_from, vol_to, name, merch_kind, quantity,
			msrp_cents, currency, notes, image_path, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`,
		in.Kind, in.SeriesID, in.EditionID, in.VolFrom, in.VolTo, in.Name, in.MerchKind, in.Quantity,
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
			kind = ?, series_id = ?, edition_id = ?, vol_from = ?, vol_to = ?, name = ?, merch_kind = ?,
			quantity = ?, msrp_cents = ?, currency = ?, notes = ?, image_path = ?, updated_at = ?
		WHERE id = ?`,
		in.Kind, in.SeriesID, in.EditionID, in.VolFrom, in.VolTo, in.Name, in.MerchKind,
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
		SELECT w.id, w.kind, w.series_id, w.edition_id, w.vol_from, w.vol_to, w.name, w.merch_kind, w.quantity,
			w.msrp_cents, w.currency, w.notes, w.image_path, w.status, w.created_at, w.updated_at,
			`+wishlistNaming+`
		FROM wishlist w
		JOIN series s ON s.id = w.series_id
		LEFT JOIN edition_names n ON n.edition_id = w.edition_id
		WHERE w.id = ?`, id).Scan(append(scanWishlist(&r.Wishlist), scanNaming(&r.Naming)...)...)
	return r, err
}

func (d *DB) ListOpenWishlist() ([]WishlistRow, error) {
	return d.listWishlist(`w.status = 'open'`, nil)
}

func (d *DB) ListOpenWishlistForSeries(seriesID int64) ([]WishlistRow, error) {
	return d.listWishlist(`w.status = 'open' AND w.series_id = ?`, []any{seriesID})
}

func (d *DB) ListOpenWishlistForEdition(editionID int64) ([]Wishlist, error) {
	rows, err := d.SQL.Query(`SELECT `+wishlistCols+` FROM wishlist WHERE status = 'open' AND kind = 'volumes' AND edition_id = ? ORDER BY id`, editionID)
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
		SELECT w.id, w.kind, w.series_id, w.edition_id, w.vol_from, w.vol_to, w.name, w.merch_kind, w.quantity,
			w.msrp_cents, w.currency, w.notes, w.image_path, w.status, w.created_at, w.updated_at,
			` + wishlistNaming + `
		FROM wishlist w
		JOIN series s ON s.id = w.series_id
		LEFT JOIN edition_names n ON n.edition_id = w.edition_id
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
		if err := rows.Scan(append(scanWishlist(&r.Wishlist), scanNaming(&r.Naming)...)...); err != nil {
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

func (d *DB) EditionOwnedKeys(editionID int64) (map[string]bool, error) {
	rows, err := d.SQL.Query(`SELECT DISTINCT volume_number FROM volumes WHERE edition_id = ?`, editionID)
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
