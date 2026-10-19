package db

import (
	"database/sql"
	"errors"
	"strings"

	"waifuwebshelf/internal/money"
	"waifuwebshelf/internal/names"
	"waifuwebshelf/internal/slug"
)

type Work struct {
	ID        int64
	SeriesID  int64
	Title     string
	Kind      string
	Slug      string
	SortOrder int
}

type WorkWrite struct {
	Title string
	Kind  string
}

// Edition is one printing of a work. Its language, name and slug are in Naming.
type Edition struct {
	ID               int64
	WorkID           int64
	SeriesID         int64
	TotalVolumes     sql.NullInt64
	DefaultMSRPCents sql.NullInt64
	DefaultCurrency  string
	SortOrder        int
	names.Naming
}

type EditionWrite struct {
	Language         string
	Name             string
	TotalVolumes     *int64
	DefaultMSRPCents *int64
	DefaultCurrency  string
}

// queryer is a *sql.DB or a *sql.Tx.
type queryer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// namingCols are the edition_names columns that fill a names.Naming, read
// through the given alias.
func namingCols(alias string) string {
	return alias + `.series_name, ` + alias + `.series_slug, ` + alias + `.work_title, ` + alias + `.kind, ` +
		alias + `.work_slug, ` + alias + `.language, ` + alias + `.edition_name, ` + alias + `.edition_slug`
}

func scanNaming(n *names.Naming) []any {
	return []any{&n.SeriesName, &n.SeriesSlug, &n.WorkTitle, &n.Medium, &n.WorkSlug, &n.Language, &n.EditionName, &n.EditionSlug}
}

const workCols = `id, series_id, title, kind, slug, sort_order`

func scanWork(w *Work) []any {
	return []any{&w.ID, &w.SeriesID, &w.Title, &w.Kind, &w.Slug, &w.SortOrder}
}

func (d *DB) ListWorks(seriesID int64) ([]Work, error) {
	rows, err := d.SQL.Query(`SELECT `+workCols+` FROM works WHERE series_id = ? ORDER BY sort_order, id`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Work
	for rows.Next() {
		var w Work
		if err := rows.Scan(scanWork(&w)...); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (d *DB) GetWork(id int64) (Work, error) {
	var w Work
	err := d.SQL.QueryRow(`SELECT `+workCols+` FROM works WHERE id = ?`, id).Scan(scanWork(&w)...)
	return w, err
}

func workSlug(q queryer, seriesID, exceptID int64, in WorkWrite) string {
	return slug.Unique(slug.ForWork(in.Title, in.Kind), func(candidate string) bool {
		var n int
		_ = q.QueryRow(`SELECT COUNT(*) FROM works WHERE series_id = ? AND slug = ? AND id != ?`, seriesID, candidate, exceptID).Scan(&n)
		return n > 0
	})
}

func createWork(q queryer, seriesID int64, in WorkWrite) (int64, error) {
	now := nowUTC()
	res, err := q.Exec(`
		INSERT INTO works (series_id, title, kind, slug, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM works WHERE series_id = ?), ?, ?)`,
		seriesID, strings.TrimSpace(in.Title), in.Kind, workSlug(q, seriesID, 0, in), seriesID, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) CreateWork(seriesID int64, in WorkWrite) (int64, error) {
	return createWork(d.SQL, seriesID, in)
}

func (d *DB) UpdateWork(id int64, in WorkWrite) error {
	w, err := d.GetWork(id)
	if err != nil {
		return err
	}
	_, err = d.SQL.Exec(`UPDATE works SET title = ?, kind = ?, slug = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(in.Title), in.Kind, workSlug(d.SQL, w.SeriesID, id, in), nowUTC(), id)
	return err
}

func (d *DB) DeleteWork(id int64) error {
	if err := deleteEditionLinks(d.SQL, `SELECT id FROM editions WHERE work_id = ?`, id); err != nil {
		return err
	}
	if _, err := d.SQL.Exec(`DELETE FROM links WHERE owner_type = 'work' AND owner_id = ?`, id); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM works WHERE id = ?`, id)
	return err
}

// deleteEditionLinks removes the links on the editions picked by editionIDs,
// a query taking args, and on their volumes and wishlist items.
func deleteEditionLinks(q queryer, editionIDs string, args ...any) error {
	for _, stmt := range []string{
		`DELETE FROM links WHERE owner_type IN ('edition', 'volume_number') AND owner_id IN (` + editionIDs + `)`,
		`DELETE FROM links WHERE owner_type = 'volume' AND owner_id IN (SELECT id FROM volumes WHERE edition_id IN (` + editionIDs + `))`,
		`DELETE FROM links WHERE owner_type = 'wishlist' AND owner_id IN (SELECT id FROM wishlist WHERE edition_id IN (` + editionIDs + `))`,
	} {
		if _, err := q.Exec(stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

const editionCols = `e.id, e.work_id, n.series_id, e.total_volumes, e.default_msrp_cents, e.default_currency, e.sort_order, `

const editionFrom = ` FROM editions e JOIN edition_names n ON n.edition_id = e.id `

func scanEdition(e *Edition) []any {
	return append([]any{&e.ID, &e.WorkID, &e.SeriesID, &e.TotalVolumes, &e.DefaultMSRPCents, &e.DefaultCurrency, &e.SortOrder}, scanNaming(&e.Naming)...)
}

func (d *DB) queryEditions(where string, args ...any) ([]Edition, error) {
	rows, err := d.SQL.Query(`SELECT `+editionCols+namingCols("n")+editionFrom+where+` ORDER BY n.work_order, n.work_id, e.sort_order, e.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edition
	for rows.Next() {
		var e Edition
		if err := rows.Scan(scanEdition(&e)...); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListEditions lists a work's editions.
func (d *DB) ListEditions(workID int64) ([]Edition, error) {
	return d.queryEditions(`WHERE e.work_id = ?`, workID)
}

// ListSeriesEditions lists every edition in a series, work by work.
func (d *DB) ListSeriesEditions(seriesID int64) ([]Edition, error) {
	return d.queryEditions(`WHERE n.series_id = ?`, seriesID)
}

// ListAllEditions lists every edition, series by series.
func (d *DB) ListAllEditions() ([]Edition, error) {
	rows, err := d.SQL.Query(`SELECT ` + editionCols + namingCols("n") + editionFrom +
		` ORDER BY n.series_name COLLATE NOCASE, n.work_order, n.work_id, e.sort_order, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edition
	for rows.Next() {
		var e Edition
		if err := rows.Scan(scanEdition(&e)...); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) GetEdition(id int64) (Edition, error) {
	var e Edition
	err := d.SQL.QueryRow(`SELECT `+editionCols+namingCols("n")+editionFrom+`WHERE e.id = ?`, id).Scan(scanEdition(&e)...)
	return e, err
}

func (d *DB) GetEditionBySlugs(seriesID int64, workSlug, editionSlug string) (Edition, error) {
	var e Edition
	err := d.SQL.QueryRow(`SELECT `+editionCols+namingCols("n")+editionFrom+`WHERE n.series_id = ? AND n.work_slug = ? AND n.edition_slug = ?`,
		seriesID, workSlug, editionSlug).Scan(scanEdition(&e)...)
	return e, err
}

func editionSlug(q queryer, workID, exceptID int64, in EditionWrite) string {
	return slug.Unique(slug.ForEdition(names.LanguageName(in.Language), in.Name), func(candidate string) bool {
		var n int
		_ = q.QueryRow(`SELECT COUNT(*) FROM editions WHERE work_id = ? AND slug = ? AND id != ?`, workID, candidate, exceptID).Scan(&n)
		return n > 0
	})
}

func createEdition(q queryer, workID int64, in EditionWrite) (int64, error) {
	now := nowUTC()
	res, err := q.Exec(`
		INSERT INTO editions (work_id, language, name, slug, total_volumes, default_msrp_cents, default_currency, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM editions WHERE work_id = ?), ?, ?)`,
		workID, in.Language, strings.TrimSpace(in.Name), editionSlug(q, workID, 0, in), nullInt64Ptr(in.TotalVolumes),
		nullInt64Ptr(in.DefaultMSRPCents), money.NormalizeCurrency(in.DefaultCurrency), workID, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) CreateEdition(workID int64, in EditionWrite) (int64, error) {
	return createEdition(d.SQL, workID, in)
}

func (d *DB) UpdateEdition(id int64, in EditionWrite) error {
	e, err := d.GetEdition(id)
	if err != nil {
		return err
	}
	_, err = d.SQL.Exec(`
		UPDATE editions SET language = ?, name = ?, slug = ?, total_volumes = ?, default_msrp_cents = ?, default_currency = ?, updated_at = ?
		WHERE id = ?`,
		in.Language, strings.TrimSpace(in.Name), editionSlug(d.SQL, e.WorkID, id, in), nullInt64Ptr(in.TotalVolumes),
		nullInt64Ptr(in.DefaultMSRPCents), money.NormalizeCurrency(in.DefaultCurrency), nowUTC(), id)
	return err
}

func (d *DB) DeleteEdition(id int64) error {
	if err := deleteEditionLinks(d.SQL, `?`, id); err != nil {
		return err
	}
	_, err := d.SQL.Exec(`DELETE FROM editions WHERE id = ?`, id)
	return err
}

// NewEdition describes an edition to find or make while adding volumes: the
// work by medium and title, and the edition by language and name.
type NewEdition struct {
	Kind     string
	Title    string
	Language string
	Name     string
}

var ErrBadEdition = errors.New("Pick a medium and a language from the lists.")

// findOrCreateEdition returns the series' edition matching ne, making the
// work and the edition when there is none yet.
func findOrCreateEdition(q queryer, seriesID int64, ne NewEdition, currency string) (int64, error) {
	if !names.ValidKind(ne.Kind) || !names.ValidLanguage(ne.Language) {
		return 0, ErrBadEdition
	}
	ne.Title, ne.Name = strings.TrimSpace(ne.Title), strings.TrimSpace(ne.Name)
	var workID int64
	err := q.QueryRow(`SELECT id FROM works WHERE series_id = ? AND kind = ? AND title = ? COLLATE NOCASE ORDER BY id LIMIT 1`,
		seriesID, ne.Kind, ne.Title).Scan(&workID)
	if err == sql.ErrNoRows {
		workID, err = createWork(q, seriesID, WorkWrite{Title: ne.Title, Kind: ne.Kind})
	}
	if err != nil {
		return 0, err
	}
	var id int64
	err = q.QueryRow(`SELECT id FROM editions WHERE work_id = ? AND language = ? AND name = ? COLLATE NOCASE ORDER BY id LIMIT 1`,
		workID, ne.Language, ne.Name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	return createEdition(q, workID, EditionWrite{Language: ne.Language, Name: ne.Name, DefaultCurrency: currency})
}
