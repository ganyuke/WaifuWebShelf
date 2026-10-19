package db

import (
	"database/sql"
	"strings"
	"time"

	"waifuwebshelf/internal/names"
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

type Volume struct {
	ID               int64
	EditionID        int64
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
	Editions   []EditionProgress
	MerchCount int
}

// VolumesOwned counts volumes owned across all editions.
func (c SeriesCard) VolumesOwned() int {
	n := 0
	for _, e := range c.Editions {
		n += e.Owned
	}
	return n
}

type EditionProgress struct {
	ID    int64
	Owned int
	Total *int
	names.Naming
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
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'work' AND owner_id IN (SELECT id FROM works WHERE series_id = ?)`, seriesID); err != nil {
		return err
	}
	if err := deleteEditionLinks(d.SQL, `SELECT edition_id FROM edition_names WHERE series_id = ?`, seriesID); err != nil {
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
	editions, err := d.allEditionProgress()
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

	bySeries := map[int64][]EditionProgress{}
	for _, ep := range editions {
		bySeries[ep.seriesID] = append(bySeries[ep.seriesID], ep.EditionProgress)
	}

	out := make([]SeriesCard, 0, len(series))
	for _, s := range series {
		card := SeriesCard{Series: s, Editions: bySeries[s.ID], MerchCount: merchCounts[s.ID]}
		if s.ImagePath.Valid {
			card.Cover = s.ImagePath.String
		} else if c, ok := covers[s.ID]; ok {
			card.Cover = c
		}
		out = append(out, card)
	}
	return out, nil
}

type editionProgressRow struct {
	seriesID int64
	EditionProgress
}

func (d *DB) allEditionProgress() ([]editionProgressRow, error) {
	rows, err := d.SQL.Query(`
		SELECT e.id, n.series_id, e.total_volumes, COUNT(DISTINCT v.volume_number), ` + namingCols("n") + `
		FROM editions e
		JOIN edition_names n ON n.edition_id = e.id
		LEFT JOIN volumes v ON v.edition_id = e.id
		GROUP BY e.id
		ORDER BY n.work_order, n.work_id, e.sort_order, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []editionProgressRow
	for rows.Next() {
		var r editionProgressRow
		var total sql.NullInt64
		if err := rows.Scan(append([]any{&r.ID, &r.seriesID, &total, &r.Owned}, scanNaming(&r.Naming)...)...); err != nil {
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
		SELECT n.series_id, i.cover_path
		FROM volume_info i
		JOIN edition_names n ON n.edition_id = i.edition_id
		WHERE i.cover_path IS NOT NULL AND i.cover_path != ''
		ORDER BY n.work_order, n.edition_order, i.volume_number, i.edition_id`)
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
		SELECT n.series_id, v.image_path
		FROM volumes v
		JOIN edition_names n ON n.edition_id = v.edition_id
		WHERE v.image_path IS NOT NULL AND v.image_path != ''
		ORDER BY n.work_order, n.edition_order, v.sort_order, v.id`)
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

func volumeCols() string {
	return `id, edition_id, volume_number, sort_order, purchased_at, store,
		msrp_cents, price_ex_tax_cents, price_inc_tax_cents, currency, image_path, notes, trip_id, receipt_line_id`
}

func scanVolume(v *Volume) []any {
	return []any{
		&v.ID, &v.EditionID, &v.VolumeNumber, &v.SortOrder, &v.PurchasedAt, &v.Store,
		&v.MSRPCents, &v.PriceExTaxCents, &v.PriceIncTaxCents, &v.Currency, &v.ImagePath, &v.Notes,
		&v.TripID, &v.ReceiptLineID,
	}
}

func (d *DB) ListVolumes(editionID int64) ([]Volume, error) {
	rows, err := d.SQL.Query(`SELECT `+volumeCols()+` FROM volumes WHERE edition_id = ? ORDER BY sort_order, id`, editionID)
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

func (d *DB) ListVolumesByNumber(editionID int64, number string) ([]Volume, error) {
	rows, err := d.SQL.Query(`SELECT `+volumeCols()+` FROM volumes WHERE edition_id = ? AND volume_number = ? ORDER BY purchased_at, id`, editionID, number)
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

func (d *DB) CreateVolume(editionID int64, number string, sortOrder int, p Purchase) (int64, error) {
	now := nowUTC()
	res, err := d.SQL.Exec(`
		INSERT INTO volumes (
			edition_id, volume_number, sort_order, purchased_at, store,
			msrp_cents, price_ex_tax_cents, price_inc_tax_cents, currency, image_path, notes,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		editionID, number, sortOrder, nullStr(p.PurchasedAt), p.Store,
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
		return d.pruneVolumeNumber(old.EditionID, old.VolumeNumber)
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
	return d.pruneVolumeNumber(v.EditionID, v.VolumeNumber)
}

func (d *DB) pruneVolumeNumber(editionID int64, number string) error {
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM volumes WHERE edition_id = ? AND volume_number = ?`, editionID, number).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'volume_number' AND owner_id = ? AND owner_key = ?`, editionID, number); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM volume_info WHERE edition_id = ? AND volume_number = ?`, editionID, number)
	return err
}

// VolumeInfo is what belongs to a volume number rather than to one copy.
type VolumeInfo struct {
	Title string
	Cover sql.NullString
}

func (d *DB) GetVolumeInfo(editionID int64, number string) (VolumeInfo, error) {
	var vi VolumeInfo
	err := d.SQL.QueryRow(`SELECT title, cover_path FROM volume_info WHERE edition_id = ? AND volume_number = ?`, editionID, number).Scan(&vi.Title, &vi.Cover)
	if err == sql.ErrNoRows {
		return VolumeInfo{}, nil
	}
	return vi, err
}

func (d *DB) VolumeCover(editionID int64, number string) (sql.NullString, error) {
	vi, err := d.GetVolumeInfo(editionID, number)
	return vi.Cover, err
}

// SetVolumeCover sets or, with an empty path, clears a volume number's cover.
func (d *DB) SetVolumeCover(editionID int64, number string, path sql.NullString) error {
	if !path.Valid || path.String == "" {
		path = sql.NullString{}
	}
	return setVolumeInfo(d.SQL, editionID, number, `cover_path`, path)
}

// SetVolumeTitle sets or, with an empty title, clears a volume number's own title.
func (d *DB) SetVolumeTitle(editionID int64, number, title string) error {
	return setVolumeInfo(d.SQL, editionID, number, `title`, strings.TrimSpace(title))
}

func setVolumeInfo(q queryer, editionID int64, number, column string, value any) error {
	if _, err := q.Exec(`
		INSERT INTO volume_info (edition_id, volume_number, `+column+`) VALUES (?, ?, ?)
		ON CONFLICT(edition_id, volume_number) DO UPDATE SET `+column+` = excluded.`+column,
		editionID, number, value); err != nil {
		return err
	}
	_, err := q.Exec(`DELETE FROM volume_info WHERE edition_id = ? AND volume_number = ? AND title = '' AND cover_path IS NULL`, editionID, number)
	return err
}

// EditionInfo maps volume number to what is stored for it in one edition.
func (d *DB) EditionInfo(editionID int64) (map[string]VolumeInfo, error) {
	rows, err := d.SQL.Query(`SELECT volume_number, title, cover_path FROM volume_info WHERE edition_id = ?`, editionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]VolumeInfo{}
	for rows.Next() {
		var number string
		var vi VolumeInfo
		if err := rows.Scan(&number, &vi.Title, &vi.Cover); err != nil {
			return nil, err
		}
		out[number] = vi
	}
	return out, rows.Err()
}

// EditionCovers maps volume number to cover image for one edition.
func (d *DB) EditionCovers(editionID int64) (map[string]string, error) {
	info, err := d.EditionInfo(editionID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for number, vi := range info {
		if vi.Cover.Valid && vi.Cover.String != "" {
			out[number] = vi.Cover.String
		}
	}
	return out, nil
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

// ListLinkURLs lists every saved link address once.
func (d *DB) ListLinkURLs() ([]string, error) {
	rows, err := d.SQL.Query(`SELECT DISTINCT url FROM links ORDER BY url`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
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
