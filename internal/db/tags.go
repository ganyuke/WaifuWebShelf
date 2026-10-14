package db

import (
	"database/sql"
	"errors"
)

// PhotoTag marks where an item is on a trip's haul photo. X and Y are in
// thousandths of the photo's width and height, from the top left.
type PhotoTag struct {
	ID     int64
	TripID int64
	Kind   string
	ItemID int64
	X, Y   int
}

var ErrNotOnTrip = errors.New("This item isn't on this trip.")

func (d *DB) TripTags(tripID int64) ([]PhotoTag, error) {
	rows, err := d.SQL.Query(`
		SELECT id, trip_id, CASE WHEN volume_id IS NOT NULL THEN 'volume' ELSE 'merch' END,
			COALESCE(volume_id, merch_id), x, y
		FROM photo_tags WHERE trip_id = ? ORDER BY y, x, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PhotoTag
	for rows.Next() {
		var t PhotoTag
		if err := rows.Scan(&t.ID, &t.TripID, &t.Kind, &t.ItemID, &t.X, &t.Y); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AddTag tags an item on its trip's photo. Tagging an item again moves its tag.
func (d *DB) AddTag(tripID int64, kind string, itemID int64, x, y int) error {
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
	if err := tx.QueryRow(`SELECT trip_id FROM `+table+` WHERE id = ?`, itemID).Scan(&trip); err != nil {
		return err
	}
	if !trip.Valid || trip.Int64 != tripID {
		return ErrNotOnTrip
	}
	col := "volume_id"
	if kind == "merch" {
		col = "merch_id"
	}
	if _, err := tx.Exec(`DELETE FROM photo_tags WHERE `+col+` = ?`, itemID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO photo_tags (trip_id, `+col+`, x, y) VALUES (?, ?, ?, ?)`, tripID, itemID, clampPM(x), clampPM(y)); err != nil {
		return err
	}
	return tx.Commit()
}

func clampPM(n int) int {
	return max(0, min(1000, n))
}

// DeleteTag removes a tag and says which trip it was on.
func (d *DB) DeleteTag(id int64) (int64, error) {
	var trip int64
	if err := d.SQL.QueryRow(`SELECT trip_id FROM photo_tags WHERE id = ?`, id).Scan(&trip); err != nil {
		return 0, err
	}
	_, err := d.SQL.Exec(`DELETE FROM photo_tags WHERE id = ?`, id)
	return trip, err
}
