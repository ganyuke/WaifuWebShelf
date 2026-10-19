package web

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
	"waifuwebshelf/internal/names"
)

// WorkPage is one work on its series page, with a grid per edition.
type WorkPage struct {
	db.Work
	Heading  string
	Links    []LinkView
	Editions []GridPage
}

// GridPage is one edition's volume grid.
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
	works, err := s.seriesWorks(series.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
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
		"Links":     s.linkViews(slinks),
		"Works":     works,
		"Merch":     merch,
		"WantMerch": wantMerch,
		"Cover":     cover,
	})
}

// seriesWorks builds the series page's works, each with its editions' grids.
func (s *Server) seriesWorks(seriesID int64) ([]WorkPage, error) {
	works, err := s.db.ListWorks(seriesID)
	if err != nil {
		return nil, err
	}
	editions, err := s.db.ListSeriesEditions(seriesID)
	if err != nil {
		return nil, err
	}
	out := make([]WorkPage, 0, len(works))
	for _, wk := range works {
		ll, err := s.db.ListLinks("work", wk.ID)
		if err != nil {
			return nil, err
		}
		page := WorkPage{Work: wk, Heading: names.WorkHeading(wk.Title, wk.Kind), Links: s.linkViews(ll)}
		for _, ed := range editions {
			if ed.WorkID != wk.ID {
				continue
			}
			grid, err := s.editionGrid(ed)
			if err != nil {
				return nil, err
			}
			page.Editions = append(page.Editions, grid)
		}
		out = append(out, page)
	}
	return out, nil
}

func (s *Server) editionGrid(ed db.Edition) (GridPage, error) {
	vols, err := s.db.ListVolumes(ed.ID)
	if err != nil {
		return GridPage{}, err
	}
	ll, err := s.db.ListLinks("edition", ed.ID)
	if err != nil {
		return GridPage{}, err
	}
	grid := GridPage{VolumeGrid: catalog.BuildGrid(ed, vols), Links: s.linkViews(ll)}
	wanted, err := s.editionWanted(ed.ID)
	if err != nil {
		return GridPage{}, err
	}
	grid.ApplyWishlist(wanted)
	covers, err := s.db.EditionCovers(ed.ID)
	if err != nil {
		return GridPage{}, err
	}
	grid.ApplyCovers(covers)
	return grid, nil
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
	ed, err := s.db.GetEditionBySlugs(ser.ID, r.PathValue("workSlug"), r.PathValue("editionSlug"))
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	number := r.PathValue("number")
	copies, err := s.db.ListVolumesByNumber(ed.ID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if len(copies) == 0 {
		s.notFound(w, r)
		return
	}
	volumeLinks, err := s.db.ListLinksKeyed("volume_number", ed.ID, number)
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
		views[i] = CopyView{Volume: c, Links: s.linkViews(ll)}
	}
	info, err := s.db.GetVolumeInfo(ed.ID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	all, err := s.db.ListVolumes(ed.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	prev, next := neighbours(ownedNumbers(all), number)
	var tripIDs []sql.NullInt64
	self := map[int64]bool{}
	for _, c := range copies {
		tripIDs = append(tripIDs, c.TripID)
		self[c.ID] = true
	}
	same, err := s.sameTrips(tripIDs, "volume", self)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	name := ed.Volume(number, info.Title)
	caption := ed.VolumeCaption(number, info.Title)
	s.renderPreview(w, r, "volume_detail", name, Preview{Text: caption, Image: nullImage(info.Cover)}, map[string]any{
		"Series":  ser,
		"Edition": ed,
		"Name":    name,
		"Caption": caption,
		"Number":  number,
		"Copies":  views,
		"Links":   s.linkViews(volumeLinks),
		"Cover":   nullImage(info.Cover),
		"Pager":   volumePager(ed.Naming, prev, next),
		"Same":    same,
	})
}

// Pager links to the previous and next item. A missing one is shown greyed
// out, so the other stays in the same place from page to page.
type Pager struct {
	PrevURL, PrevTitle, NextURL, NextTitle string
}

func (p Pager) Any() bool { return p.PrevURL != "" || p.NextURL != "" }

func volumePager(n names.Naming, prev, next string) Pager {
	var p Pager
	if prev != "" {
		p.PrevURL, p.PrevTitle = n.VolumeURL(prev), "Vol. "+prev
	}
	if next != "" {
		p.NextURL, p.NextTitle = n.VolumeURL(next), "Vol. "+next
	}
	return p
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
	var pager Pager
	for i := range all {
		if all[i].ID != row.ID {
			continue
		}
		if i > 0 {
			pager.PrevURL, pager.PrevTitle = "/merch/"+formatID(all[i-1].ID), all[i-1].Name
		}
		if i+1 < len(all) {
			pager.NextURL, pager.NextTitle = "/merch/"+formatID(all[i+1].ID), all[i+1].Name
		}
		break
	}
	same, err := s.sameTrips([]sql.NullInt64{row.TripID}, "merch", map[int64]bool{row.ID: true})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderPreview(w, r, "merch_detail", row.Name, Preview{Text: row.SeriesName, Image: nullImage(row.ImagePath)}, map[string]any{
		"Same":  same,
		"Merch": row,
		"Links": s.linkViews(ll),
		"Pager": pager,
	})
}

// Trip is one row of the purchases list: a saved trip, or everything bought on
// one day at one store that is not on a trip yet.
type Trip struct {
	ID    int64
	Date  sql.NullString
	Store string
	Count int
	Paid  []string
	Lines []TripLine
	Photo string
	// Receipt is set when a saved trip has receipt lines or a total, so it isn't a visit with nothing bought.
	Receipt bool

	paid  map[string]int64
	order []string
}

// TripLine is one row of a trip: a run of volumes from one edition, or one merch item.
type TripLine struct {
	names.Naming
	Volumes []TripVolume
	Merch   *db.Expenditure
	Paid    string

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
		line := TripLine{Naming: e.Naming, Merch: &e}
		if paid.Valid {
			line.Paid = money.Display(paid.Int64, cur)
		}
		t.Lines = append(t.Lines, line)
		return
	}
	key := e.SeriesSlug + "/" + e.WorkSlug + "/" + e.EditionSlug + "/" + cur
	for i := range t.Lines {
		if t.Lines[i].lineKey == key {
			t.Lines[i].addVolume(e.VolumeNumber, paid)
			return
		}
	}
	line := TripLine{
		Naming:   e.Naming,
		lineKey:  key,
		currency: cur,
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
		if e.TripID.Valid {
			key = "trip\x00" + strconv.FormatInt(e.TripID.Int64, 10)
		}
		t, ok := byKey[key]
		if !ok {
			t = &Trip{ID: e.TripID.Int64, Date: e.PurchasedAt, Store: e.Store, paid: map[string]int64{}}
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
	saved, err := s.db.ListTrips()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	months, err := s.db.SpendingByMonth()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	stores, err := s.db.SpendingByStore()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	receipts, err := s.db.ReceiptTotals()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "stats", "Spending", struct {
		db.ShelfStats
		Trips          []Trip
		ByMonth        []db.SpendRow
		ByStore        []db.SpendRow
		Receipts       []db.ReceiptTotal
		WishCount      int
		WishByCurrency []db.MoneyTotals
	}{stats, mergeTrips(groupTrips(purchases), saved), months, stores, receipts, wishCount(wishItems), wishMoneyTotals(wishItems)})
}

// mergeTrips puts saved trips into the purchases list. A saved trip shows its own
// store and photo, and only its receipt total as paid, never its items added up.
// Trips where nothing was bought get a row too.
func mergeTrips(rows []Trip, saved []db.Trip) []Trip {
	byID := map[int64]int{}
	for i, t := range rows {
		if t.ID != 0 {
			byID[t.ID] = i
		}
	}
	for _, st := range saved {
		i, ok := byID[st.ID]
		if !ok {
			rows = append(rows, Trip{ID: st.ID})
			i = len(rows) - 1
		}
		t := &rows[i]
		t.Date = sql.NullString{String: st.VisitedOn, Valid: true}
		t.Store = st.StoreName
		t.Photo = nullImage(st.PhotoPath)
		t.Receipt = st.LineCount > 0 || st.PaidCents.Valid
		t.Paid = nil
		if st.PaidCents.Valid {
			t.Paid = []string{money.Display(st.PaidCents.Int64, st.Currency)}
		}
		for j := range t.Lines {
			t.Lines[j].Paid = ""
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].Date.String != rows[b].Date.String {
			return rows[a].Date.String > rows[b].Date.String
		}
		return rows[a].ID > rows[b].ID
	})
	return rows
}
