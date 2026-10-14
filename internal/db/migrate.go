package db

import (
	"database/sql"
	"strings"
)

func migrate(sqlDB *sql.DB) error {
	if err := ensureLinksAllowWishlist(sqlDB); err != nil {
		return err
	}
	return ensureWishlistMSRP(sqlDB)
}

func ensureWishlistMSRP(sqlDB *sql.DB) error {
	var createSQL string
	err := sqlDB.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'wishlist'`).Scan(&createSQL)
	if err != nil {
		return err
	}
	if !strings.Contains(createSQL, "target_cents") {
		return nil
	}
	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() { _, _ = sqlDB.Exec(`PRAGMA foreign_keys = ON`) }()

	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		CREATE TABLE wishlist_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL CHECK (kind IN ('volumes', 'merch')),
			series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
			book_line_id INTEGER REFERENCES book_lines(id) ON DELETE CASCADE,
			vol_from TEXT NOT NULL DEFAULT '',
			vol_to TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			merch_kind TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 1 CHECK (quantity >= 1),
			msrp_cents INTEGER,
			currency TEXT NOT NULL DEFAULT 'USD',
			notes TEXT NOT NULL DEFAULT '',
			image_path TEXT,
			status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'bought')),
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO wishlist_new (
			id, kind, series_id, book_line_id, vol_from, vol_to, name, merch_kind, quantity,
			msrp_cents, currency, notes, image_path, status, created_at, updated_at
		)
		SELECT
			id, kind, series_id, book_line_id, vol_from, vol_to, name, merch_kind, quantity,
			target_cents, currency, notes, image_path, status, created_at, updated_at
		FROM wishlist`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE wishlist`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE wishlist_new RENAME TO wishlist`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_wishlist_series ON wishlist(series_id, status)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_wishlist_line ON wishlist(book_line_id, status)`); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureLinksAllowWishlist(sqlDB *sql.DB) error {
	var createSQL string
	err := sqlDB.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'links'`).Scan(&createSQL)
	if err != nil {
		return err
	}
	if strings.Contains(createSQL, "'wishlist'") {
		return nil
	}
	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() { _, _ = sqlDB.Exec(`PRAGMA foreign_keys = ON`) }()

	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		CREATE TABLE links_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_type TEXT NOT NULL CHECK (owner_type IN ('series', 'book_line', 'volume', 'volume_number', 'merch', 'wishlist')),
			owner_id INTEGER NOT NULL,
			owner_key TEXT NOT NULL DEFAULT '',
			label TEXT NOT NULL,
			url TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO links_new (id, owner_type, owner_id, owner_key, label, url, created_at)
		SELECT id, owner_type, owner_id, owner_key, label, url, created_at FROM links`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE links`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE links_new RENAME TO links`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_links_owner ON links(owner_type, owner_id, owner_key)`); err != nil {
		return err
	}
	return tx.Commit()
}
