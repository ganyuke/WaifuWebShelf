package db

import (
	"path/filepath"
	"testing"
)

func TestWishlistVolumeRangeAndMerch(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sid, err := store.CreateSeries("Spice and Wolf", "spice-and-wolf", "", nullStr(""))
	if err != nil {
		t.Fatal(err)
	}
	total := int64(3)
	lineID, err := testEdition(store, sid, "light_novel", EditionWrite{TotalVolumes: &total})
	if err != nil {
		t.Fatal(err)
	}
	paid := int64(1200)
	if _, err := store.CreateVolume(lineID, "1", 100, Purchase{Currency: "CAD", PriceExTaxCents: &paid}); err != nil {
		t.Fatal(err)
	}
	target := int64(1400)
	wid, err := store.CreateWishlist(WishlistWrite{
		Kind: "volumes", SeriesID: sid,
		EditionID: nullInt64Ptr(&lineID),
		VolFrom:   "1",
		VolTo:     "3",
		Quantity:  1,
		MSRPCents: &target,
		Currency:  "CAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if wid == 0 {
		t.Fatal("id")
	}

	open, err := store.ListOpenWishlistForEdition(lineID)
	if err != nil || len(open) != 1 {
		t.Fatalf("open %v %v", open, err)
	}
	owned, err := store.EditionOwnedKeys(lineID)
	if err != nil {
		t.Fatal(err)
	}
	if !owned["1"] || owned["2"] {
		t.Fatalf("owned %+v", owned)
	}

	if _, err := store.CreateVolume(lineID, "2", 200, Purchase{Currency: "CAD", PriceExTaxCents: &paid}); err != nil {
		t.Fatal(err)
	}
	owned, err = store.EditionOwnedKeys(lineID)
	if err != nil {
		t.Fatal(err)
	}
	if !owned["2"] {
		t.Fatal("expected vol 2 owned")
	}

	mid, err := store.CreateWishlist(WishlistWrite{
		Kind: "merch", SeriesID: sid, Name: "Holo 1/7", MerchKind: "figure",
		Quantity: 1, MSRPCents: &target, Currency: "CAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateLink("wishlist", mid, "", "MFC", "https://myfigurecollection.net/item/1"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkWishlistBought(mid); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListOpenWishlist()
	if err != nil || len(rows) != 1 || rows[0].Kind != "volumes" {
		t.Fatalf("open after merch bought %v %v", rows, err)
	}

	if err := store.DeleteWishlist(open[0].ID); err != nil {
		t.Fatal(err)
	}
	links, err := store.ListLinks("wishlist", mid)
	if err != nil || len(links) != 1 {
		t.Fatalf("kept merch links %v %v", links, err)
	}
	if err := store.DeleteWishlist(mid); err != nil {
		t.Fatal(err)
	}
	links, err = store.ListLinks("wishlist", mid)
	if err != nil || len(links) != 0 {
		t.Fatalf("pruned %v %v", links, err)
	}
}
