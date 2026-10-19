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

-- A work is one title in a series with one medium, like the K-On! Shuffle
-- manga. An empty title means the series' own name.
CREATE TABLE IF NOT EXISTS works (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK (kind IN ('manga', 'light_novel', 'artbook', 'other')),
    slug TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- An edition is one printing of a work with its own volume numbers, like the
-- English one or a 3-in-1. Language is a code from names.Languages.
CREATE TABLE IF NOT EXISTS editions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_id INTEGER NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    language TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    slug TEXT NOT NULL,
    total_volumes INTEGER,
    default_msrp_cents INTEGER,
    default_currency TEXT NOT NULL DEFAULT 'USD',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- A store keeps the currency and tax that new trips there start with.
CREATE TABLE IF NOT EXISTS stores (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    currency TEXT NOT NULL DEFAULT 'USD',
    -- Sales tax in hundredths of a percent (1200 = 12%), only used to suggest a trip total.
    tax_bp INTEGER,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- A trip is one store visit: one receipt, possibly with nothing bought.
CREATE TABLE IF NOT EXISTS trips (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    store_id INTEGER REFERENCES stores(id),
    visited_on TEXT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'USD',
    -- What was actually paid, tax included. Never split across items.
    paid_cents INTEGER,
    photo_path TEXT,
    receipt_path TEXT,
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- One printed receipt row, like "Book $2 x 8". Items on it take its unit price, before tax.
CREATE TABLE IF NOT EXISTS receipt_lines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    unit_cents INTEGER NOT NULL CHECK (unit_cents >= 0),
    count INTEGER NOT NULL CHECK (count >= 1),
    sort_order INTEGER NOT NULL DEFAULT 0
);

-- One row per copy owned.
CREATE TABLE IF NOT EXISTS volumes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    edition_id INTEGER NOT NULL REFERENCES editions(id) ON DELETE CASCADE,
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
    trip_id INTEGER REFERENCES trips(id) ON DELETE SET NULL,
    receipt_line_id INTEGER REFERENCES receipt_lines(id) ON DELETE SET NULL,
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
    trip_id INTEGER REFERENCES trips(id) ON DELETE SET NULL,
    receipt_line_id INTEGER REFERENCES receipt_lines(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('series', 'work', 'edition', 'volume', 'volume_number', 'merch', 'wishlist')),
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
    edition_id INTEGER REFERENCES editions(id) ON DELETE CASCADE,
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

-- A tag marks where an item is on its trip's haul photo, in thousandths of the width and height.
CREATE TABLE IF NOT EXISTS photo_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    volume_id INTEGER REFERENCES volumes(id) ON DELETE CASCADE,
    merch_id INTEGER REFERENCES merch(id) ON DELETE CASCADE,
    x INTEGER NOT NULL CHECK (x BETWEEN 0 AND 1000),
    y INTEGER NOT NULL CHECK (y BETWEEN 0 AND 1000),
    CHECK ((volume_id IS NULL) != (merch_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_photo_tags_trip ON photo_tags(trip_id);
CREATE INDEX IF NOT EXISTS idx_trips_visited ON trips(visited_on);
CREATE INDEX IF NOT EXISTS idx_receipt_lines_trip ON receipt_lines(trip_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_works_slug ON works(series_id, slug);
CREATE UNIQUE INDEX IF NOT EXISTS idx_editions_slug ON editions(work_id, slug);
CREATE INDEX IF NOT EXISTS idx_volumes_number ON volumes(edition_id, volume_number);
CREATE INDEX IF NOT EXISTS idx_volumes_trip ON volumes(trip_id);
CREATE INDEX IF NOT EXISTS idx_merch_trip ON merch(trip_id);
CREATE INDEX IF NOT EXISTS idx_merch_series ON merch(series_id);
CREATE INDEX IF NOT EXISTS idx_links_owner ON links(owner_type, owner_id, owner_key);
CREATE INDEX IF NOT EXISTS idx_wishlist_series ON wishlist(series_id, status);
CREATE INDEX IF NOT EXISTS idx_wishlist_edition ON wishlist(edition_id, status);

-- What belongs to a volume number rather than to one copy: its cover and its
-- own title, which replaces "Work vol. N" as its name when set.
CREATE TABLE IF NOT EXISTS volume_info (
    edition_id INTEGER NOT NULL REFERENCES editions(id) ON DELETE CASCADE,
    volume_number TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    cover_path TEXT,
    PRIMARY KEY (edition_id, volume_number)
);

-- Everything a volume's name and address are built from, one row per edition.
CREATE VIEW IF NOT EXISTS edition_names AS
SELECT
    e.id AS edition_id, w.id AS work_id, s.id AS series_id,
    s.name AS series_name, s.slug AS series_slug,
    w.title AS work_title, w.kind AS kind, w.slug AS work_slug,
    e.language AS language, e.name AS edition_name, e.slug AS edition_slug,
    w.sort_order AS work_order, e.sort_order AS edition_order
FROM editions e
JOIN works w ON w.id = e.work_id
JOIN series s ON s.id = w.series_id;
