package db

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCreateItemAtomic(t *testing.T) {
	d, e := Open(filepath.Join(t.TempDir(), "shelf.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	in := ItemWrite{SeriesName: "Atomic", NewEdition: NewEdition{Kind: "manga", Language: "en"}, Number: "1", Purchase: Purchase{Currency: "USD"}}
	// Force an item insert failure after both parent rows were inserted.
	_, e = d.SQL.Exec(`CREATE TRIGGER fail_item BEFORE INSERT ON volumes BEGIN SELECT RAISE(ABORT, 'forced failure'); END`)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = d.CreateItem(in); e == nil {
		t.Fatal("expected insert failure")
	}
	for _, table := range []string{"series", "works", "editions", "volumes"} {
		var n int
		if e = d.SQL.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatalf("%s: count %d, %v", table, n, e)
		}
	}
	d.SQL.Exec(`DROP TRIGGER fail_item`)
	sid, lid, _, e := d.CreateItem(in)
	if e != nil {
		t.Fatal(e)
	}
	in.SeriesID = sid
	in.EditionID = lid
	if _, _, _, e = d.CreateItem(in); !errors.Is(e, ErrOwned) {
		t.Fatalf("duplicate: %v", e)
	}
	in.AnotherCopy = true
	if _, _, _, e = d.CreateItem(in); e != nil {
		t.Fatal(e)
	}
	var n int
	d.SQL.QueryRow(`SELECT COUNT(*) FROM volumes`).Scan(&n)
	if n != 2 {
		t.Fatal(n)
	}
}
