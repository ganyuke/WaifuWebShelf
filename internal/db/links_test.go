package db

import (
	"path/filepath"
	"testing"
)

func TestVolumeNumberLinksSharedAcrossCopies(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	id, err := store.CreateSeries("Sword Art Online", "sao", "", nullStr(""))
	if err != nil {
		t.Fatal(err)
	}
	lineID, err := store.CreateBookLine(id, BookLineWrite{Kind: "manga"})
	if err != nil {
		t.Fatal(err)
	}
	paid := int64(200)
	v1, err := store.CreateVolume(lineID, "1", 100, Purchase{Currency: "CAD", PriceExTaxCents: &paid})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := store.CreateVolume(lineID, "1", 100, Purchase{Currency: "CAD", PriceExTaxCents: &paid})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateLink("volume_number", lineID, "1", "Yen Press", "https://yenpress.com/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateLink("volume", v1, "", "Receipt", "https://example.com/r"); err != nil {
		t.Fatal(err)
	}

	shared, err := store.ListLinksKeyed("volume_number", lineID, "1")
	if err != nil || len(shared) != 1 || shared[0].Label != "Yen Press" {
		t.Fatalf("shared %v %v", shared, err)
	}
	copyLinks, err := store.ListLinks("volume", v1)
	if err != nil || len(copyLinks) != 1 {
		t.Fatalf("copy links %v %v", copyLinks, err)
	}

	if err := store.DeleteVolume(v1); err != nil {
		t.Fatal(err)
	}
	shared, err = store.ListLinksKeyed("volume_number", lineID, "1")
	if err != nil || len(shared) != 1 {
		t.Fatalf("kept after one delete %v %v", shared, err)
	}

	if err := store.DeleteVolume(v2); err != nil {
		t.Fatal(err)
	}
	shared, err = store.ListLinksKeyed("volume_number", lineID, "1")
	if err != nil || len(shared) != 0 {
		t.Fatalf("pruned %v %v", shared, err)
	}
}
