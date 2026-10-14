PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS series (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    notes TEXT NOT NULL DEFAULT '',
    image_path TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS book_lines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('manga', 'light_novel', 'artbook', 'other')),
    label TEXT NOT NULL DEFAULT '',
    slug TEXT NOT NULL DEFAULT '',
    total_volumes INTEGER,
    default_msrp_cents INTEGER,
    default_currency TEXT NOT NULL DEFAULT 'USD',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS volumes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    book_line_id INTEGER NOT NULL REFERENCES book_lines(id) ON DELETE CASCADE,
    volume_number TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    purchased_at TEXT,
    store TEXT NOT NULL DEFAULT '',
    msrp_cents INTEGER,
    price_ex_tax_cents INTEGER,
    price_inc_tax_cents INTEGER,
    currency TEXT NOT NULL DEFAULT 'USD',
    image_path TEXT,
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS merch (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT '',
    quantity INTEGER NOT NULL DEFAULT 1 CHECK (quantity >= 1),
    purchased_at TEXT,
    store TEXT NOT NULL DEFAULT '',
    msrp_cents INTEGER,
    price_ex_tax_cents INTEGER,
    price_inc_tax_cents INTEGER,
    currency TEXT NOT NULL DEFAULT 'USD',
    image_path TEXT,
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('series', 'book_line', 'volume', 'volume_number', 'merch', 'wishlist')),
    owner_id INTEGER NOT NULL,
    owner_key TEXT NOT NULL DEFAULT '',
    label TEXT NOT NULL,
    url TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS wishlist (
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
);

CREATE INDEX IF NOT EXISTS idx_book_lines_series ON book_lines(series_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_book_lines_slug ON book_lines(series_id, slug);
CREATE INDEX IF NOT EXISTS idx_volumes_book_line ON volumes(book_line_id);
CREATE INDEX IF NOT EXISTS idx_volumes_number ON volumes(book_line_id, volume_number);
CREATE INDEX IF NOT EXISTS idx_merch_series ON merch(series_id);
CREATE INDEX IF NOT EXISTS idx_links_owner ON links(owner_type, owner_id, owner_key);
CREATE INDEX IF NOT EXISTS idx_wishlist_series ON wishlist(series_id, status);
CREATE INDEX IF NOT EXISTS idx_wishlist_line ON wishlist(book_line_id, status);

CREATE TABLE IF NOT EXISTS volume_covers (
    book_line_id INTEGER NOT NULL REFERENCES book_lines(id) ON DELETE CASCADE,
    volume_number TEXT NOT NULL,
    image_path TEXT NOT NULL,
    PRIMARY KEY (book_line_id, volume_number)
);
