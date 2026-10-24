package db

import (
	"path/filepath"
	"testing"
)

func TestVolumeCoverSharedAndKept(t *testing.T) {
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
	lineID, err := testEdition(store, id, "light_novel", EditionWrite{})
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
	if err != nil || cover.String != "cover.jpg" {
		t.Fatalf("kept after the last copy is deleted %+v %v", cover, err)
	}
	covers, err := store.EditionCovers(lineID)
	if err != nil || covers["1"] != "cover.jpg" {
		t.Fatalf("spare covers %v %v", covers, err)
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
	lineID, err := testEdition(store, id, "manga", EditionWrite{})
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

func TestSeriesCover(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	covered := func(name, slug, image, cover string) Series {
		id, err := store.CreateSeries(name, slug, "", nullStr(image))
		if err != nil {
			t.Fatal(err)
		}
		lineID, err := testEdition(store, id, "manga", EditionWrite{})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetVolumeCover(lineID, "1", nullStr(cover)); err != nil {
			t.Fatal(err)
		}
		s, err := store.GetSeries(id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	kaguya := covered("Kaguya-sama", "kaguya-sama", "", "kaguya-1.jpg")
	bocchi := covered("Bocchi the Rock!", "bocchi-the-rock", "", "bocchi-1.jpg")
	frieren := covered("Frieren", "frieren", "frieren.jpg", "frieren-1.jpg")
	empty, err := store.CreateSeries("Oshi no Ko", "oshi-no-ko", "", nullStr(""))
	if err != nil {
		t.Fatal(err)
	}
	none, err := store.GetSeries(empty)
	if err != nil {
		t.Fatal(err)
	}

	cards, err := store.SeriesCards()
	if err != nil {
		t.Fatal(err)
	}
	table := map[int64]string{}
	for _, c := range cards {
		table[c.ID] = c.Cover
	}
	for _, tc := range []struct {
		s    Series
		want string
	}{{kaguya, "kaguya-1.jpg"}, {bocchi, "bocchi-1.jpg"}, {frieren, "frieren.jpg"}, {none, ""}} {
		got, err := store.SeriesCover(tc.s)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want || got != table[tc.s.ID] {
			t.Errorf("%s: page cover %q, table cover %q, want %q", tc.s.Name, got, table[tc.s.ID], tc.want)
		}
	}
}
