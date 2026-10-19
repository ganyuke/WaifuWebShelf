package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func tripFixture(t *testing.T) (*DB, int64) {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	sid, err := d.CreateSeries("Rent-A-Girlfriend", "rag", "", sql.NullString{})
	if err != nil {
		t.Fatal(err)
	}
	msrp := int64(1699)
	lid, err := testEdition(d, sid, "manga", EditionWrite{Language: "en", DefaultMSRPCents: &msrp, DefaultCurrency: "CAD"})
	if err != nil {
		t.Fatal(err)
	}
	return d, lid
}

func ptr(n int64) *int64 { return &n }

func TestTripReceiptPricesAndTotals(t *testing.T) {
	d, lid := tripFixture(t)
	tax := int64(1200)
	tid, err := d.CreateTrip(TripWrite{
		NewStore:  &StoreWrite{Name: "Book Off Robson", Currency: "CAD", TaxBP: &tax},
		VisitedOn: "2026-10-11", Currency: "CAD", PaidCents: ptr(1792),
		Lines: []ReceiptLineWrite{{Label: "Book", UnitCents: 200, Count: 8}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines, err := d.ReceiptLines(tid)
	if err != nil || len(lines) != 1 {
		t.Fatalf("lines %v %v", lines, err)
	}
	if err := d.AddVolumes(tid, lines[0].ID, EditionRef{ID: lid}, []string{"14", "15"}, []int{1400, 1500}, false); err != nil {
		t.Fatal(err)
	}
	vols, err := d.ListVolumes(lid)
	if err != nil || len(vols) != 2 {
		t.Fatalf("vols %v %v", vols, err)
	}
	v := vols[0]
	if v.Store != "Book Off Robson" || v.PurchasedAt.String != "2026-10-11" || v.Currency != "CAD" {
		t.Fatalf("trip fields not copied: %+v", v)
	}
	if v.PriceExTaxCents.Int64 != 200 || v.PriceIncTaxCents.Valid || !v.PlusTax() {
		t.Fatalf("receipt price: %+v", v)
	}
	if v.MSRPCents.Int64 != 1699 {
		t.Fatalf("list price from the book line: %+v", v)
	}

	lines, _ = d.ReceiptLines(tid)
	if lines[0].Entered != 2 {
		t.Fatalf("entered %d", lines[0].Entered)
	}

	err = d.AddVolumes(tid, lines[0].ID, EditionRef{ID: lid}, []string{"15", "16"}, []int{1500, 1600}, false)
	var owned *OwnedError
	if !errors.As(err, &owned) || len(owned.Numbers) != 1 || owned.Numbers[0] != "15" {
		t.Fatalf("want owned 15, got %v", err)
	}
	if vols, _ := d.ListVolumes(lid); len(vols) != 2 {
		t.Fatalf("nothing should be saved when one is owned, have %d", len(vols))
	}

	st, err := d.ShelfStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.ByCurrency) != 1 || st.ByCurrency[0].IncTaxCents != 1792 || st.ByCurrency[0].ExTaxCents != 400 {
		t.Fatalf("paid is the trip total, before tax the receipt prices: %+v", st.ByCurrency)
	}

	// Changing the line's price and the trip's date moves its items with it.
	if err := d.UpdateTrip(tid, TripWrite{StoreID: 1, VisitedOn: "2026-10-12", Currency: "CAD", PaidCents: ptr(1792),
		Lines: []ReceiptLineWrite{{ID: lines[0].ID, Label: "Book", UnitCents: 300, Count: 8}}}); err != nil {
		t.Fatal(err)
	}
	vols, _ = d.ListVolumes(lid)
	if vols[0].PriceExTaxCents.Int64 != 300 || vols[0].PurchasedAt.String != "2026-10-12" {
		t.Fatalf("not synced: %+v", vols[0])
	}

	months, err := d.SpendingByMonth()
	if err != nil || len(months) != 1 || months[0].Key != "2026-10" || months[0].Paid != 1792 || months[0].Visits != 1 {
		t.Fatalf("months %+v %v", months, err)
	}
	totals, err := d.ReceiptTotals()
	if err != nil || len(totals) != 1 || totals[0].Count != 8 || totals[0].Cents != 2400 {
		t.Fatalf("receipt totals %+v %v", totals, err)
	}

	// Deleting the trip keeps the books.
	if err := d.DeleteTrip(tid); err != nil {
		t.Fatal(err)
	}
	vols, _ = d.ListVolumes(lid)
	if len(vols) != 2 || vols[0].TripID.Valid || vols[0].ReceiptLineID.Valid || vols[0].PriceExTaxCents.Int64 != 300 {
		t.Fatalf("after delete: %+v", vols)
	}
}

func TestEmptyVisitAndMissingTotal(t *testing.T) {
	d, _ := tripFixture(t)
	if _, err := d.CreateTrip(TripWrite{NewStore: &StoreWrite{Name: "Book Off", Currency: "CAD"}, VisitedOn: "2026-10-04", Currency: "CAD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateTrip(TripWrite{NewStore: &StoreWrite{Name: "book off", Currency: "CAD"}, VisitedOn: "2026-10-11", Currency: "CAD",
		Lines: []ReceiptLineWrite{{Label: "Keychain", UnitCents: 1200, Count: 1}}}); err != nil {
		t.Fatal(err)
	}
	stores, err := d.ListStores()
	if err != nil || len(stores) != 1 || stores[0].Visits != 2 {
		t.Fatalf("same store by name: %+v %v", stores, err)
	}
	st, _ := d.ShelfStats()
	if len(st.ByCurrency) != 1 || st.ByCurrency[0].TripsWithoutTotal != 1 {
		t.Fatalf("one trip has a receipt with no total: %+v", st.ByCurrency)
	}
	byStore, _ := d.SpendingByStore()
	if len(byStore) != 1 || byStore[0].Visits != 2 || byStore[0].NoTotal != 1 {
		t.Fatalf("by store %+v", byStore)
	}
}

func TestPhotoTags(t *testing.T) {
	d, lid := tripFixture(t)
	a, _ := d.CreateTrip(TripWrite{NewStore: &StoreWrite{Name: "Book Off"}, VisitedOn: "2026-10-11", Currency: "CAD"})
	b, _ := d.CreateTrip(TripWrite{StoreID: 1, VisitedOn: "2026-10-18", Currency: "CAD"})
	if err := d.AddVolumes(a, 0, EditionRef{ID: lid}, []string{"1"}, []int{100}, false); err != nil {
		t.Fatal(err)
	}
	vols, _ := d.ListVolumes(lid)
	if err := d.AddTag(b, "volume", vols[0].ID, 10, 10); !errors.Is(err, ErrNotOnTrip) {
		t.Fatalf("tagging an item from another trip: %v", err)
	}
	if err := d.AddTag(a, "volume", vols[0].ID, 100, 200); err != nil {
		t.Fatal(err)
	}
	// Tagging it again moves the tag.
	if err := d.AddTag(a, "volume", vols[0].ID, 1500, 300); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.TripTags(a)
	if len(tags) != 1 || tags[0].X != 1000 || tags[0].Y != 300 {
		t.Fatalf("tags %+v", tags)
	}
	if err := d.TakeOffTrip("volume", vols[0].ID); err != nil {
		t.Fatal(err)
	}
	if tags, _ := d.TripTags(a); len(tags) != 0 {
		t.Fatalf("tag kept after taking the item off the trip: %+v", tags)
	}
}

func TestAddVolumesNewSeries(t *testing.T) {
	d, lid := tripFixture(t)
	tid, err := d.CreateTrip(TripWrite{
		NewStore:  &StoreWrite{Name: "Book Off Robson", Currency: "CAD"},
		VisitedOn: "2026-10-11", Currency: "CAD",
		Lines: []ReceiptLineWrite{{Label: "Book", UnitCents: 200, Count: 8}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines, _ := d.ReceiptLines(tid)
	fly := EditionRef{SeriesName: "Fly Me to the Moon", NewEdition: NewEdition{Kind: "manga", Language: "en"}}
	if err := d.AddVolumes(tid, lines[0].ID, fly, []string{"1", "2", "3"}, []int{100, 200, 300}, false); err != nil {
		t.Fatal(err)
	}
	// Typing the name again in other case lands on the same series and edition.
	again := EditionRef{SeriesName: "fly me to the moon", NewEdition: NewEdition{Kind: "manga", Language: "en"}}
	if err := d.AddVolumes(tid, lines[0].ID, again, []string{"4"}, []int{400}, false); err != nil {
		t.Fatal(err)
	}
	if err := d.AddVolumes(tid, lines[0].ID, again, []string{"4"}, []int{400}, false); err == nil {
		t.Fatal("want owned error for volume 4")
	}
	series, err := d.ListSeries()
	if err != nil || len(series) != 2 {
		t.Fatalf("series %v %v", series, err)
	}
	var flyID int64
	for _, s := range series {
		if s.Name == "Fly Me to the Moon" {
			flyID = s.ID
		}
	}
	bls, err := d.ListSeriesEditions(flyID)
	if err != nil || len(bls) != 1 || bls[0].Medium != "manga" || bls[0].Language != "en" || bls[0].DefaultCurrency != "CAD" {
		t.Fatalf("editions %+v %v", bls, err)
	}
	if vols, _ := d.ListVolumes(bls[0].ID); len(vols) != 4 || vols[0].PriceExTaxCents.Int64 != 200 {
		t.Fatalf("vols %+v", vols)
	}

	// An existing series with its existing edition adds to that edition.
	rag := EditionRef{SeriesName: "rent-a-girlfriend", NewEdition: NewEdition{Kind: "manga", Language: "en"}}
	if err := d.AddVolumes(tid, lines[0].ID, rag, []string{"9"}, []int{900}, false); err != nil {
		t.Fatal(err)
	}
	if vols, _ := d.ListVolumes(lid); len(vols) != 1 {
		t.Fatalf("want volume 9 on the existing edition, have %d", len(vols))
	}
}
