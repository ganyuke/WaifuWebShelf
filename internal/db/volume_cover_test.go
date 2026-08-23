package db

import (
	"path/filepath"
	"testing"
)

func TestVolumeCoverSharedAndPruned(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	id, err := store.CreateSeries("Spice and Wolf", "spice-and-wolf", "", nullStr(""))
	if err != nil {
		t.Fatal(err)
	}
	lineID, err := store.CreateBookLine(id, BookLineWrite{Kind: "light_novel"})
	if err != nil {
		t.Fatal(err)
	}
	paid := int64(1200)
	v1, err := store.CreateVolume(lineID, "1", 100, Purchase{Currency: "CAD", PriceExTaxCents: &paid})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := store.CreateVolume(lineID, "1", 100, Purchase{
		Currency: "CAD", PriceExTaxCents: &paid, ImagePath: nullStr("copy.jpg"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetVolumeCover(lineID, "1", nullStr("cover.jpg")); err != nil {
		t.Fatal(err)
	}

	cover, err := store.VolumeCover(lineID, "1")
	if err != nil || cover.String != "cover.jpg" {
		t.Fatalf("cover %+v %v", cover, err)
	}
	second, err := store.GetVolume(v2)
	if err != nil || second.ImagePath.String != "copy.jpg" {
		t.Fatalf("copy photo %+v %v", second.ImagePath, err)
	}

	if err := store.DeleteVolume(v1); err != nil {
		t.Fatal(err)
	}
	cover, err = store.VolumeCover(lineID, "1")
	if err != nil || cover.String != "cover.jpg" {
		t.Fatalf("kept after one delete %+v %v", cover, err)
	}
	if err := store.DeleteVolume(v2); err != nil {
		t.Fatal(err)
	}
	cover, err = store.VolumeCover(lineID, "1")
	if err != nil || cover.Valid {
		t.Fatalf("pruned %+v %v", cover, err)
	}
}

func TestSetVolumeCover(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	id, err := store.CreateSeries("SAO", "sao", "", nullStr(""))
	if err != nil {
		t.Fatal(err)
	}
	lineID, err := store.CreateBookLine(id, BookLineWrite{Kind: "manga"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetVolumeCover(lineID, "1", nullStr("a.jpg")); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVolumeCover(lineID, "1", nullStr("b.jpg")); err != nil {
		t.Fatal(err)
	}
	cover, err := store.VolumeCover(lineID, "1")
	if err != nil || cover.String != "b.jpg" {
		t.Fatalf("replaced %+v %v", cover, err)
	}
	if err := store.SetVolumeCover(lineID, "1", nullStr("")); err != nil {
		t.Fatal(err)
	}
	cover, err = store.VolumeCover(lineID, "1")
	if err != nil || cover.Valid {
		t.Fatalf("removed %+v %v", cover, err)
	}
}
