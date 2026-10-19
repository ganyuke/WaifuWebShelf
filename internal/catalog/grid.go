package catalog

import (
	"sort"
	"strconv"

	"waifuwebshelf/internal/db"
)

type GridCell struct {
	Number int
	Owned  bool
	Wanted bool
	Copies []db.Volume
	Cover  string
}

type VolumeGrid struct {
	Edition      db.Edition
	Cells        []GridCell
	Extras       []db.Volume
	WantedExtras []string
	Owned        int
	Copies       int
	Total        *int
}

func BuildGrid(ed db.Edition, vols []db.Volume) VolumeGrid {
	g := VolumeGrid{
		Edition: ed,
		Copies:  len(vols),
	}
	if ed.TotalVolumes.Valid {
		n := int(ed.TotalVolumes.Int64)
		g.Total = &n
	}

	numeric := map[int][]db.Volume{}
	var extras []db.Volume
	maxNum := 0
	for _, v := range vols {
		n, err := strconv.Atoi(v.VolumeNumber)
		if err != nil || n < 1 {
			extras = append(extras, v)
			continue
		}
		if g.Total != nil && n > *g.Total {
			extras = append(extras, v)
			continue
		}
		numeric[n] = append(numeric[n], v)
		if n > maxNum {
			maxNum = n
		}
	}

	end := 0
	if g.Total != nil {
		end = *g.Total
	} else {
		end = maxNum
	}
	for i := 1; i <= end; i++ {
		cell := GridCell{Number: i}
		if copies, ok := numeric[i]; ok {
			cell.Owned = true
			cell.Copies = copies
		}
		g.Cells = append(g.Cells, cell)
	}
	g.Extras = extras
	g.Owned = len(numeric)
	return g
}

// ApplyCovers sets each cell's cover from a map of volume number to image path.
// When the total is unknown, the grid grows to the highest volume with a
// cover, since a cover shows that volume exists.
func (g *VolumeGrid) ApplyCovers(covers map[string]string) {
	if g.Total == nil {
		present := map[int]bool{}
		maxNum := 0
		for _, c := range g.Cells {
			present[c.Number] = true
			maxNum = max(maxNum, c.Number)
		}
		for number := range covers {
			if n, err := strconv.Atoi(number); err == nil && n > maxNum {
				maxNum = n
			}
		}
		for n := 1; n <= maxNum; n++ {
			if !present[n] {
				g.Cells = append(g.Cells, GridCell{Number: n})
			}
		}
		sort.Slice(g.Cells, func(i, j int) bool { return g.Cells[i].Number < g.Cells[j].Number })
	}
	for i := range g.Cells {
		g.Cells[i].Cover = covers[strconv.Itoa(g.Cells[i].Number)]
	}
}

// ProgressText says how much of the line is owned, e.g. "22 of 37 volumes".
func (g VolumeGrid) ProgressText() string {
	text := VolumesOwned(g.Owned, g.Total)
	if g.Copies > g.Owned+len(g.Extras) {
		text += ", " + itoa(g.Copies) + " copies in total"
	}
	return text
}

func (g VolumeGrid) Percent() int {
	return Percent(g.Owned, g.Total)
}

func (g VolumeGrid) HasWanted() bool {
	if len(g.WantedExtras) > 0 {
		return true
	}
	for _, c := range g.Cells {
		if c.Wanted {
			return true
		}
	}
	return false
}

func (g VolumeGrid) HasMissing() bool {
	for _, c := range g.Cells {
		if !c.Owned && !c.Wanted {
			return true
		}
	}
	return false
}

func VolumesOwned(owned int, total *int) string {
	if total != nil && *total == 1 {
		return itoa(owned) + " of 1 volume"
	}
	if total != nil {
		return itoa(owned) + " of " + itoa(*total) + " volumes"
	}
	if owned == 1 {
		return "1 volume"
	}
	return itoa(owned) + " volumes"
}

// Percent is how much of a line is owned, or -1 when its length is unknown.
func Percent(owned int, total *int) int {
	if total == nil || *total <= 0 {
		return -1
	}
	p := owned * 100 / *total
	if p > 100 {
		p = 100
	}
	return p
}

func (g *VolumeGrid) ApplyWishlist(wanted Remaining) {
	set := wanted.NumberSet()
	present := map[int]bool{}
	for _, c := range g.Cells {
		present[c.Number] = true
	}
	for n := range set {
		if g.Total != nil && n > *g.Total {
			continue
		}
		if present[n] {
			continue
		}
		g.Cells = append(g.Cells, GridCell{Number: n})
		present[n] = true
	}
	sort.Slice(g.Cells, func(i, j int) bool { return g.Cells[i].Number < g.Cells[j].Number })
	for i := range g.Cells {
		if !g.Cells[i].Owned && set[g.Cells[i].Number] {
			g.Cells[i].Wanted = true
		}
	}
	g.WantedExtras = append([]string(nil), wanted.Keys...)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
