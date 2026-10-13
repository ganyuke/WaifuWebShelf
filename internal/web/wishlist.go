package web

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
)

type WishView struct {
	db.WishlistRow
	LineTitle   string
	Remain      catalog.Remaining
	RemainLabel string
	RemainCount int
	MSRPTotal   *int64
	Links       []LinkView
	// Volumes are the wanted volumes still missing, with a cover when one is stored.
	Volumes []WishVolume
}

type WishVolume struct {
	Number int
	Cover  string
}

// Picture is the image shown for the item: its own, or the first wanted volume's cover.
func (v WishView) Picture() string {
	if v.ImagePath.Valid && v.ImagePath.String != "" {
		return v.ImagePath.String
	}
	for _, vol := range v.Volumes {
		if vol.Cover != "" {
			return vol.Cover
		}
	}
	return ""
}

// Each is the list price of one volume, shown when several are wanted.
func (v WishView) Each() string {
	if v.Kind != "volumes" || v.RemainCount < 2 || !v.MSRPCents.Valid {
		return ""
	}
	return money.Display(v.MSRPCents.Int64, v.Currency)
}

// UnitPrice is the list price of one volume or one merch piece.
func (v WishView) UnitPrice() string {
	if !v.MSRPCents.Valid {
		return ""
	}
	return money.Display(v.MSRPCents.Int64, v.Currency)
}

func (v *WishView) applyCovers(covers map[string]string) {
	v.Volumes = nil
	for _, n := range v.Remain.Numbers {
		v.Volumes = append(v.Volumes, WishVolume{Number: n, Cover: covers[strconv.Itoa(n)]})
	}
}

func (v WishView) Active() bool {
	if v.Status != "open" {
		return false
	}
	if v.Kind == "volumes" {
		return !v.Remain.Empty()
	}
	return true
}

// VolumesLabel reads "volume 3" or "volumes 25–29".
func (v WishView) VolumesLabel() string {
	if v.RemainCount == 1 {
		return "volume " + v.RemainLabel
	}
	return "volumes " + v.RemainLabel
}

func (v WishView) MSRPValid() bool {
	return v.MSRPTotal != nil
}

func (v WishView) MSRPAmount() int64 {
	if v.MSRPTotal == nil {
		return 0
	}
	return *v.MSRPTotal
}

type WishGroup struct {
	SeriesName string
	SeriesSlug string
	SeriesID   int64
	Items      []WishView
}

type wishlistFormData struct {
	ID         int64
	Kind       string
	SeriesID   int64
	SeriesName string
	SeriesSlug string
	BookLineID int64
	LineTitle  string
	Lines      []db.BookLine
	VolFrom    string
	VolTo      string
	Name       string
	MerchKind  string
	Quantity   string
	Currency   string
	MSRP       string
	Notes      string
	Image      string
	New        bool
	Links      []LinkView
}

func (data *wishlistFormData) applyLine(id int64) {
	data.BookLineID = id
	data.LineTitle = ""
	for _, line := range data.Lines {
		if line.ID != id {
			continue
		}
		data.LineTitle = catalog.LineTitle(line.Kind, line.Label)
		return
	}
}

func wishlistFormTitle(data wishlistFormData) string {
	if data.New {
		return "Add to wishlist"
	}
	return "Edit wishlist"
}

func (s *Server) wishView(row db.WishlistRow, owned map[string]bool) WishView {
	v := WishView{
		WishlistRow: row,
		LineTitle:   catalog.LineFullTitle(row.LineKind, row.LineLabel),
	}
	if row.Kind == "volumes" {
		v.Remain = catalog.SpanRemaining(row.VolFrom, row.VolTo, owned)
		v.RemainCount = v.Remain.Count()
		v.RemainLabel = catalog.FormatRemaining(v.Remain)
		if row.MSRPCents.Valid {
			v.MSRPTotal = catalog.RemainingMSRP(&row.MSRPCents.Int64, v.RemainCount)
		}
		return v
	}
	v.RemainCount = row.Quantity
	if row.MSRPCents.Valid {
		v.MSRPTotal = catalog.RemainingMSRP(&row.MSRPCents.Int64, row.Quantity)
	}
	return v
}

func (s *Server) activeWishViews(rows []db.WishlistRow) ([]WishView, error) {
	ownedCache := map[int64]map[string]bool{}
	coverCache := map[int64]map[string]string{}
	var out []WishView
	for _, row := range rows {
		owned := map[string]bool{}
		var covers map[string]string
		if row.Kind == "volumes" && row.BookLineID.Valid {
			id := row.BookLineID.Int64
			if _, ok := ownedCache[id]; !ok {
				keys, err := s.db.LineOwnedKeys(id)
				if err != nil {
					return nil, err
				}
				ownedCache[id] = keys
				lineCovers, err := s.db.LineCovers(id)
				if err != nil {
					return nil, err
				}
				coverCache[id] = lineCovers
			}
			owned = ownedCache[id]
			covers = coverCache[id]
		}
		v := s.wishView(row, owned)
		if !v.Active() {
			continue
		}
		v.applyCovers(covers)
		out = append(out, v)
	}
	return out, nil
}

// Count is how many volumes and merch pieces the group still wants.
func (g WishGroup) Count() int { return wishCount(g.Items) }

func wishCount(items []WishView) int {
	n := 0
	for _, item := range items {
		n += item.RemainCount
	}
	return n
}

func groupWishViews(items []WishView) []WishGroup {
	var groups []WishGroup
	for _, item := range items {
		if len(groups) == 0 || groups[len(groups)-1].SeriesID != item.SeriesID {
			groups = append(groups, WishGroup{SeriesName: item.SeriesName, SeriesSlug: item.SeriesSlug, SeriesID: item.SeriesID})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, item)
	}
	return groups
}

func wishMoneyTotals(items []WishView) []db.MoneyTotals {
	by := map[string]*db.MoneyTotals{}
	var order []string
	for _, item := range items {
		cur := money.NormalizeCurrency(item.Currency)
		t, ok := by[cur]
		if !ok {
			t = &db.MoneyTotals{Currency: cur}
			by[cur] = t
			order = append(order, cur)
		}
		t.ItemCount++
		if item.MSRPTotal != nil {
			t.MSRPCents += *item.MSRPTotal
			t.WithMSRP++
		} else {
			t.MissingMSRP++
		}
	}
	out := make([]db.MoneyTotals, 0, len(order))
	for _, cur := range order {
		out = append(out, *by[cur])
	}
	return out
}

func (s *Server) lineWanted(lineID int64) (catalog.Remaining, error) {
	wants, err := s.db.ListOpenWishlistForLine(lineID)
	if err != nil {
		return catalog.Remaining{}, err
	}
	owned, err := s.db.LineOwnedKeys(lineID)
	if err != nil {
		return catalog.Remaining{}, err
	}
	var parts []catalog.Remaining
	for _, w := range wants {
		parts = append(parts, catalog.SpanRemaining(w.VolFrom, w.VolTo, owned))
	}
	return catalog.MergeRemaining(parts...), nil
}

func (s *Server) wishlist(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.ListOpenWishlist()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	items, err := s.activeWishViews(rows)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	for i := range items {
		ll, err := s.db.ListLinks("wishlist", items[i].ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		items[i].Links = toLinkViews(ll)
	}
	s.render(w, r, "wishlist", "Wishlist", map[string]any{
		"Groups": groupWishViews(items),
		"Count":  wishCount(items),
		"Totals": wishMoneyTotals(items),
	})
}

func (s *Server) wishlistDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	row, err := s.db.GetWishlistRow(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	owned := map[string]bool{}
	var covers map[string]string
	if row.Kind == "volumes" && row.BookLineID.Valid {
		owned, err = s.db.LineOwnedKeys(row.BookLineID.Int64)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		covers, err = s.db.LineCovers(row.BookLineID.Int64)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	v := s.wishView(row, owned)
	if !v.Active() {
		s.notFound(w, r)
		return
	}
	v.applyCovers(covers)
	ll, err := s.db.ListLinks("wishlist", row.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	v.Links = toLinkViews(ll)
	s.renderPreview(w, r, "wishlist_detail", wishTitle(v), Preview{Text: "Wishlist · " + v.SeriesName, Image: v.Picture()}, v)
}

func wishTitle(v WishView) string {
	if v.Kind == "merch" && v.Name != "" {
		return v.Name
	}
	if v.RemainLabel != "" {
		return v.LineTitle + ", " + v.VolumesLabel()
	}
	return "Wishlist"
}

func (s *Server) wishlistNewForm(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.newWishlistForm(id, r)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_wishlist_form", wishlistFormTitle(data), data)
}

func (s *Server) newWishlistForm(seriesID int64, r *http.Request) (wishlistFormData, error) {
	ser, err := s.db.GetSeries(seriesID)
	if err != nil {
		return wishlistFormData{}, err
	}
	lines, err := s.db.ListBookLines(seriesID)
	if err != nil {
		return wishlistFormData{}, err
	}
	kind := r.FormValue("kind")
	if kind == "" {
		kind = r.URL.Query().Get("kind")
	}
	if kind != "volumes" {
		kind = "merch"
	}
	data := wishlistFormData{
		New:        true,
		Kind:       kind,
		SeriesID:   ser.ID,
		SeriesName: ser.Name,
		SeriesSlug: ser.Slug,
		Lines:      lines,
		Quantity:   "1",
		Currency:   money.Default,
		VolFrom:    strings.TrimSpace(r.URL.Query().Get("from")),
		Name:       r.FormValue("name"),
		MerchKind:  r.FormValue("kind_name"),
	}
	if lineID, err := parseID(r.URL.Query().Get("line")); err == nil {
		data.applyLine(lineID)
	}
	if kind == "volumes" && data.BookLineID == 0 && len(lines) == 1 {
		data.applyLine(lines[0].ID)
	}
	if kind == "volumes" && data.BookLineID != 0 {
		for _, line := range lines {
			if line.ID != data.BookLineID {
				continue
			}
			data.Currency = line.DefaultCurrency
			data.MSRP = moneyVal(line.DefaultMSRPCents, line.DefaultCurrency)
			break
		}
	}
	return data, nil
}

func (s *Server) wishlistSaveNew(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	data, err := s.newWishlistForm(id, r)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.fillWishlistForm(&data, r)
	in, img, msg, err := s.parseWishlistWrite(r, data, sql.NullString{})
	if err != nil {
		s.renderErr(w, r, "admin_wishlist_form", wishlistFormTitle(data), msg, data)
		return
	}
	in.ImagePath = img
	wid, err := s.db.CreateWishlist(in)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/wishlist/"+formatID(wid)+"/edit?ok=created", http.StatusSeeOther)
}

func (s *Server) wishlistEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadWishlistForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "admin_wishlist_form", wishlistFormTitle(data), data)
}

func (s *Server) loadWishlistForm(id int64) (wishlistFormData, error) {
	w, err := s.db.GetWishlist(id)
	if err != nil {
		return wishlistFormData{}, err
	}
	ser, err := s.db.GetSeries(w.SeriesID)
	if err != nil {
		return wishlistFormData{}, err
	}
	lines, err := s.db.ListBookLines(w.SeriesID)
	if err != nil {
		return wishlistFormData{}, err
	}
	ll, err := s.db.ListLinks("wishlist", id)
	if err != nil {
		return wishlistFormData{}, err
	}
	to := w.VolTo
	if to == w.VolFrom {
		to = ""
	}
	lineID := int64(0)
	if w.BookLineID.Valid {
		lineID = w.BookLineID.Int64
	}
	data := wishlistFormData{
		ID:         w.ID,
		Kind:       w.Kind,
		SeriesID:   w.SeriesID,
		SeriesName: ser.Name,
		SeriesSlug: ser.Slug,
		Lines:      lines,
		VolFrom:    w.VolFrom,
		VolTo:      to,
		Name:       w.Name,
		MerchKind:  w.MerchKind,
		Quantity:   strconv.Itoa(w.Quantity),
		Currency:   w.Currency,
		MSRP:       moneyVal(w.MSRPCents, w.Currency),
		Notes:      w.Notes,
		Image:      nullImage(w.ImagePath),
		Links:      toLinkViews(ll),
	}
	data.applyLine(lineID)
	return data, nil
}

func (s *Server) wishlistSaveEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := parseMaybeMultipart(r); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	existing, err := s.db.GetWishlist(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data, err := s.loadWishlistForm(id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.fillWishlistForm(&data, r)
	in, img, msg, err := s.parseWishlistWrite(r, data, existing.ImagePath)
	if err != nil {
		s.renderErr(w, r, "admin_wishlist_form", wishlistFormTitle(data), msg, data)
		return
	}
	in.ImagePath = img
	if err := s.db.UpdateWishlist(id, in); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/wishlist/"+formatID(id)+"/edit?ok=updated", http.StatusSeeOther)
}

func (s *Server) wishlistDeleteForm(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadWishlistForm(id)
	if db.IsNotFound(err) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	title := data.Name
	if title == "" {
		title = data.VolFrom
		if data.VolTo != "" {
			title += "–" + data.VolTo
		}
	}
	s.render(w, r, "admin_confirm_delete", "Delete wishlist", map[string]any{
		"Title":  title,
		"Action": "/admin/wishlist/" + r.PathValue("id") + "/delete",
		"Cancel": "/admin/wishlist/" + r.PathValue("id") + "/edit",
		"Body":   "Remove this from the wishlist.",
	})
}

func (s *Server) wishlistDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.db.GetWishlist(id); db.IsNotFound(err) {
		s.notFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.db.DeleteWishlist(id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/wishlist?ok=deleted", http.StatusSeeOther)
}

func (s *Server) fillWishlistForm(data *wishlistFormData, r *http.Request) {
	data.Kind = r.FormValue("kind")
	if data.Kind != "volumes" {
		data.Kind = "merch"
	}
	data.VolFrom = r.FormValue("vol_from")
	data.VolTo = r.FormValue("vol_to")
	data.Name = r.FormValue("name")
	data.MerchKind = r.FormValue("merch_kind")
	data.Quantity = r.FormValue("quantity")
	data.Currency = r.FormValue("currency")
	data.MSRP = r.FormValue("msrp")
	data.Notes = r.FormValue("notes")
	if id, err := parseID(r.FormValue("book_line_id")); err == nil {
		data.applyLine(id)
	}
}

func (s *Server) parseWishlistWrite(r *http.Request, data wishlistFormData, existing sql.NullString) (db.WishlistWrite, sql.NullString, string, error) {
	kind := data.Kind
	if kind != "volumes" && kind != "merch" {
		return db.WishlistWrite{}, existing, "Pick volumes or merch.", errWishlist
	}
	currency := money.NormalizeCurrency(r.FormValue("currency"))
	msrp, err := money.Parse(r.FormValue("msrp"), currency)
	if err != nil {
		return db.WishlistWrite{}, existing, "MSRP: " + err.Error(), err
	}
	in := db.WishlistWrite{
		Kind:      kind,
		SeriesID:  data.SeriesID,
		MSRPCents: msrp,
		Currency:  currency,
		Notes:     strings.TrimSpace(r.FormValue("notes")),
		Quantity:  1,
	}
	if kind == "volumes" {
		from, to, err := catalog.ParseVolumeSpan(data.VolFrom, data.VolTo)
		if err != nil {
			return in, existing, err.Error() + ".", err
		}
		lineID, err := parseID(r.FormValue("book_line_id"))
		if err != nil || lineID == 0 {
			return in, existing, "Pick a book line.", errWishlist
		}
		ok := false
		for _, line := range data.Lines {
			if line.ID == lineID {
				ok = true
				break
			}
		}
		if !ok {
			return in, existing, "Pick a book line.", errWishlist
		}
		in.BookLineID = sql.NullInt64{Int64: lineID, Valid: true}
		in.VolFrom = from
		in.VolTo = to
		return in, existing, "", nil
	}

	name := strings.TrimSpace(data.Name)
	if name == "" {
		return in, existing, "Name is required.", errWishlist
	}
	qty, err := parseQuantity(data.Quantity)
	if err != nil {
		return in, existing, err.Error(), err
	}
	img, err := s.applyImage(r, existing)
	if err != nil {
		return in, existing, err.Error(), err
	}
	in.Name = name
	in.MerchKind = strings.TrimSpace(data.MerchKind)
	in.Quantity = qty
	return in, img, "", nil
}

var errWishlist = errString("wishlist")

type errString string

func (e errString) Error() string { return string(e) }
