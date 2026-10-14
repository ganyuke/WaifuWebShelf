package web

import (
	"net/http"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
)

type GridPage struct {
	catalog.VolumeGrid
	Links []LinkView
}

type MerchGroup struct {
	SeriesName string
	SeriesSlug string
	SeriesID   int64
	Items      []db.MerchRow
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.ShelfStats()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cards, err := s.db.SeriesCards()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "home", "Shelf", map[string]any{
		"Stats":  stats,
		"Series": cards,
	})
}

func (s *Server) seriesList(w http.ResponseWriter, r *http.Request) {
	cards, err := s.db.SeriesCards()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "series_list", "Series", cards)
}

func (s *Server) seriesDetail(w http.ResponseWriter, r *http.Request) {
	series, err := s.db.GetSeriesBySlug(r.PathValue("slug"))
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	lines, err := s.db.ListBookLines(series.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	grids := make([]GridPage, 0, len(lines))
	for _, line := range lines {
		vols, err := s.db.ListVolumes(line.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		ll, err := s.db.ListLinks("book_line", line.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		grids = append(grids, GridPage{
			VolumeGrid: catalog.BuildGrid(line, vols),
			Links:      toLinkViews(ll),
		})
		wanted, err := s.lineWanted(line.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		grids[len(grids)-1].ApplyWishlist(wanted)
	}
	merch, err := s.db.ListMerch(series.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	slinks, err := s.db.ListLinks("series", series.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wants, err := s.db.ListOpenWishlistForSeries(series.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wishItems, err := s.activeWishViews(wants)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var wantMerch []WishView
	for _, item := range wishItems {
		if item.Kind == "merch" {
			wantMerch = append(wantMerch, item)
		}
	}
	s.render(w, r, "series_detail", series.Name, map[string]any{
		"Series":    series,
		"Links":     toLinkViews(slinks),
		"Grids":     grids,
		"Merch":     merch,
		"WantMerch": wantMerch,
		"Cover":     nullImage(series.ImagePath),
	})
}

type CopyView struct {
	db.Volume
	Links []LinkView
}

func (s *Server) volumePage(w http.ResponseWriter, r *http.Request) {
	ser, err := s.db.GetSeriesBySlug(r.PathValue("slug"))
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	line, err := s.db.GetBookLineBySlug(ser.ID, r.PathValue("lineSlug"))
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	number := r.PathValue("number")
	copies, err := s.db.ListVolumesByNumber(line.ID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if len(copies) == 0 {
		s.notFound(w, r)
		return
	}
	volumeLinks, err := s.db.ListLinksKeyed("volume_number", line.ID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	views := make([]CopyView, len(copies))
	for i, c := range copies {
		ll, err := s.db.ListLinks("volume", c.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		views[i] = CopyView{Volume: c, Links: toLinkViews(ll)}
	}
	cover, err := s.db.VolumeCover(line.ID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "volume_detail", ser.Name+" vol. "+number, map[string]any{
		"Series": ser,
		"Line":   line,
		"Title":  catalog.LineTitle(line.Kind, line.Label),
		"Number": number,
		"Copies": views,
		"Links":  toLinkViews(volumeLinks),
		"Cover":  nullImage(cover),
	})
}

func (s *Server) merchList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.ListAllMerch()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var groups []MerchGroup
	for _, item := range rows {
		if len(groups) == 0 || groups[len(groups)-1].SeriesSlug != item.SeriesSlug {
			groups = append(groups, MerchGroup{SeriesName: item.SeriesName, SeriesSlug: item.SeriesSlug, SeriesID: item.SeriesID})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, item)
	}
	s.render(w, r, "merch_list", "Merch", groups)
}

func (s *Server) merchDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	row, err := s.db.GetMerchRow(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	ll, err := s.db.ListLinks("merch", row.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "merch_detail", row.Name, map[string]any{
		"Merch": row,
		"Links": toLinkViews(ll),
	})
}

func (s *Server) statsPage(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.ShelfStats()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	purchases, err := s.db.ListExpenditures()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wants, err := s.db.ListOpenWishlist()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wishItems, err := s.activeWishViews(wants)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "stats", "Spending", struct {
		db.ShelfStats
		Expenditures   []db.Expenditure
		WishCount      int
		WishByCurrency []db.MoneyTotals
	}{stats, purchases, len(wishItems), wishMoneyTotals(wishItems)})
}
