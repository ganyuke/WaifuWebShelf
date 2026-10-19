package catalog

import (
	"database/sql"
	"testing"

	"waifuwebshelf/internal/db"
)

func vols(numbers ...string) []db.Volume {
	out := make([]db.Volume, len(numbers))
	for i, n := range numbers {
		out[i] = db.Volume{ID: int64(i + 1), VolumeNumber: n}
	}
	return out
}

func TestGridKnownTotal(t *testing.T) {
	line := db.Edition{TotalVolumes: sql.NullInt64{Int64: 5, Valid: true}}
	g := BuildGrid(line, vols("1", "3", "8.5"))
	if len(g.Cells) != 5 {
		t.Fatalf("cells %d", len(g.Cells))
	}
	if !g.Cells[0].Owned || g.Cells[1].Owned || !g.Cells[2].Owned {
		t.Fatalf("owned flags %+v %+v %+v", g.Cells[0], g.Cells[1], g.Cells[2])
	}
	if len(g.Extras) != 1 || g.Extras[0].VolumeNumber != "8.5" {
		t.Fatalf("extras %+v", g.Extras)
	}
}

func TestGridDuplicateCopies(t *testing.T) {
	line := db.Edition{TotalVolumes: sql.NullInt64{Int64: 3, Valid: true}}
	g := BuildGrid(line, vols("1", "1", "1"))
	if !g.Cells[0].Owned || len(g.Cells[0].Copies) != 3 {
		t.Fatalf("copies %+v", g.Cells[0])
	}
	if g.Owned != 1 || g.Copies != 3 {
		t.Fatalf("owned %d copies %d", g.Owned, g.Copies)
	}
}

func TestGridUnknownTotal(t *testing.T) {
	line := db.Edition{}
	g := BuildGrid(line, vols("1", "2", "omake"))
	if len(g.Cells) != 2 {
		t.Fatalf("cells %d", len(g.Cells))
	}
	if !g.Cells[0].Owned || !g.Cells[1].Owned {
		t.Fatal("expected both owned")
	}
	if len(g.Extras) != 1 {
		t.Fatalf("extras %d", len(g.Extras))
	}
}

func TestApplyWishlistUnknownTotal(t *testing.T) {
	g := BuildGrid(db.Edition{}, vols("1"))
	g.ApplyWishlist(Remaining{Numbers: []int{8, 9, 10}})
	if len(g.Cells) != 4 {
		t.Fatalf("cells %d", len(g.Cells))
	}
	if g.Cells[0].Number != 1 || !g.Cells[0].Owned || g.Cells[0].Wanted {
		t.Fatalf("owned %+v", g.Cells[0])
	}
	if g.Cells[1].Number != 8 || !g.Cells[1].Wanted {
		t.Fatalf("wanted %+v", g.Cells[1])
	}
}

func TestApplyWishlistKnownTotal(t *testing.T) {
	g := BuildGrid(db.Edition{TotalVolumes: sql.NullInt64{Int64: 3, Valid: true}}, vols("1"))
	g.ApplyWishlist(Remaining{Numbers: []int{2, 3, 9}, Keys: []string{"8.5"}})
	if len(g.Cells) != 3 {
		t.Fatalf("cells %d", len(g.Cells))
	}
	if g.Cells[0].Wanted || !g.Cells[1].Wanted || !g.Cells[2].Wanted {
		t.Fatalf("flags %+v", g.Cells)
	}
	if len(g.WantedExtras) != 1 || g.WantedExtras[0] != "8.5" {
		t.Fatalf("extras %+v", g.WantedExtras)
	}
}

func TestApplyCoversGrowsUnknownTotal(t *testing.T) {
	g := BuildGrid(db.Edition{}, vols("1", "14"))
	g.ApplyCovers(map[string]string{"1": "a.jpg", "37": "b.jpg", "omake": "c.jpg"})
	if len(g.Cells) != 37 {
		t.Fatalf("cells %d", len(g.Cells))
	}
	if g.Cells[0].Cover != "a.jpg" || !g.Cells[13].Owned || g.Cells[36].Number != 37 || g.Cells[36].Cover != "b.jpg" || g.Cells[36].Owned {
		t.Fatalf("cells %+v %+v %+v", g.Cells[0], g.Cells[13], g.Cells[36])
	}
	if g.Owned != 2 {
		t.Fatalf("owned %d", g.Owned)
	}
}

func TestApplyCoversKeepsKnownTotal(t *testing.T) {
	g := BuildGrid(db.Edition{TotalVolumes: sql.NullInt64{Int64: 3, Valid: true}}, vols("1"))
	g.ApplyCovers(map[string]string{"2": "a.jpg", "9": "b.jpg"})
	if len(g.Cells) != 3 || g.Cells[1].Cover != "a.jpg" {
		t.Fatalf("cells %+v", g.Cells)
	}
}
