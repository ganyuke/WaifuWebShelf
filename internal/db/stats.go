package db

import (
	"database/sql"
	"sort"
)

type MoneyTotals struct {
	Currency             string
	MSRPCents            int64
	ExTaxCents           int64
	IncTaxCents          int64
	ComparableExTaxCents int64
	ItemCount            int
	WithMSRP             int
	MissingMSRP          int
	// TripsWithoutTotal counts trips with a receipt but no amount paid entered yet.
	TripsWithoutTotal int
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
	TripID           sql.NullInt64
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
			series_name, series_slug, line_kind, line_label, line_slug, volume_number, name, merch_kind, quantity, trip_id
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
				1 AS quantity,
				v.trip_id
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
				m.quantity,
				m.trip_id
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
			&e.VolumeNumber, &e.Name, &e.MerchKind, &e.Quantity, &e.TripID,
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

	// Paid counts each trip's total once, and items with their own price with tax
	// only when their trip has no total.
	rows, err := d.SQL.Query(`
		SELECT currency,
			COUNT(*),
			COALESCE(SUM(msrp_cents), 0),
			COALESCE(SUM(price_ex_tax_cents), 0),
			COALESCE(SUM(CASE WHEN trip_paid THEN 0 ELSE price_inc_tax_cents END), 0),
			COALESCE(SUM(CASE WHEN msrp_cents IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN msrp_cents IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN msrp_cents IS NOT NULL THEN COALESCE(price_ex_tax_cents, 0) ELSE 0 END), 0)
		FROM (
			SELECT v.currency, v.msrp_cents, v.price_ex_tax_cents, v.price_inc_tax_cents, t.paid_cents IS NOT NULL AS trip_paid
			FROM volumes v LEFT JOIN trips t ON t.id = v.trip_id
			UNION ALL
			SELECT m.currency, m.msrp_cents, m.price_ex_tax_cents, m.price_inc_tax_cents, t.paid_cents IS NOT NULL
			FROM merch m LEFT JOIN trips t ON t.id = m.trip_id
		)
		GROUP BY currency
		ORDER BY currency`)
	if err != nil {
		return s, err
	}
	byCur := map[string]int{}
	for rows.Next() {
		var t MoneyTotals
		if err := rows.Scan(&t.Currency, &t.ItemCount, &t.MSRPCents, &t.ExTaxCents, &t.IncTaxCents, &t.WithMSRP, &t.MissingMSRP, &t.ComparableExTaxCents); err != nil {
			rows.Close()
			return s, err
		}
		byCur[t.Currency] = len(s.ByCurrency)
		s.ByCurrency = append(s.ByCurrency, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, err
	}

	trips, err := d.SQL.Query(`
		SELECT currency,
			COALESCE(SUM(paid_cents), 0),
			SUM(CASE WHEN paid_cents IS NULL AND EXISTS (SELECT 1 FROM receipt_lines l WHERE l.trip_id = t.id) THEN 1 ELSE 0 END)
		FROM trips t
		GROUP BY currency`)
	if err != nil {
		return s, err
	}
	defer trips.Close()
	for trips.Next() {
		var cur string
		var paid int64
		var missing int
		if err := trips.Scan(&cur, &paid, &missing); err != nil {
			return s, err
		}
		if paid == 0 && missing == 0 {
			continue
		}
		i, ok := byCur[cur]
		if !ok {
			i = len(s.ByCurrency)
			byCur[cur] = i
			s.ByCurrency = append(s.ByCurrency, MoneyTotals{Currency: cur})
		}
		s.ByCurrency[i].IncTaxCents += paid
		s.ByCurrency[i].TripsWithoutTotal += missing
	}
	sort.Slice(s.ByCurrency, func(a, b int) bool { return s.ByCurrency[a].Currency < s.ByCurrency[b].Currency })
	return s, trips.Err()
}

// SpendRow is money spent in one period or at one store, in one currency.
type SpendRow struct {
	Key      string
	Currency string
	Visits   int
	Paid     int64
	// NoTotal counts trips there with a receipt but no amount paid entered yet.
	NoTotal int
}

// spendEvents is every payment: a trip's total, or an item's own price when it
// is not on a trip with a total.
const spendEvents = `
	SELECT t.visited_on AS day, COALESCE(s.name, '') AS store, t.currency, t.paid_cents AS amount, 1 AS visit,
		CASE WHEN t.paid_cents IS NULL AND EXISTS (SELECT 1 FROM receipt_lines l WHERE l.trip_id = t.id) THEN 1 ELSE 0 END AS no_total
	FROM trips t LEFT JOIN stores s ON s.id = t.store_id
	UNION ALL
	SELECT v.purchased_at, v.store, v.currency, COALESCE(v.price_inc_tax_cents, v.price_ex_tax_cents), 0, 0
	FROM volumes v LEFT JOIN trips t ON t.id = v.trip_id
	WHERE t.paid_cents IS NULL AND v.purchased_at IS NOT NULL AND v.purchased_at != ''
	UNION ALL
	SELECT m.purchased_at, m.store, m.currency, COALESCE(m.price_inc_tax_cents, m.price_ex_tax_cents), 0, 0
	FROM merch m LEFT JOIN trips t ON t.id = m.trip_id
	WHERE t.paid_cents IS NULL AND m.purchased_at IS NOT NULL AND m.purchased_at != ''`

func (d *DB) spendBy(keyExpr, order string) ([]SpendRow, error) {
	rows, err := d.SQL.Query(`
		SELECT ` + keyExpr + ` AS k, currency, SUM(visit), COALESCE(SUM(amount), 0), SUM(no_total)
		FROM (` + spendEvents + `)
		GROUP BY k, currency
		ORDER BY ` + order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpendRow
	for rows.Next() {
		var r SpendRow
		if err := rows.Scan(&r.Key, &r.Currency, &r.Visits, &r.Paid, &r.NoTotal); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SpendingByMonth totals what was paid per calendar month (Key is like 2026-10), newest first.
func (d *DB) SpendingByMonth() ([]SpendRow, error) {
	return d.spendBy(`substr(day, 1, 7)`, `k DESC, currency`)
}

// SpendingByStore totals what was paid at each store, most spent first.
func (d *DB) SpendingByStore() ([]SpendRow, error) {
	return d.spendBy(`store`, `currency, SUM(amount) DESC, k COLLATE NOCASE`)
}

// ReceiptTotal adds up receipt lines with the same label and price across every trip.
type ReceiptTotal struct {
	Label     string
	UnitCents int64
	Currency  string
	Count     int
	Cents     int64
}

func (d *DB) ReceiptTotals() ([]ReceiptTotal, error) {
	rows, err := d.SQL.Query(`
		SELECT MIN(l.label), l.unit_cents, t.currency, SUM(l.count), SUM(l.count * l.unit_cents)
		FROM receipt_lines l
		JOIN trips t ON t.id = l.trip_id
		GROUP BY lower(l.label), l.unit_cents, t.currency
		ORDER BY lower(MIN(l.label)), t.currency, l.unit_cents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReceiptTotal
	for rows.Next() {
		var r ReceiptTotal
		if err := rows.Scan(&r.Label, &r.UnitCents, &r.Currency, &r.Count, &r.Cents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
