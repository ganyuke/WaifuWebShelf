package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"waifuwebshelf/internal/money"
)

type Store struct {
	ID        int64
	Name      string
	Currency  string
	TaxBP     sql.NullInt64
	Visits    int
	LastVisit sql.NullString
}

type StoreWrite struct {
	Name     string
	Currency string
	TaxBP    *int64
}

// Trip is one store visit. Items on it share its date, store and currency,
// and PaidCents is the receipt total, tax included.
type Trip struct {
	ID          int64
	StoreID     sql.NullInt64
	StoreName   string
	TaxBP       sql.NullInt64
	VisitedOn   string
	Currency    string
	PaidCents   sql.NullInt64
	PhotoPath   sql.NullString
	ReceiptPath sql.NullString
	Notes       string
	LineCount   int
}

type ReceiptLine struct {
	ID        int64
	TripID    int64
	Label     string
	UnitCents int64
	Count     int
	// Entered is how many items are on this line so far, merch counted by quantity.
	Entered int
}

func (l ReceiptLine) TotalCents() int64 { return l.UnitCents * int64(l.Count) }

type ReceiptLineWrite struct {
	ID        int64
	Label     string
	UnitCents int64
	Count     int
}

type TripWrite struct {
	StoreID   int64
	NewStore  *StoreWrite
	VisitedOn string
	Currency  string
	PaidCents *int64
	Notes     string
	Photo     sql.NullString
	Receipt   sql.NullString
	Lines     []ReceiptLineWrite
}

// TripItem is a volume or merch row on a trip, with what the trip page shows for it.
type TripItem struct {
	Kind          string
	ID            int64
	ReceiptLineID sql.NullInt64
	SeriesName    string
	SeriesSlug    string
	LineKind      string
	LineLabel     string
	LineSlug      string
	VolumeNumber  string
	Name          string
	Quantity      int
	Image         sql.NullString
	Currency      string
	PriceExTax    sql.NullInt64
	PriceIncTax   sql.NullInt64
}

func (i TripItem) Paid() sql.NullInt64 { return paid(i.PriceIncTax, i.PriceExTax) }

// OwnedError lists volume numbers that are already on the shelf.
type OwnedError struct{ Numbers []string }

func (e *OwnedError) Error() string {
	return "Already owned: " + strings.Join(e.Numbers, ", ") + ". Select Add another copy to add them anyway."
}

var ErrWrongTrip = errors.New("This receipt line belongs to another trip.")

func (d *DB) ListStores() ([]Store, error) {
	rows, err := d.SQL.Query(`
		SELECT s.id, s.name, s.currency, s.tax_bp, COUNT(t.id), MAX(t.visited_on)
		FROM stores s
		LEFT JOIN trips t ON t.store_id = s.id
		GROUP BY s.id
		ORDER BY MAX(t.visited_on) IS NULL, MAX(t.visited_on) DESC, s.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Store
	for rows.Next() {
		var s Store
		if err := rows.Scan(&s.ID, &s.Name, &s.Currency, &s.TaxBP, &s.Visits, &s.LastVisit); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) GetStore(id int64) (Store, error) {
	var s Store
	err := d.SQL.QueryRow(`SELECT id, name, currency, tax_bp FROM stores WHERE id = ?`, id).Scan(&s.ID, &s.Name, &s.Currency, &s.TaxBP)
	return s, err
}

// UpdateStore also renames the store on every item bought there on a trip.
func (d *DB) UpdateStore(id int64, in StoreWrite) error {
	tx, err := d.SQL.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE stores SET name = ?, currency = ?, tax_bp = ? WHERE id = ?`,
		in.Name, money.NormalizeCurrency(in.Currency), nullInt64Ptr(in.TaxBP), id); err != nil {
		return err
	}
	for _, table := range []string{"volumes", "merch"} {
		if _, err := tx.Exec(`UPDATE `+table+` SET store = ? WHERE trip_id IN (SELECT id FROM trips WHERE store_id = ?)`, in.Name, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const tripSelect = `
	SELECT t.id, t.store_id, COALESCE(s.name, ''), s.tax_bp, t.visited_on, t.currency,
		t.paid_cents, t.photo_path, t.receipt_path, t.notes,
		(SELECT COUNT(*) FROM receipt_lines l WHERE l.trip_id = t.id)
	FROM trips t
	LEFT JOIN stores s ON s.id = t.store_id`

func scanTrip(t *Trip) []any {
	return []any{&t.ID, &t.StoreID, &t.StoreName, &t.TaxBP, &t.VisitedOn, &t.Currency,
		&t.PaidCents, &t.PhotoPath, &t.ReceiptPath, &t.Notes, &t.LineCount}
}

func (d *DB) GetTrip(id int64) (Trip, error) {
	var t Trip
	err := d.SQL.QueryRow(tripSelect+` WHERE t.id = ?`, id).Scan(scanTrip(&t)...)
	return t, err
}

// ListTrips lists every trip, newest first.
func (d *DB) ListTrips() ([]Trip, error) {
	rows, err := d.SQL.Query(tripSelect + ` ORDER BY t.visited_on DESC, t.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trip
	for rows.Next() {
		var t Trip
		if err := rows.Scan(scanTrip(&t)...); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) ReceiptLines(tripID int64) ([]ReceiptLine, error) {
	rows, err := d.SQL.Query(`
		SELECT l.id, l.trip_id, l.label, l.unit_cents, l.count,
			(SELECT COUNT(*) FROM volumes v WHERE v.receipt_line_id = l.id)
			+ (SELECT COALESCE(SUM(m.quantity), 0) FROM merch m WHERE m.receipt_line_id = l.id)
		FROM receipt_lines l
		WHERE l.trip_id = ?
		ORDER BY l.sort_order, l.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReceiptLine
	for rows.Next() {
		var l ReceiptLine
		if err := rows.Scan(&l.ID, &l.TripID, &l.Label, &l.UnitCents, &l.Count, &l.Entered); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *DB) TripItems(tripID int64) ([]TripItem, error) {
	rows, err := d.SQL.Query(`
		SELECT kind, id, receipt_line_id, series_name, series_slug, line_kind, line_label, line_slug, volume_number,
			name, quantity, image, currency, price_ex_tax_cents, price_inc_tax_cents
		FROM (
			SELECT 'volume' AS kind, v.id, v.receipt_line_id, s.name AS series_name, s.slug AS series_slug,
				bl.kind AS line_kind, bl.label AS line_label, bl.slug AS line_slug, bl.sort_order AS line_order,
				v.volume_number, v.sort_order AS vol_order, '' AS name, 1 AS quantity,
				COALESCE(c.image_path, v.image_path) AS image, v.currency, v.price_ex_tax_cents, v.price_inc_tax_cents
			FROM volumes v
			JOIN book_lines bl ON bl.id = v.book_line_id
			JOIN series s ON s.id = bl.series_id
			LEFT JOIN volume_covers c ON c.book_line_id = v.book_line_id AND c.volume_number = v.volume_number
			WHERE v.trip_id = ?
			UNION ALL
			SELECT 'merch', m.id, m.receipt_line_id, s.name, s.slug, '', '', '', 0, '', 0,
				m.name, m.quantity, m.image_path, m.currency, m.price_ex_tax_cents, m.price_inc_tax_cents
			FROM merch m
			JOIN series s ON s.id = m.series_id
			WHERE m.trip_id = ?
		)
		ORDER BY kind DESC, series_name COLLATE NOCASE, line_order, vol_order, volume_number, name COLLATE NOCASE, id`, tripID, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TripItem
	for rows.Next() {
		var i TripItem
		if err := rows.Scan(&i.Kind, &i.ID, &i.ReceiptLineID, &i.SeriesName, &i.SeriesSlug, &i.LineKind, &i.LineLabel,
			&i.LineSlug, &i.VolumeNumber, &i.Name, &i.Quantity, &i.Image, &i.Currency, &i.PriceExTax, &i.PriceIncTax); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// resolveStore picks the trip's store, creating a new one unless a store with that name exists.
func resolveStore(tx *sql.Tx, in TripWrite) (sql.NullInt64, error) {
	if in.NewStore == nil {
		if in.StoreID == 0 {
			return sql.NullInt64{}, nil
		}
		return sql.NullInt64{Int64: in.StoreID, Valid: true}, nil
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM stores WHERE name = ? COLLATE NOCASE`, in.NewStore.Name).Scan(&id)
	if err == nil {
		return sql.NullInt64{Int64: id, Valid: true}, nil
	}
	if err != sql.ErrNoRows {
		return sql.NullInt64{}, err
	}
	res, err := tx.Exec(`INSERT INTO stores (name, currency, tax_bp) VALUES (?, ?, ?)`,
		in.NewStore.Name, money.NormalizeCurrency(in.NewStore.Currency), nullInt64Ptr(in.NewStore.TaxBP))
	if err != nil {
		return sql.NullInt64{}, err
	}
	id, err = res.LastInsertId()
	return sql.NullInt64{Int64: id, Valid: true}, err
}

func (d *DB) CreateTrip(in TripWrite) (int64, error) {
	tx, err := d.SQL.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	storeID, err := resolveStore(tx, in)
	if err != nil {
		return 0, err
	}
	now := nowUTC()
	res, err := tx.Exec(`
		INSERT INTO trips (store_id, visited_on, currency, paid_cents, photo_path, receipt_path, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		storeID, in.VisitedOn, money.NormalizeCurrency(in.Currency), nullInt64Ptr(in.PaidCents),
		in.Photo, in.Receipt, in.Notes, now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for i, l := range in.Lines {
		if _, err := tx.Exec(`INSERT INTO receipt_lines (trip_id, label, unit_cents, count, sort_order) VALUES (?, ?, ?, ?, ?)`,
			id, l.Label, l.UnitCents, l.Count, i); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// UpdateTrip saves the trip and its receipt lines. Lines left out are removed,
// and items on them stay on the trip with the price they had.
func (d *DB) UpdateTrip(id int64, in TripWrite) error {
	tx, err := d.SQL.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	storeID, err := resolveStore(tx, in)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE trips SET store_id = ?, visited_on = ?, currency = ?, paid_cents = ?,
			photo_path = ?, receipt_path = ?, notes = ?, updated_at = ?
		WHERE id = ?`,
		storeID, in.VisitedOn, money.NormalizeCurrency(in.Currency), nullInt64Ptr(in.PaidCents),
		in.Photo, in.Receipt, in.Notes, nowUTC(), id); err != nil {
		return err
	}
	existing := map[int64]bool{}
	rows, err := tx.Query(`SELECT id FROM receipt_lines WHERE trip_id = ?`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var lid int64
		if err := rows.Scan(&lid); err != nil {
			rows.Close()
			return err
		}
		existing[lid] = true
	}
	rows.Close()
	kept := map[int64]bool{}
	for i, l := range in.Lines {
		if l.ID != 0 && existing[l.ID] {
			kept[l.ID] = true
			if _, err := tx.Exec(`UPDATE receipt_lines SET label = ?, unit_cents = ?, count = ?, sort_order = ? WHERE id = ?`,
				l.Label, l.UnitCents, l.Count, i, l.ID); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(`INSERT INTO receipt_lines (trip_id, label, unit_cents, count, sort_order) VALUES (?, ?, ?, ?, ?)`,
			id, l.Label, l.UnitCents, l.Count, i); err != nil {
			return err
		}
	}
	for lid := range existing {
		if !kept[lid] {
			if _, err := tx.Exec(`DELETE FROM receipt_lines WHERE id = ?`, lid); err != nil {
				return err
			}
		}
	}
	if err := syncTripItems(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// syncTripItems copies the trip's date, store and currency onto its items, and each
// receipt line's price onto the items on it. Item queries elsewhere read these copies.
func syncTripItems(tx *sql.Tx, tripID int64) error {
	now := nowUTC()
	for _, table := range []string{"volumes", "merch"} {
		if _, err := tx.Exec(`
			UPDATE `+table+` SET
				purchased_at = (SELECT visited_on FROM trips WHERE id = ?1),
				store = (SELECT COALESCE(s.name, '') FROM trips t LEFT JOIN stores s ON s.id = t.store_id WHERE t.id = ?1),
				currency = (SELECT currency FROM trips WHERE id = ?1),
				updated_at = ?2
			WHERE trip_id = ?1`, tripID, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		UPDATE volumes SET
			price_ex_tax_cents = (SELECT unit_cents FROM receipt_lines WHERE id = volumes.receipt_line_id),
			price_inc_tax_cents = NULL
		WHERE trip_id = ? AND receipt_line_id IS NOT NULL`, tripID); err != nil {
		return err
	}
	_, err := tx.Exec(`
		UPDATE merch SET
			price_ex_tax_cents = quantity * (SELECT unit_cents FROM receipt_lines WHERE id = merch.receipt_line_id),
			price_inc_tax_cents = NULL
		WHERE trip_id = ? AND receipt_line_id IS NOT NULL`, tripID)
	return err
}

// DeleteTrip removes the trip and its receipt lines. Its items stay on the shelf
// with their date, store and price.
func (d *DB) DeleteTrip(id int64) error {
	_, err := d.SQL.Exec(`DELETE FROM trips WHERE id = ?`, id)
	return err
}

// tripPurchase fills in what an item bought on a trip gets from it: date, store,
// currency, and the receipt line's price for qty pieces.
func tripPurchase(tx *sql.Tx, tripID, lineID int64, qty int) (Purchase, sql.NullInt64, error) {
	var p Purchase
	err := tx.QueryRow(`
		SELECT t.visited_on, COALESCE(s.name, ''), t.currency
		FROM trips t LEFT JOIN stores s ON s.id = t.store_id
		WHERE t.id = ?`, tripID).Scan(&p.PurchasedAt, &p.Store, &p.Currency)
	if err != nil {
		return p, sql.NullInt64{}, err
	}
	if lineID == 0 {
		return p, sql.NullInt64{}, nil
	}
	var owner, unit int64
	if err := tx.QueryRow(`SELECT trip_id, unit_cents FROM receipt_lines WHERE id = ?`, lineID).Scan(&owner, &unit); err != nil {
		return p, sql.NullInt64{}, err
	}
	if owner != tripID {
		return p, sql.NullInt64{}, ErrWrongTrip
	}
	price := unit * int64(qty)
	p.PriceExTaxCents = &price
	return p, sql.NullInt64{Int64: lineID, Valid: true}, nil
}

// AddVolumes puts several volumes of one book line on a trip in one go.
// Nothing is saved when any number is already owned, unless anotherCopy is set.
func (d *DB) AddVolumes(tripID, receiptLineID, bookLineID int64, numbers []string, sortOrders []int, anotherCopy bool) error {
	tx, err := d.SQL.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, line, err := tripPurchase(tx, tripID, receiptLineID, 1)
	if err != nil {
		return err
	}
	var msrp sql.NullInt64
	var lineCurrency string
	if err := tx.QueryRow(`SELECT default_msrp_cents, default_currency FROM book_lines WHERE id = ?`, bookLineID).Scan(&msrp, &lineCurrency); err != nil {
		return err
	}
	// A list price in another currency can't sit on a volume priced in the trip's currency.
	if money.NormalizeCurrency(lineCurrency) != money.NormalizeCurrency(p.Currency) {
		msrp = sql.NullInt64{}
	}
	if !anotherCopy {
		var owned []string
		for _, n := range numbers {
			var c int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM volumes WHERE book_line_id = ? AND volume_number = ?`, bookLineID, n).Scan(&c); err != nil {
				return err
			}
			if c > 0 {
				owned = append(owned, n)
			}
		}
		if len(owned) > 0 {
			return &OwnedError{Numbers: owned}
		}
	}
	now := nowUTC()
	for i, n := range numbers {
		if _, err := tx.Exec(`
			INSERT INTO volumes (book_line_id, volume_number, sort_order, purchased_at, store,
				msrp_cents, price_ex_tax_cents, currency, trip_id, receipt_line_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			bookLineID, n, sortOrders[i], p.PurchasedAt, p.Store, msrp, nullInt64Ptr(p.PriceExTaxCents),
			p.Currency, tripID, line, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func itemTable(kind string) (string, error) {
	switch kind {
	case "volume":
		return "volumes", nil
	case "merch":
		return "merch", nil
	}
	return "", fmt.Errorf("unknown item kind %q", kind)
}

// TakeOffTrip unlinks an item from its trip. It keeps its date, store and price.
func (d *DB) TakeOffTrip(kind string, id int64) error {
	table, err := itemTable(kind)
	if err != nil {
		return err
	}
	col := "volume_id"
	if kind == "merch" {
		col = "merch_id"
	}
	// A tag on the trip's photo only makes sense while the item is on the trip.
	if _, err := d.SQL.Exec(`DELETE FROM photo_tags WHERE `+col+` = ?`, id); err != nil {
		return err
	}
	_, err = d.SQL.Exec(`UPDATE `+table+` SET trip_id = NULL, receipt_line_id = NULL, updated_at = ? WHERE id = ?`, nowUTC(), id)
	return err
}

// SetReceiptLine moves an item onto one of its trip's receipt lines, which sets its
// price to that line's price. With lineID 0 it leaves every line and keeps its price.
func (d *DB) SetReceiptLine(kind string, id, lineID int64) error {
	table, err := itemTable(kind)
	if err != nil {
		return err
	}
	tx, err := d.SQL.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var trip sql.NullInt64
	qty := 1
	if kind == "merch" {
		err = tx.QueryRow(`SELECT trip_id, quantity FROM merch WHERE id = ?`, id).Scan(&trip, &qty)
	} else {
		err = tx.QueryRow(`SELECT trip_id FROM volumes WHERE id = ?`, id).Scan(&trip)
	}
	if err != nil {
		return err
	}
	if lineID == 0 {
		if _, err := tx.Exec(`UPDATE `+table+` SET receipt_line_id = NULL WHERE id = ?`, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	if !trip.Valid {
		return ErrWrongTrip
	}
	p, line, err := tripPurchase(tx, trip.Int64, lineID, qty)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE `+table+` SET receipt_line_id = ?, price_ex_tax_cents = ?, price_inc_tax_cents = NULL, updated_at = ? WHERE id = ?`,
		line, nullInt64Ptr(p.PriceExTaxCents), nowUTC(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// UngroupedPurchases counts dated items not on any trip, and how many trips they would make.
func (d *DB) UngroupedPurchases() (items, trips int, err error) {
	err = d.SQL.QueryRow(`
		SELECT COUNT(*), COUNT(DISTINCT purchased_at || char(0) || store || char(0) || currency)
		FROM (
			SELECT purchased_at, store, currency FROM volumes WHERE trip_id IS NULL AND purchased_at IS NOT NULL AND purchased_at != ''
			UNION ALL
			SELECT purchased_at, store, currency FROM merch WHERE trip_id IS NULL AND purchased_at IS NOT NULL AND purchased_at != ''
		)`).Scan(&items, &trips)
	return
}

// GroupIntoTrips makes one trip per day, store and currency for dated items not on a trip.
// Items keep their prices. A trip's total is the sum of its items' prices with tax,
// and is left empty when any item has no price with tax.
func (d *DB) GroupIntoTrips() (trips, items int, err error) {
	tx, err := d.SQL.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	type group struct {
		day, store, currency string
		n, withInc           int
		inc                  int64
	}
	rows, err := tx.Query(`
		SELECT purchased_at, store, currency, COUNT(*), COUNT(price_inc_tax_cents), COALESCE(SUM(price_inc_tax_cents), 0)
		FROM (
			SELECT purchased_at, store, currency, price_inc_tax_cents FROM volumes WHERE trip_id IS NULL AND purchased_at IS NOT NULL AND purchased_at != ''
			UNION ALL
			SELECT purchased_at, store, currency, price_inc_tax_cents FROM merch WHERE trip_id IS NULL AND purchased_at IS NOT NULL AND purchased_at != ''
		)
		GROUP BY 1, 2, 3
		ORDER BY 1, 2, 3`)
	if err != nil {
		return 0, 0, err
	}
	var groups []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.day, &g.store, &g.currency, &g.n, &g.withInc, &g.inc); err != nil {
			rows.Close()
			return 0, 0, err
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	now := nowUTC()
	for _, g := range groups {
		var storeID sql.NullInt64
		if g.store != "" {
			storeID, err = resolveStore(tx, TripWrite{NewStore: &StoreWrite{Name: g.store, Currency: g.currency}})
			if err != nil {
				return 0, 0, err
			}
		}
		var total sql.NullInt64
		if g.withInc == g.n {
			total = sql.NullInt64{Int64: g.inc, Valid: true}
		}
		res, err := tx.Exec(`INSERT INTO trips (store_id, visited_on, currency, paid_cents, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			storeID, g.day, g.currency, total, now, now)
		if err != nil {
			return 0, 0, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, 0, err
		}
		for _, table := range []string{"volumes", "merch"} {
			if _, err := tx.Exec(`UPDATE `+table+` SET trip_id = ? WHERE trip_id IS NULL AND purchased_at = ? AND store = ? AND currency = ?`,
				id, g.day, g.store, g.currency); err != nil {
				return 0, 0, err
			}
		}
		trips++
		items += g.n
	}
	return trips, items, tx.Commit()
}

// Backup writes a complete copy of the database to path.
func (d *DB) Backup(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`VACUUM INTO ?`, path)
	return err
}

// LineChoice is a book line offered when adding books to a trip.
type LineChoice struct {
	BookLine
	SeriesName string
	LastBought sql.NullString
	Owned      []string
}

// LineChoices lists every book line, the ones bought from most recently first.
func (d *DB) LineChoices() ([]LineChoice, error) {
	rows, err := d.SQL.Query(`
		SELECT bl.id, bl.series_id, bl.kind, bl.label, bl.slug, bl.total_volumes, bl.default_msrp_cents,
			bl.default_currency, bl.sort_order, s.name, MAX(v.purchased_at), COALESCE(group_concat(v.volume_number, char(31)), '')
		FROM book_lines bl
		JOIN series s ON s.id = bl.series_id
		LEFT JOIN volumes v ON v.book_line_id = bl.id
		GROUP BY bl.id
		ORDER BY MAX(v.purchased_at) IS NULL, MAX(v.purchased_at) DESC, s.name COLLATE NOCASE, bl.sort_order, bl.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LineChoice
	for rows.Next() {
		var c LineChoice
		var owned string
		if err := rows.Scan(append(scanBookLine(&c.BookLine), &c.SeriesName, &c.LastBought, &owned)...); err != nil {
			return nil, err
		}
		if owned != "" {
			c.Owned = strings.Split(owned, "\x1f")
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReceiptLabels lists labels used on receipts before, the most used first.
func (d *DB) ReceiptLabels() ([]string, error) {
	rows, err := d.SQL.Query(`SELECT MIN(label) FROM receipt_lines GROUP BY lower(label) ORDER BY COUNT(*) DESC, lower(label)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
