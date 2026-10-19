package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestOldDatabaseRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shelf.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE book_lines (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if _, err := Open(path); !errors.Is(err, ErrOldDatabase) {
		t.Fatalf("got %v", err)
	}

	// A new file opens, and opens again.
	fresh := filepath.Join(t.TempDir(), "shelf.db")
	for i := 0; i < 2; i++ {
		d, err := Open(fresh)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		d.Close()
	}
}

func TestWorksAndEditions(t *testing.T) {
	d := openTest(t)
	sid, err := d.CreateSeries("K-On!", "k-on", "", nullStr(""))
	if err != nil {
		t.Fatal(err)
	}
	main, err := d.CreateWork(sid, WorkWrite{Kind: "manga"})
	if err != nil {
		t.Fatal(err)
	}
	shuffle, err := d.CreateWork(sid, WorkWrite{Kind: "manga", Title: "K-On! Shuffle"})
	if err != nil {
		t.Fatal(err)
	}
	en, err := d.CreateEdition(main, EditionWrite{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateEdition(main, EditionWrite{Language: "en", Name: "Omnibus"}); err != nil {
		t.Fatal(err)
	}
	shuffleEn, err := d.CreateEdition(shuffle, EditionWrite{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}

	ed, err := d.GetEdition(shuffleEn)
	if err != nil {
		t.Fatal(err)
	}
	if ed.Volume("1", "") != "K-On! Shuffle vol. 1" || ed.VolumeURL("1") != "/series/k-on/k-on-shuffle/english/1" {
		t.Fatalf("shuffle %q %q", ed.Volume("1", ""), ed.VolumeURL("1"))
	}
	all, err := d.ListSeriesEditions(sid)
	if err != nil || len(all) != 3 || all[0].ID != en || all[1].EditionSlug != "english-omnibus" || all[2].ID != shuffleEn {
		t.Fatalf("editions %+v %v", all, err)
	}
	if got, err := d.GetEditionBySlugs(sid, "manga", "english"); err != nil || got.ID != en {
		t.Fatalf("by slugs %+v %v", got, err)
	}

	// Finding an edition by description reuses the work and the edition.
	id, err := findOrCreateEdition(d.SQL, sid, NewEdition{Kind: "manga", Title: "k-on! shuffle", Language: "en"}, "USD")
	if err != nil || id != shuffleEn {
		t.Fatalf("found %d %v, want %d", id, err, shuffleEn)
	}
	id, err = findOrCreateEdition(d.SQL, sid, NewEdition{Kind: "manga", Title: "K-On! Shuffle", Language: "ja"}, "JPY")
	if err != nil || id == shuffleEn {
		t.Fatalf("new language %d %v", id, err)
	}
	works, _ := d.ListWorks(sid)
	if len(works) != 2 {
		t.Fatalf("works %+v", works)
	}
	if _, err := findOrCreateEdition(d.SQL, sid, NewEdition{Kind: "comic", Language: "en"}, "USD"); !errors.Is(err, ErrBadEdition) {
		t.Fatalf("bad medium %v", err)
	}

	// Deleting a work takes its editions, volumes and links with it.
	if _, err := d.CreateVolume(shuffleEn, "1", 100, Purchase{Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateLink("work", shuffle, "", "MAL", "https://myanimelist.net/manga/1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateLink("edition", shuffleEn, "", "Yen Press", "https://yenpress.com/x"); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteWork(shuffle); err != nil {
		t.Fatal(err)
	}
	var n int
	d.SQL.QueryRow(`SELECT (SELECT COUNT(*) FROM links) + (SELECT COUNT(*) FROM volumes) + (SELECT COUNT(*) FROM editions WHERE work_id = ?)`, shuffle).Scan(&n)
	if n != 0 {
		t.Fatalf("left behind %d rows", n)
	}
}

func TestVolumeTitle(t *testing.T) {
	d := openTest(t)
	sid, _ := d.CreateSeries("Rascal Does Not Dream", "rascal", "", nullStr(""))
	ed, err := testEdition(d, sid, "light_novel", EditionWrite{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetVolumeTitle(ed, "2", "  Rascal Does Not Dream of Petite Devil Kohai "); err != nil {
		t.Fatal(err)
	}
	if err := d.SetVolumeCover(ed, "2", nullStr("kohai.jpg")); err != nil {
		t.Fatal(err)
	}
	info, err := d.GetVolumeInfo(ed, "2")
	if err != nil || info.Title != "Rascal Does Not Dream of Petite Devil Kohai" || info.Cover.String != "kohai.jpg" {
		t.Fatalf("info %+v %v", info, err)
	}
	// Clearing the title keeps the cover, and clearing both leaves no row.
	if err := d.SetVolumeTitle(ed, "2", ""); err != nil {
		t.Fatal(err)
	}
	if info, _ := d.GetVolumeInfo(ed, "2"); info.Title != "" || info.Cover.String != "kohai.jpg" {
		t.Fatalf("after title %+v", info)
	}
	if err := d.SetVolumeCover(ed, "2", nullStr("")); err != nil {
		t.Fatal(err)
	}
	var n int
	d.SQL.QueryRow(`SELECT COUNT(*) FROM volume_info`).Scan(&n)
	if n != 0 {
		t.Fatalf("rows %d", n)
	}
}
