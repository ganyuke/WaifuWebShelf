package web

import (
	"database/sql"
	"net/http"
	"strconv"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
)

type GridPage struct {
	catalog.VolumeGrid
	FullTitle string
	Links     []LinkView
}

type MerchGroup struct {
	SeriesName string
	SeriesSlug string
	SeriesID   int64
	Items      []db.MerchRow
}

// How many entries the home page shows in its side lists and merch grid.
const (
	homeList  = 8
	homeMerch = 14
)

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
	recent, err := s.db.RecentAdditions(homeList)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	merch, err := s.db.ListAllMerch()
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
	preview := Preview{Text: shelfSummary(stats)}
	if len(cards) > 0 {
		preview.Image = cards[0].Cover
	}
	s.renderPreview(w, r, "home", "", preview, map[string]any{
		"Stats":     stats,
		"Series":    cards,
		"Recent":    recent,
		"Merch":     firstN(merch, homeMerch),
		"MerchMore": len(merch) > homeMerch,
		"Wanted":    firstN(wishItems, homeList),
	})
}

func firstN[T any](items []T, n int) []T {
	if len(items) > n {
		return items[:n]
	}
	return items
}

func shelfSummary(stats db.ShelfStats) string {
	return strconv.Itoa(stats.SeriesCount) + " series, " +
		strconv.Itoa(stats.VolumeCount) + " volumes, " +
		strconv.Itoa(stats.MerchPieces) + " merch"
}

func (s *Server) seriesList(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
		grid := GridPage{
			VolumeGrid: catalog.BuildGrid(line, vols),
			FullTitle:  catalog.LineFullTitle(line.Kind, line.Label),
			Links:      toLinkViews(ll),
		}
		wanted, err := s.lineWanted(line.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		grid.ApplyWishlist(wanted)
		covers, err := s.db.LineCovers(line.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		grid.ApplyCovers(covers)
		grids = append(grids, grid)
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
	cover := nullImage(series.ImagePath)
	s.renderPreview(w, r, "series_detail", series.Name, Preview{Image: cover}, map[string]any{
		"Series":    series,
		"Links":     toLinkViews(slinks),
		"Grids":     grids,
		"Merch":     merch,
		"WantMerch": wantMerch,
		"Cover":     cover,
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
	all, err := s.db.ListVolumes(line.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	prev, next := neighbours(ownedNumbers(all), number)
	title := catalog.LineFullTitle(line.Kind, line.Label)
	s.renderPreview(w, r, "volume_detail", ser.Name+" vol. "+number, Preview{Text: title, Image: nullImage(cover)}, map[string]any{
		"Series": ser,
		"Line":   line,
		"Title":  title,
		"Number": number,
		"Copies": views,
		"Links":  toLinkViews(volumeLinks),
		"Cover":  nullImage(cover),
		"Prev":   prev,
		"Next":   next,
	})
}

// ownedNumbers lists each owned volume number once, in shelf order.
func ownedNumbers(vols []db.Volume) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vols {
		if !seen[v.VolumeNumber] {
			seen[v.VolumeNumber] = true
			out = append(out, v.VolumeNumber)
		}
	}
	return out
}

// neighbours returns the entries before and after current, or "" at either end.
func neighbours(items []string, current string) (prev, next string) {
	for i, item := range items {
		if item != current {
			continue
		}
		if i > 0 {
			prev = items[i-1]
		}
		if i+1 < len(items) {
			next = items[i+1]
		}
		break
	}
	return prev, next
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
	all, err := s.db.ListAllMerch()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var prev, next *db.MerchRow
	for i := range all {
		if all[i].ID != row.ID {
			continue
		}
		if i > 0 {
			prev = &all[i-1]
		}
		if i+1 < len(all) {
			next = &all[i+1]
		}
		break
	}
	s.renderPreview(w, r, "merch_detail", row.Name, Preview{Text: row.SeriesName, Image: nullImage(row.ImagePath)}, map[string]any{
		"Merch": row,
		"Links": toLinkViews(ll),
		"Prev":  prev,
		"Next":  next,
	})
}

// Trip is everything bought on one day at one store.
type Trip struct {
	Date  sql.NullString
	Store string
	Count int
	Paid  []string
	Lines []TripLine

	paid  map[string]int64
	order []string
}

// TripLine is one row of a trip: a run of volumes from one book line, or one merch item.
type TripLine struct {
	SeriesName string
	SeriesSlug string
	LineSlug   string
	LineTitle  string
	Volumes    []TripVolume
	Merch      *db.Expenditure
	Paid       string

	lineKey  string
	paid     int64
	hasPaid  bool
	currency string
}

// TripVolume is one volume number in a trip, with how many copies were bought.
type TripVolume struct {
	Number string
	Copies int
}

func (t *Trip) add(e db.Expenditure) {
	t.Count += e.Quantity
	paid := e.Paid()
	cur := money.NormalizeCurrency(e.Currency)
	if paid.Valid {
		if _, ok := t.paid[cur]; !ok {
			t.order = append(t.order, cur)
		}
		t.paid[cur] += paid.Int64
	}
	if e.Kind != "volume" {
		line := TripLine{SeriesName: e.SeriesName, SeriesSlug: e.SeriesSlug, Merch: &e}
		if paid.Valid {
			line.Paid = money.Display(paid.Int64, cur)
		}
		t.Lines = append(t.Lines, line)
		return
	}
	key := e.SeriesSlug + "/" + e.LineSlug + "/" + cur
	for i := range t.Lines {
		if t.Lines[i].lineKey == key {
			t.Lines[i].addVolume(e.VolumeNumber, paid)
			return
		}
	}
	line := TripLine{
		SeriesName: e.SeriesName,
		SeriesSlug: e.SeriesSlug,
		LineSlug:   e.LineSlug,
		LineTitle:  catalog.LineFullTitle(e.LineKind, e.LineLabel),
		lineKey:    key,
		currency:   cur,
	}
	line.addVolume(e.VolumeNumber, paid)
	t.Lines = append(t.Lines, line)
}

func (l *TripLine) addVolume(number string, paid sql.NullInt64) {
	found := false
	for i := range l.Volumes {
		if l.Volumes[i].Number == number {
			l.Volumes[i].Copies++
			found = true
		}
	}
	if !found {
		l.Volumes = append(l.Volumes, TripVolume{Number: number, Copies: 1})
	}
	if paid.Valid {
		l.paid += paid.Int64
		l.hasPaid = true
		l.Paid = money.Display(l.paid, l.currency)
	}
}

// groupTrips turns the purchase list, newest first, into one entry per day and store.
func groupTrips(purchases []db.Expenditure) []Trip {
	var trips []*Trip
	byKey := map[string]*Trip{}
	for _, e := range purchases {
		key := e.PurchasedAt.String + "\x00" + e.Store
		t, ok := byKey[key]
		if !ok {
			t = &Trip{Date: e.PurchasedAt, Store: e.Store, paid: map[string]int64{}}
			byKey[key] = t
			trips = append(trips, t)
		}
		t.add(e)
	}
	out := make([]Trip, len(trips))
	for i, t := range trips {
		for _, cur := range t.order {
			t.Paid = append(t.Paid, money.Display(t.paid[cur], cur))
		}
		// Volumes arrive newest first, and read better counting up.
		for j := range t.Lines {
			v := t.Lines[j].Volumes
			for a, b := 0, len(v)-1; a < b; a, b = a+1, b-1 {
				v[a], v[b] = v[b], v[a]
			}
		}
		// One row already has its price in the trip heading.
		if len(t.Lines) == 1 {
			t.Lines[0].Paid = ""
		}
		out[i] = *t
	}
	return out
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
		Trips          []Trip
		WishCount      int
		WishByCurrency []db.MoneyTotals
	}{stats, groupTrips(purchases), len(wishItems), wishMoneyTotals(wishItems)})
}
