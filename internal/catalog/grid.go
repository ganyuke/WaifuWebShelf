package catalog

import (
	"strconv"

	"waifuwebshelf/internal/db"
)

type GridCell struct {
	Number int
	Owned  bool
	Copies []db.Volume
}

type VolumeGrid struct {
	Line    db.BookLine
	Title   string
	Cells   []GridCell
	Extras  []db.Volume
	Owned   int
	Copies  int
	Total   *int
	Caption string
}

func BuildGrid(line db.BookLine, vols []db.Volume) VolumeGrid {
	g := VolumeGrid{
		Line:   line,
		Title:  LineTitle(line.Kind, line.Label),
		Copies: len(vols),
	}
	if line.TotalVolumes.Valid {
		n := int(line.TotalVolumes.Int64)
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
	g.Caption = progressCaption(g.Title, g.Owned, g.Total, g.Copies, len(extras))
	return g
}

func progressCaption(title string, uniqueOwned int, total *int, copies, extras int) string {
	base := title + "  "
	if total != nil {
		base += itoa(uniqueOwned) + " / " + itoa(*total)
	} else if copies == 0 {
		base += "0 owned"
	} else {
		base += itoa(uniqueOwned) + " owned"
	}
	if copies > uniqueOwned+extras {
		base += " · " + itoa(copies) + " copies"
	}
	return base
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func ProgressLabel(owned int, total *int) string {
	if total != nil {
		return itoa(owned) + " / " + itoa(*total)
	}
	return itoa(owned) + " owned"
}
