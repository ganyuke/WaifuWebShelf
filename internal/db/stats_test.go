package db

import (
	"path/filepath"
	"testing"
)

func TestShelfStatsSkipsMissingMSRP(t *testing.T) {
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
	total := int64(2)
	lineID, err := testEdition(store, id, "light_novel", EditionWrite{TotalVolumes: &total})
	if err != nil {
		t.Fatal(err)
	}
	msrp := int64(1500)
	paid := int64(1200)
	if _, err := store.CreateVolume(lineID, "1", 100, Purchase{
		Currency: "CAD", MSRPCents: &msrp, PriceExTaxCents: &paid, PriceIncTaxCents: &paid,
	}); err != nil {
		t.Fatal(err)
	}
	paid2 := int64(999)
	if _, err := store.CreateVolume(lineID, "2", 200, Purchase{
		Currency: "CAD", PriceExTaxCents: &paid2,
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := store.ShelfStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.SeriesCount != 1 || stats.VolumeCount != 2 {
		t.Fatalf("counts %+v", stats)
	}
	if len(stats.ByCurrency) != 1 {
		t.Fatalf("currencies %d", len(stats.ByCurrency))
	}
	tot := stats.ByCurrency[0]
	if tot.MSRPCents != 1500 || tot.ComparableExTaxCents != 1200 || tot.ExTaxCents != 2199 {
		t.Fatalf("money %+v", tot)
	}
	if tot.DeltaCents() != 300 {
		t.Fatalf("delta %d", tot.DeltaCents())
	}
	if tot.MissingMSRP != 1 || tot.WithMSRP != 1 {
		t.Fatalf("msrp coverage %+v", tot)
	}
}

func TestDuplicateVolumePurchases(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	id, err := store.CreateSeries("Rent-A-Girlfriend", "rag", "", nullStr(""))
	if err != nil {
		t.Fatal(err)
	}
	lineID, err := testEdition(store, id, "manga", EditionWrite{})
	if err != nil {
		t.Fatal(err)
	}
	for _, paid := range []int64{200, 1000, 200} {
		p := paid
		if _, err := store.CreateVolume(lineID, "1", 100, Purchase{
			Currency: "CAD", Store: "Book Off", PriceExTaxCents: &p,
		}); err != nil {
			t.Fatal(err)
		}
	}
	vols, err := store.ListVolumesByNumber(lineID, "1")
	if err != nil || len(vols) != 3 {
		t.Fatalf("copies %v %v", vols, err)
	}
	stats, err := store.ShelfStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.VolumeCount != 3 {
		t.Fatalf("volume count %d", stats.VolumeCount)
	}
	if stats.ByCurrency[0].ExTaxCents != 1400 {
		t.Fatalf("paid %d", stats.ByCurrency[0].ExTaxCents)
	}
}

func TestListExpendituresOrder(t *testing.T) {
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
	if _, err := store.CreateVolume(lineID, "2", 200, Purchase{
		PurchasedAt: "2024-06-02", Currency: "CAD", PriceExTaxCents: &paid,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMerch(id, "Holo figure", "figure", 1, Purchase{
		PurchasedAt: "2024-06-01", Currency: "CAD", Store: "AmiAmi", PriceExTaxCents: &paid,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateVolume(lineID, "1", 100, Purchase{
		Currency: "CAD", PriceExTaxCents: &paid,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := store.ListExpenditures()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Kind != "volume" || rows[0].VolumeNumber != "2" || rows[0].PurchasedAt.String != "2024-06-02" {
		t.Fatalf("newest %+v", rows[0])
	}
	if rows[1].Kind != "merch" || rows[1].Name != "Holo figure" {
		t.Fatalf("middle %+v", rows[1])
	}
	if rows[2].Kind != "volume" || rows[2].VolumeNumber != "1" || rows[2].PurchasedAt.Valid {
		t.Fatalf("undated %+v", rows[2])
	}
}
