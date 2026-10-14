package db

import (
	"database/sql"
	"strings"
	"time"

	"waifuwebshelf/internal/money"
	"waifuwebshelf/internal/slug"
)

type Series struct {
	ID        int64
	Name      string
	Slug      string
	Notes     string
	ImagePath sql.NullString
	CreatedAt string
	UpdatedAt string
}

type BookLine struct {
	ID               int64
	SeriesID         int64
	Kind             string
	Label            string
	Slug             string
	TotalVolumes     sql.NullInt64
	DefaultMSRPCents sql.NullInt64
	DefaultCurrency  string
	SortOrder        int
}

type BookLineWrite struct {
	Kind             string
	Label            string
	TotalVolumes     *int64
	DefaultMSRPCents *int64
	DefaultCurrency  string
}

type Volume struct {
	ID               int64
	BookLineID       int64
	VolumeNumber     string
	SortOrder        int
	PurchasedAt      sql.NullString
	Store            string
	MSRPCents        sql.NullInt64
	PriceExTaxCents  sql.NullInt64
	PriceIncTaxCents sql.NullInt64
	Currency         string
	ImagePath        sql.NullString
	Notes            string
	TripID           sql.NullInt64
	ReceiptLineID    sql.NullInt64
}

type Merch struct {
	ID               int64
	SeriesID         int64
	Name             string
	Kind             string
	Quantity         int
	PurchasedAt      sql.NullString
	Store            string
	MSRPCents        sql.NullInt64
	PriceExTaxCents  sql.NullInt64
	PriceIncTaxCents sql.NullInt64
	Currency         string
	ImagePath        sql.NullString
	Notes            string
	TripID           sql.NullInt64
	ReceiptLineID    sql.NullInt64
}

// Paid is the price actually paid: with tax when known, otherwise without.
func (v Volume) Paid() sql.NullInt64 { return paid(v.PriceIncTaxCents, v.PriceExTaxCents) }

func (m Merch) Paid() sql.NullInt64 { return paid(m.PriceIncTaxCents, m.PriceExTaxCents) }

// PlusTax is true when the only price is the receipt price, with the tax paid on the trip as a whole.
func (v Volume) PlusTax() bool { return plusTax(v.TripID, v.PriceIncTaxCents, v.PriceExTaxCents) }

func (m Merch) PlusTax() bool { return plusTax(m.TripID, m.PriceIncTaxCents, m.PriceExTaxCents) }

func plusTax(trip, inc, ex sql.NullInt64) bool {
	return trip.Valid && !inc.Valid && ex.Valid
}

func paid(inc, ex sql.NullInt64) sql.NullInt64 {
	if inc.Valid {
		return inc
	}
	return ex
}

type Link struct {
	ID        int64
	OwnerType string
	OwnerID   int64
	OwnerKey  string
	Label     string
	URL       string
}

type SeriesCard struct {
	Series
	Cover      string
	Lines      []LineProgress
	MerchCount int
}

type LineProgress struct {
	ID    int64
	Kind  string
	Label string
	Owned int
	Total *int
}

type MerchRow struct {
	Merch
	SeriesName string
	SeriesSlug string
}

type Purchase struct {
	PurchasedAt      string
	Store            string
	Currency         string
	MSRPCents        *int64
	PriceExTaxCents  *int64
	PriceIncTaxCents *int64
	ImagePath        sql.NullString
	Notes            string
}

func scanSeries(s *Series) []any {
	return []any{&s.ID, &s.Name, &s.Slug, &s.Notes, &s.ImagePath, &s.CreatedAt, &s.UpdatedAt}
}

const seriesCols = `id, name, slug, notes, image_path, created_at, updated_at`

func (d *DB) ListSeries() ([]Series, error) {
	rows, err := d.SQL.Query(`SELECT ` + seriesCols + ` FROM series ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Series
	for rows.Next() {
		var s Series
		if err := rows.Scan(scanSeries(&s)...); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) GetSeries(id int64) (Series, error) {
	var s Series
	err := d.SQL.QueryRow(`SELECT `+seriesCols+` FROM series WHERE id = ?`, id).Scan(scanSeries(&s)...)
	return s, err
}

func (d *DB) GetSeriesBySlug(slug string) (Series, error) {
	var s Series
	err := d.SQL.QueryRow(`SELECT `+seriesCols+` FROM series WHERE slug = ?`, slug).Scan(scanSeries(&s)...)
	return s, err
}

func (d *DB) SlugExists(slug string, exceptID int64) (bool, error) {
	var n int
	err := d.SQL.QueryRow(`SELECT COUNT(*) FROM series WHERE slug = ? AND id != ?`, slug, exceptID).Scan(&n)
	return n > 0, err
}

func (d *DB) CreateSeries(name, slug, notes string, image sql.NullString) (int64, error) {
	now := nowUTC()
	res, err := d.SQL.Exec(
		`INSERT INTO series (name, slug, notes, image_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		name, slug, notes, image, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateSeries(id int64, name, slug, notes string, image sql.NullString) error {
	_, err := d.SQL.Exec(
		`UPDATE series SET name = ?, slug = ?, notes = ?, image_path = ?, updated_at = ? WHERE id = ?`,
		name, slug, notes, image, nowUTC(), id,
	)
	return err
}

func (d *DB) DeleteSeries(id int64) error {
	if err := d.deleteSeriesLinks(id); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM series WHERE id = ?`, id)
	return err
}

func (d *DB) deleteSeriesLinks(seriesID int64) error {
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'series' AND owner_id = ?`, seriesID); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`
		DELETE FROM links WHERE owner_type = 'book_line' AND owner_id IN (
			SELECT id FROM book_lines WHERE series_id = ?
		)`, seriesID); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`
		DELETE FROM links WHERE owner_type = 'volume' AND owner_id IN (
			SELECT v.id FROM volumes v
			JOIN book_lines bl ON bl.id = v.book_line_id
			WHERE bl.series_id = ?
		)`, seriesID); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`
		DELETE FROM links WHERE owner_type = 'volume_number' AND owner_id IN (
			SELECT id FROM book_lines WHERE series_id = ?
		)`, seriesID); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'merch' AND owner_id IN (SELECT id FROM merch WHERE series_id = ?)`, seriesID); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'wishlist' AND owner_id IN (SELECT id FROM wishlist WHERE series_id = ?)`, seriesID)
	return err
}

func (d *DB) SeriesCards() ([]SeriesCard, error) {
	series, err := d.ListSeries()
	if err != nil {
		return nil, err
	}
	lines, err := d.allLineProgress()
	if err != nil {
		return nil, err
	}
	merchCounts, err := d.merchCounts()
	if err != nil {
		return nil, err
	}
	covers, err := d.fallbackCovers()
	if err != nil {
		return nil, err
	}

	bySeries := map[int64][]LineProgress{}
	for _, lp := range lines {
		bySeries[lp.seriesID] = append(bySeries[lp.seriesID], lp.LineProgress)
	}

	out := make([]SeriesCard, 0, len(series))
	for _, s := range series {
		card := SeriesCard{Series: s, Lines: bySeries[s.ID], MerchCount: merchCounts[s.ID]}
		if s.ImagePath.Valid {
			card.Cover = s.ImagePath.String
		} else if c, ok := covers[s.ID]; ok {
			card.Cover = c
		}
		out = append(out, card)
	}
	return out, nil
}

type lineProgressRow struct {
	seriesID int64
	LineProgress
}

func (d *DB) allLineProgress() ([]lineProgressRow, error) {
	rows, err := d.SQL.Query(`
		SELECT bl.id, bl.series_id, bl.kind, bl.label, bl.total_volumes, COUNT(DISTINCT v.volume_number)
		FROM book_lines bl
		LEFT JOIN volumes v ON v.book_line_id = bl.id
		GROUP BY bl.id
		ORDER BY bl.sort_order, bl.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lineProgressRow
	for rows.Next() {
		var r lineProgressRow
		var total sql.NullInt64
		if err := rows.Scan(&r.ID, &r.seriesID, &r.Kind, &r.Label, &total, &r.Owned); err != nil {
			return nil, err
		}
		if total.Valid {
			n := int(total.Int64)
			r.Total = &n
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) merchCounts() (map[int64]int, error) {
	rows, err := d.SQL.Query(`SELECT series_id, COALESCE(SUM(quantity), 0) FROM merch GROUP BY series_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (d *DB) fallbackCovers() (map[int64]string, error) {
	out := map[int64]string{}
	rows, err := d.SQL.Query(`
		SELECT bl.series_id, c.image_path
		FROM volume_covers c
		JOIN book_lines bl ON bl.id = c.book_line_id
		WHERE c.image_path IS NOT NULL AND c.image_path != ''
		ORDER BY bl.sort_order, c.volume_number, c.book_line_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := out[id]; !ok {
			out[id] = path
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = d.SQL.Query(`
		SELECT bl.series_id, v.image_path
		FROM volumes v
		JOIN book_lines bl ON bl.id = v.book_line_id
		WHERE v.image_path IS NOT NULL AND v.image_path != ''
		ORDER BY bl.sort_order, v.sort_order, v.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		if _, ok := out[id]; !ok {
			out[id] = path
		}
	}
	return out, rows.Err()
}

func bookLineCols() string {
	return `id, series_id, kind, label, slug, total_volumes, default_msrp_cents, default_currency, sort_order`
}

func scanBookLine(b *BookLine) []any {
	return []any{&b.ID, &b.SeriesID, &b.Kind, &b.Label, &b.Slug, &b.TotalVolumes, &b.DefaultMSRPCents, &b.DefaultCurrency, &b.SortOrder}
}

func (d *DB) ListBookLines(seriesID int64) ([]BookLine, error) {
	rows, err := d.SQL.Query(`SELECT `+bookLineCols()+` FROM book_lines WHERE series_id = ? ORDER BY sort_order, id`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BookLine
	for rows.Next() {
		var b BookLine
		if err := rows.Scan(scanBookLine(&b)...); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (d *DB) GetBookLine(id int64) (BookLine, error) {
	var b BookLine
	err := d.SQL.QueryRow(`SELECT `+bookLineCols()+` FROM book_lines WHERE id = ?`, id).Scan(scanBookLine(&b)...)
	return b, err
}

func (d *DB) GetBookLineBySlug(seriesID int64, lineSlug string) (BookLine, error) {
	var b BookLine
	err := d.SQL.QueryRow(`SELECT `+bookLineCols()+` FROM book_lines WHERE series_id = ? AND slug = ?`, seriesID, lineSlug).Scan(scanBookLine(&b)...)
	return b, err
}

func (d *DB) lineSlugFor(seriesID, exceptID int64, kind, label string) string {
	return uniqueLineSlug(d.SQL, seriesID, exceptID, slug.ForBookLine(kind, label))
}

func uniqueLineSlug(sqlDB *sql.DB, seriesID, exceptID int64, base string) string {
	return slug.Unique(base, func(candidate string) bool {
		var n int
		_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM book_lines WHERE series_id = ? AND slug = ? AND id != ?`, seriesID, candidate, exceptID).Scan(&n)
		return n > 0
	})
}

func (d *DB) CreateBookLine(seriesID int64, in BookLineWrite) (int64, error) {
	var next int
	if err := d.SQL.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM book_lines WHERE series_id = ?`, seriesID).Scan(&next); err != nil {
		return 0, err
	}
	now := nowUTC()
	lineSlug := d.lineSlugFor(seriesID, 0, in.Kind, in.Label)
	res, err := d.SQL.Exec(
		`INSERT INTO book_lines (series_id, kind, label, slug, total_volumes, default_msrp_cents, default_currency, sort_order, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seriesID, in.Kind, in.Label, lineSlug, nullInt64Ptr(in.TotalVolumes),
		nullInt64Ptr(in.DefaultMSRPCents), money.NormalizeCurrency(in.DefaultCurrency), next, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateBookLine(id int64, in BookLineWrite) error {
	line, err := d.GetBookLine(id)
	if err != nil {
		return err
	}
	lineSlug := d.lineSlugFor(line.SeriesID, id, in.Kind, in.Label)
	_, err = d.SQL.Exec(
		`UPDATE book_lines SET kind = ?, label = ?, slug = ?, total_volumes = ?, default_msrp_cents = ?, default_currency = ?, updated_at = ? WHERE id = ?`,
		in.Kind, in.Label, lineSlug, nullInt64Ptr(in.TotalVolumes),
		nullInt64Ptr(in.DefaultMSRPCents), money.NormalizeCurrency(in.DefaultCurrency), nowUTC(), id,
	)
	return err
}

func (d *DB) DeleteBookLine(id int64) error {
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'book_line' AND owner_id = ?`, id); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`
		DELETE FROM links WHERE owner_type = 'volume' AND owner_id IN (
			SELECT id FROM volumes WHERE book_line_id = ?
		)`, id); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'volume_number' AND owner_id = ?`, id); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'wishlist' AND owner_id IN (SELECT id FROM wishlist WHERE book_line_id = ?)`, id); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM book_lines WHERE id = ?`, id)
	return err
}

func volumeCols() string {
	return `id, book_line_id, volume_number, sort_order, purchased_at, store,
		msrp_cents, price_ex_tax_cents, price_inc_tax_cents, currency, image_path, notes, trip_id, receipt_line_id`
}

func scanVolume(v *Volume) []any {
	return []any{
		&v.ID, &v.BookLineID, &v.VolumeNumber, &v.SortOrder, &v.PurchasedAt, &v.Store,
		&v.MSRPCents, &v.PriceExTaxCents, &v.PriceIncTaxCents, &v.Currency, &v.ImagePath, &v.Notes,
		&v.TripID, &v.ReceiptLineID,
	}
}

func (d *DB) ListVolumes(bookLineID int64) ([]Volume, error) {
	rows, err := d.SQL.Query(`SELECT `+volumeCols()+` FROM volumes WHERE book_line_id = ? ORDER BY sort_order, id`, bookLineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Volume
	for rows.Next() {
		var v Volume
		if err := rows.Scan(scanVolume(&v)...); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) ListVolumesByNumber(bookLineID int64, number string) ([]Volume, error) {
	rows, err := d.SQL.Query(`SELECT `+volumeCols()+` FROM volumes WHERE book_line_id = ? AND volume_number = ? ORDER BY purchased_at, id`, bookLineID, number)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Volume
	for rows.Next() {
		var v Volume
		if err := rows.Scan(scanVolume(&v)...); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) GetVolume(id int64) (Volume, error) {
	var v Volume
	err := d.SQL.QueryRow(`SELECT `+volumeCols()+` FROM volumes WHERE id = ?`, id).Scan(scanVolume(&v)...)
	return v, err
}

func (d *DB) CreateVolume(bookLineID int64, number string, sortOrder int, p Purchase) (int64, error) {
	now := nowUTC()
	res, err := d.SQL.Exec(`
		INSERT INTO volumes (
			book_line_id, volume_number, sort_order, purchased_at, store,
			msrp_cents, price_ex_tax_cents, price_inc_tax_cents, currency, image_path, notes,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bookLineID, number, sortOrder, nullStr(p.PurchasedAt), p.Store,
		nullInt64Ptr(p.MSRPCents), nullInt64Ptr(p.PriceExTaxCents), nullInt64Ptr(p.PriceIncTaxCents),
		p.Currency, p.ImagePath, p.Notes, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateVolume(id int64, number string, sortOrder int, p Purchase) error {
	old, err := d.GetVolume(id)
	if err != nil {
		return err
	}
	_, err = d.SQL.Exec(`
		UPDATE volumes SET
			volume_number = ?, sort_order = ?, purchased_at = ?, store = ?,
			msrp_cents = ?, price_ex_tax_cents = ?, price_inc_tax_cents = ?,
			currency = ?, image_path = ?, notes = ?, updated_at = ?
		WHERE id = ?`,
		number, sortOrder, nullStr(p.PurchasedAt), p.Store,
		nullInt64Ptr(p.MSRPCents), nullInt64Ptr(p.PriceExTaxCents), nullInt64Ptr(p.PriceIncTaxCents),
		p.Currency, p.ImagePath, p.Notes, nowUTC(), id,
	)
	if err != nil {
		return err
	}
	if old.VolumeNumber != number {
		return d.pruneVolumeNumber(old.BookLineID, old.VolumeNumber)
	}
	return nil
}

func (d *DB) DeleteVolume(id int64) error {
	v, err := d.GetVolume(id)
	if err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'volume' AND owner_id = ?`, id); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`DELETE FROM volumes WHERE id = ?`, id); err != nil {
		return err
	}
	return d.pruneVolumeNumber(v.BookLineID, v.VolumeNumber)
}

func (d *DB) pruneVolumeNumber(bookLineID int64, number string) error {
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM volumes WHERE book_line_id = ? AND volume_number = ?`, bookLineID, number).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'volume_number' AND owner_id = ? AND owner_key = ?`, bookLineID, number); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM volume_covers WHERE book_line_id = ? AND volume_number = ?`, bookLineID, number)
	return err
}

func (d *DB) VolumeCover(bookLineID int64, number string) (sql.NullString, error) {
	var path sql.NullString
	err := d.SQL.QueryRow(
		`SELECT image_path FROM volume_covers WHERE book_line_id = ? AND volume_number = ?`,
		bookLineID, number,
	).Scan(&path)
	if err == sql.ErrNoRows {
		return sql.NullString{}, nil
	}
	return path, err
}

func (d *DB) SetVolumeCover(bookLineID int64, number string, path sql.NullString) error {
	if !path.Valid || path.String == "" {
		_, err := d.SQL.Exec(`DELETE FROM volume_covers WHERE book_line_id = ? AND volume_number = ?`, bookLineID, number)
		return err
	}
	_, err := d.SQL.Exec(`
		INSERT INTO volume_covers (book_line_id, volume_number, image_path)
		VALUES (?, ?, ?)
		ON CONFLICT(book_line_id, volume_number) DO UPDATE SET image_path = excluded.image_path`,
		bookLineID, number, path.String,
	)
	return err
}

// LineCovers maps volume number to cover image for one book line.
func (d *DB) LineCovers(bookLineID int64) (map[string]string, error) {
	rows, err := d.SQL.Query(
		`SELECT volume_number, image_path FROM volume_covers WHERE book_line_id = ? AND image_path != ''`,
		bookLineID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var number, path string
		if err := rows.Scan(&number, &path); err != nil {
			return nil, err
		}
		out[number] = path
	}
	return out, rows.Err()
}

func merchCols() string {
	return `id, series_id, name, kind, quantity, purchased_at, store,
		msrp_cents, price_ex_tax_cents, price_inc_tax_cents, currency, image_path, notes, trip_id, receipt_line_id`
}

func scanMerch(m *Merch) []any {
	return []any{
		&m.ID, &m.SeriesID, &m.Name, &m.Kind, &m.Quantity, &m.PurchasedAt, &m.Store,
		&m.MSRPCents, &m.PriceExTaxCents, &m.PriceIncTaxCents, &m.Currency, &m.ImagePath, &m.Notes,
		&m.TripID, &m.ReceiptLineID,
	}
}

func (d *DB) ListMerch(seriesID int64) ([]Merch, error) {
	rows, err := d.SQL.Query(`SELECT `+merchCols()+` FROM merch WHERE series_id = ? ORDER BY purchased_at DESC, id DESC`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Merch
	for rows.Next() {
		var m Merch
		if err := rows.Scan(scanMerch(&m)...); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) ListAllMerch() ([]MerchRow, error) {
	rows, err := d.SQL.Query(`
		SELECT m.id, m.series_id, m.name, m.kind, m.quantity, m.purchased_at, m.store,
			m.msrp_cents, m.price_ex_tax_cents, m.price_inc_tax_cents, m.currency, m.image_path, m.notes,
			m.trip_id, m.receipt_line_id, s.name, s.slug
		FROM merch m
		JOIN series s ON s.id = m.series_id
		ORDER BY s.name COLLATE NOCASE, m.name COLLATE NOCASE, m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MerchRow
	for rows.Next() {
		var r MerchRow
		if err := rows.Scan(
			&r.ID, &r.SeriesID, &r.Name, &r.Kind, &r.Quantity, &r.PurchasedAt, &r.Store,
			&r.MSRPCents, &r.PriceExTaxCents, &r.PriceIncTaxCents, &r.Currency, &r.ImagePath, &r.Notes,
			&r.TripID, &r.ReceiptLineID, &r.SeriesName, &r.SeriesSlug,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) GetMerch(id int64) (Merch, error) {
	var m Merch
	err := d.SQL.QueryRow(`SELECT `+merchCols()+` FROM merch WHERE id = ?`, id).Scan(scanMerch(&m)...)
	return m, err
}

func (d *DB) GetMerchRow(id int64) (MerchRow, error) {
	var r MerchRow
	err := d.SQL.QueryRow(`
		SELECT m.id, m.series_id, m.name, m.kind, m.quantity, m.purchased_at, m.store,
			m.msrp_cents, m.price_ex_tax_cents, m.price_inc_tax_cents, m.currency, m.image_path, m.notes,
			m.trip_id, m.receipt_line_id, s.name, s.slug
		FROM merch m
		JOIN series s ON s.id = m.series_id
		WHERE m.id = ?`, id).Scan(
		&r.ID, &r.SeriesID, &r.Name, &r.Kind, &r.Quantity, &r.PurchasedAt, &r.Store,
		&r.MSRPCents, &r.PriceExTaxCents, &r.PriceIncTaxCents, &r.Currency, &r.ImagePath, &r.Notes,
		&r.TripID, &r.ReceiptLineID, &r.SeriesName, &r.SeriesSlug,
	)
	return r, err
}

func (d *DB) CreateMerch(seriesID int64, name, kind string, qty int, p Purchase) (int64, error) {
	now := nowUTC()
	res, err := d.SQL.Exec(`
		INSERT INTO merch (
			series_id, name, kind, quantity, purchased_at, store,
			msrp_cents, price_ex_tax_cents, price_inc_tax_cents, currency, image_path, notes,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seriesID, name, kind, qty, nullStr(p.PurchasedAt), p.Store,
		nullInt64Ptr(p.MSRPCents), nullInt64Ptr(p.PriceExTaxCents), nullInt64Ptr(p.PriceIncTaxCents),
		p.Currency, p.ImagePath, p.Notes, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateMerch(id int64, name, kind string, qty int, p Purchase) error {
	_, err := d.SQL.Exec(`
		UPDATE merch SET
			name = ?, kind = ?, quantity = ?, purchased_at = ?, store = ?,
			msrp_cents = ?, price_ex_tax_cents = ?, price_inc_tax_cents = ?,
			currency = ?, image_path = ?, notes = ?, updated_at = ?
		WHERE id = ?`,
		name, kind, qty, nullStr(p.PurchasedAt), p.Store,
		nullInt64Ptr(p.MSRPCents), nullInt64Ptr(p.PriceExTaxCents), nullInt64Ptr(p.PriceIncTaxCents),
		p.Currency, p.ImagePath, p.Notes, nowUTC(), id,
	)
	return err
}

func (d *DB) DeleteMerch(id int64) error {
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'merch' AND owner_id = ?`, id); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM merch WHERE id = ?`, id)
	return err
}

func (d *DB) ListLinks(ownerType string, ownerID int64) ([]Link, error) {
	return d.ListLinksKeyed(ownerType, ownerID, "")
}

func (d *DB) ListLinksKeyed(ownerType string, ownerID int64, key string) ([]Link, error) {
	rows, err := d.SQL.Query(
		`SELECT id, owner_type, owner_id, owner_key, label, url FROM links WHERE owner_type = ? AND owner_id = ? AND owner_key = ? ORDER BY id`,
		ownerType, ownerID, key,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.OwnerType, &l.OwnerID, &l.OwnerKey, &l.Label, &l.URL); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *DB) CreateLink(ownerType string, ownerID int64, ownerKey, label, url string) (int64, error) {
	res, err := d.SQL.Exec(
		`INSERT INTO links (owner_type, owner_id, owner_key, label, url) VALUES (?, ?, ?, ?, ?)`,
		ownerType, ownerID, ownerKey, label, url,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetLink(id int64) (Link, error) {
	var l Link
	err := d.SQL.QueryRow(`SELECT id, owner_type, owner_id, owner_key, label, url FROM links WHERE id = ?`, id).Scan(
		&l.ID, &l.OwnerType, &l.OwnerID, &l.OwnerKey, &l.Label, &l.URL,
	)
	return l, err
}

func (d *DB) DeleteLink(id int64) error {
	_, err := d.SQL.Exec(`DELETE FROM links WHERE id = ?`, id)
	return err
}

func nowUTC() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

func IsUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "constraint")
}

func IsNotFound(err error) bool {
	return err == sql.ErrNoRows
}
