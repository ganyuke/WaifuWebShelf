package db

import "database/sql"

type MoneyTotals struct {
	Currency             string
	MSRPCents            int64
	ExTaxCents           int64
	IncTaxCents          int64
	ComparableExTaxCents int64
	ItemCount            int
	WithMSRP             int
	MissingMSRP          int
}

func (m MoneyTotals) DeltaCents() int64 {
	return m.MSRPCents - m.ComparableExTaxCents
}

func (m MoneyTotals) Incomplete() bool {
	return m.MissingMSRP > 0
}

type ShelfStats struct {
	SeriesCount int
	VolumeCount int
	MerchPieces int
	MerchRows   int
	ByCurrency  []MoneyTotals
}

type Expenditure struct {
	Kind             string
	ID               int64
	PurchasedAt      sql.NullString
	Store            string
	Currency         string
	MSRPCents        sql.NullInt64
	PriceExTaxCents  sql.NullInt64
	PriceIncTaxCents sql.NullInt64
	SeriesName       string
	SeriesSlug       string
	LineKind         string
	LineLabel        string
	LineSlug         string
	VolumeNumber     string
	Name             string
	MerchKind        string
	Quantity         int
}

func (e Expenditure) Paid() sql.NullInt64 { return paid(e.PriceIncTaxCents, e.PriceExTaxCents) }

// Addition is one recently bought volume or merch piece, with a picture when there is one.
type Addition struct {
	Kind         string
	ID           int64
	PurchasedAt  sql.NullString
	SeriesName   string
	SeriesSlug   string
	LineKind     string
	LineLabel    string
	LineSlug     string
	VolumeNumber string
	Name         string
	Image        sql.NullString
}

// RecentAdditions lists the latest dated purchases, newest first.
func (d *DB) RecentAdditions(limit int) ([]Addition, error) {
	rows, err := d.SQL.Query(`
		SELECT kind, id, purchased_at, series_name, series_slug, line_kind, line_label, line_slug, volume_number, name, image
		FROM (
			SELECT 'volume' AS kind, v.id, v.purchased_at, s.name AS series_name, s.slug AS series_slug,
				bl.kind AS line_kind, bl.label AS line_label, bl.slug AS line_slug, v.volume_number,
				'' AS name, COALESCE(c.image_path, v.image_path) AS image
			FROM volumes v
			JOIN book_lines bl ON bl.id = v.book_line_id
			JOIN series s ON s.id = bl.series_id
			LEFT JOIN volume_covers c ON c.book_line_id = v.book_line_id AND c.volume_number = v.volume_number
			UNION ALL
			SELECT 'merch', m.id, m.purchased_at, s.name, s.slug, '', '', '', '', m.name, m.image_path
			FROM merch m
			JOIN series s ON s.id = m.series_id
		)
		WHERE purchased_at IS NOT NULL AND purchased_at != ''
		ORDER BY purchased_at DESC, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Addition
	for rows.Next() {
		var a Addition
		if err := rows.Scan(&a.Kind, &a.ID, &a.PurchasedAt, &a.SeriesName, &a.SeriesSlug,
			&a.LineKind, &a.LineLabel, &a.LineSlug, &a.VolumeNumber, &a.Name, &a.Image); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) ListExpenditures() ([]Expenditure, error) {
	rows, err := d.SQL.Query(`
		SELECT kind, id, purchased_at, store, currency, msrp_cents, price_ex_tax_cents, price_inc_tax_cents,
			series_name, series_slug, line_kind, line_label, line_slug, volume_number, name, merch_kind, quantity
		FROM (
			SELECT
				'volume' AS kind,
				v.id,
				v.purchased_at,
				v.store,
				v.currency,
				v.msrp_cents,
				v.price_ex_tax_cents,
				v.price_inc_tax_cents,
				s.name AS series_name,
				s.slug AS series_slug,
				bl.kind AS line_kind,
				bl.label AS line_label,
				bl.slug AS line_slug,
				v.volume_number,
				'' AS name,
				'' AS merch_kind,
				1 AS quantity
			FROM volumes v
			JOIN book_lines bl ON bl.id = v.book_line_id
			JOIN series s ON s.id = bl.series_id
			UNION ALL
			SELECT
				'merch',
				m.id,
				m.purchased_at,
				m.store,
				m.currency,
				m.msrp_cents,
				m.price_ex_tax_cents,
				m.price_inc_tax_cents,
				s.name,
				s.slug,
				'',
				'',
				'',
				'',
				m.name,
				m.kind,
				m.quantity
			FROM merch m
			JOIN series s ON s.id = m.series_id
		)
		ORDER BY purchased_at IS NULL, purchased_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Expenditure
	for rows.Next() {
		var e Expenditure
		if err := rows.Scan(
			&e.Kind, &e.ID, &e.PurchasedAt, &e.Store, &e.Currency,
			&e.MSRPCents, &e.PriceExTaxCents, &e.PriceIncTaxCents,
			&e.SeriesName, &e.SeriesSlug, &e.LineKind, &e.LineLabel, &e.LineSlug,
			&e.VolumeNumber, &e.Name, &e.MerchKind, &e.Quantity,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) ShelfStats() (ShelfStats, error) {
	var s ShelfStats
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM series`).Scan(&s.SeriesCount); err != nil {
		return s, err
	}
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM volumes`).Scan(&s.VolumeCount); err != nil {
		return s, err
	}
	if err := d.SQL.QueryRow(`SELECT COUNT(*), COALESCE(SUM(quantity), 0) FROM merch`).Scan(&s.MerchRows, &s.MerchPieces); err != nil {
		return s, err
	}

	rows, err := d.SQL.Query(`
		SELECT currency,
			COUNT(*),
			COALESCE(SUM(msrp_cents), 0),
			COALESCE(SUM(price_ex_tax_cents), 0),
			COALESCE(SUM(price_inc_tax_cents), 0),
			COALESCE(SUM(CASE WHEN msrp_cents IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN msrp_cents IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN msrp_cents IS NOT NULL THEN COALESCE(price_ex_tax_cents, 0) ELSE 0 END), 0)
		FROM (
			SELECT currency, msrp_cents, price_ex_tax_cents, price_inc_tax_cents FROM volumes
			UNION ALL
			SELECT currency, msrp_cents, price_ex_tax_cents, price_inc_tax_cents FROM merch
		)
		GROUP BY currency
		ORDER BY currency`)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var t MoneyTotals
		if err := rows.Scan(&t.Currency, &t.ItemCount, &t.MSRPCents, &t.ExTaxCents, &t.IncTaxCents, &t.WithMSRP, &t.MissingMSRP, &t.ComparableExTaxCents); err != nil {
			return s, err
		}
		s.ByCurrency = append(s.ByCurrency, t)
	}
	return s, rows.Err()
}
