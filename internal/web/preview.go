package web

import (
	"net/http"
	"strconv"

	"waifuwebshelf/internal/catalog"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/money"
)

// PreviewCard is the popup shown when a link to the item is hovered: its
// picture and the same details its page shows. Paths mirror the public pages
// under /preview.
type PreviewCard struct {
	Image string
	Title string
	Sub   string
	Note  string
	Facts []PreviewFact
	// Tally lays the facts out as a list of counts, right-aligned.
	Tally bool
}

type PreviewFact struct {
	Label, Value string
}

func (s *Server) renderCard(w http.ResponseWriter, r *http.Request, card PreviewCard) {
	s.renderView(w, r, "preview_card", card.Title, "", Preview{}, card)
}

// cardNotFound answers with an empty body, so no popup appears.
func cardNotFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
}

// boughtText reads like "3 Nov 2025 at Book Off San Diego".
func boughtText(date, store string) string {
	switch {
	case date != "" && store != "":
		return displayDate(date) + " at " + store
	case date != "":
		return displayDate(date)
	}
	return store
}

// purchaseFacts lists what was paid, the way the item's page does. Before
// tax only shows to the owner.
func purchaseFacts(signedIn bool, date, store, currency string, paid, exTax, msrp *int64, plusTax bool) []PreviewFact {
	var facts []PreviewFact
	if b := boughtText(date, store); b != "" {
		facts = append(facts, PreviewFact{"Bought", b})
	}
	if paid != nil {
		v := money.Display(*paid, currency)
		if plusTax {
			v += " plus tax"
		}
		facts = append(facts, PreviewFact{"Paid", v})
	}
	if signedIn && exTax != nil {
		facts = append(facts, PreviewFact{"Before tax", money.Display(*exTax, currency)})
	}
	if msrp != nil {
		facts = append(facts, PreviewFact{"List price", money.Display(*msrp, currency)})
	}
	return facts
}

func ptrOf(valid bool, n int64) *int64 {
	if !valid {
		return nil
	}
	return &n
}

func (s *Server) previewSeries(w http.ResponseWriter, r *http.Request) {
	ser, err := s.db.GetSeriesBySlug(r.PathValue("slug"))
	if db.IsNotFound(err) {
		cardNotFound(w)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	lines, err := s.db.ListBookLines(ser.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	card := PreviewCard{Image: nullImage(ser.ImagePath), Title: ser.Name, Tally: true}
	for _, line := range lines {
		vols, err := s.db.ListVolumes(line.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		owned := len(ownedNumbers(vols))
		var total *int
		if line.TotalVolumes.Valid {
			t := int(line.TotalVolumes.Int64)
			total = &t
		}
		card.Facts = append(card.Facts, PreviewFact{catalog.LineFullTitle(line.Kind, line.Label), catalog.VolumesOwned(owned, total)})
	}
	merch, err := s.db.ListMerch(ser.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if len(merch) > 0 {
		card.Facts = append(card.Facts, PreviewFact{"Merch", strconv.Itoa(len(merch))})
	}
	s.renderCard(w, r, card)
}

func (s *Server) previewVolume(w http.ResponseWriter, r *http.Request) {
	ser, err := s.db.GetSeriesBySlug(r.PathValue("slug"))
	if err == nil {
		var line db.BookLine
		line, err = s.db.GetBookLineBySlug(ser.ID, r.PathValue("lineSlug"))
		if err == nil {
			s.previewLineVolume(w, r, ser, line, normalizeVolumeNumber(r.PathValue("number")))
			return
		}
	}
	if db.IsNotFound(err) {
		cardNotFound(w)
		return
	}
	s.serverError(w, r, err)
}

func (s *Server) previewLineVolume(w http.ResponseWriter, r *http.Request, ser db.Series, line db.BookLine, number string) {
	copies, err := s.db.ListVolumesByNumber(line.ID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cover, err := s.db.VolumeCover(line.ID, number)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	card := PreviewCard{
		Image: nullImage(cover),
		Title: "Volume " + number,
		Sub:   ser.Name + ", " + catalog.LineFullTitle(line.Kind, line.Label),
	}
	signedIn := s.loggedIn(r)
	switch len(copies) {
	case 0:
		card.Note = "Not owned"
		if price, ok, err := s.wantedPrice(line.ID, number); err != nil {
			s.serverError(w, r, err)
			return
		} else if ok {
			card.Note = "On the wishlist"
			if price != "" {
				card.Facts = append(card.Facts, PreviewFact{"List price", price})
			}
		}
	case 1:
		c := copies[0]
		if card.Image == "" {
			card.Image = nullImage(c.ImagePath)
		}
		paid := c.Paid()
		card.Facts = purchaseFacts(signedIn, dateVal(c.PurchasedAt), c.Store, c.Currency,
			ptrOf(paid.Valid, paid.Int64), ptrOf(c.PriceExTaxCents.Valid, c.PriceExTaxCents.Int64),
			ptrOf(c.MSRPCents.Valid, c.MSRPCents.Int64), c.PlusTax())
	default:
		card.Note = strconv.Itoa(len(copies)) + " copies"
		last := copies[len(copies)-1]
		for _, c := range copies {
			if c.PurchasedAt.String > last.PurchasedAt.String {
				last = c
			}
		}
		if b := boughtText(dateVal(last.PurchasedAt), last.Store); b != "" {
			card.Facts = append(card.Facts, PreviewFact{"Last bought", b})
		}
	}
	s.renderCard(w, r, card)
}

// wantedPrice reports whether volume number is on an open wishlist item for
// the line, and its list price for one volume when known.
func (s *Server) wantedPrice(lineID int64, number string) (string, bool, error) {
	n, err := strconv.Atoi(number)
	if err != nil {
		return "", false, nil
	}
	wants, err := s.db.ListOpenWishlistForLine(lineID)
	if err != nil {
		return "", false, err
	}
	owned, err := s.db.LineOwnedKeys(lineID)
	if err != nil {
		return "", false, err
	}
	for _, want := range wants {
		if !catalog.SpanRemaining(want.VolFrom, want.VolTo, owned).NumberSet()[n] {
			continue
		}
		row, err := s.db.GetWishlistRow(want.ID)
		if err != nil {
			return "", false, err
		}
		return s.wishView(row, owned).UnitPrice(), true, nil
	}
	return "", false, nil
}

func (s *Server) previewMerch(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		cardNotFound(w)
		return
	}
	m, err := s.db.GetMerchRow(id)
	if db.IsNotFound(err) {
		cardNotFound(w)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	paid := m.Paid()
	s.renderCard(w, r, PreviewCard{
		Image: nullImage(m.ImagePath),
		Title: m.Name,
		Sub:   m.SeriesName,
		Note:  merchSub(m.Name, m.Kind, m.Quantity),
		Facts: purchaseFacts(s.loggedIn(r), dateVal(m.PurchasedAt), m.Store, m.Currency,
			ptrOf(paid.Valid, paid.Int64), ptrOf(m.PriceExTaxCents.Valid, m.PriceExTaxCents.Int64),
			ptrOf(m.MSRPCents.Valid, m.MSRPCents.Int64), m.PlusTax()),
	})
}

func (s *Server) previewWishlist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		cardNotFound(w)
		return
	}
	row, err := s.db.GetWishlistRow(id)
	if db.IsNotFound(err) {
		cardNotFound(w)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	owned := map[string]bool{}
	var covers map[string]string
	if row.Kind == "volumes" && row.BookLineID.Valid {
		if owned, err = s.db.LineOwnedKeys(row.BookLineID.Int64); err != nil {
			s.serverError(w, r, err)
			return
		}
		if covers, err = s.db.LineCovers(row.BookLineID.Int64); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	v := s.wishView(row, owned)
	if !v.Active() {
		cardNotFound(w)
		return
	}
	v.applyCovers(covers)
	card := PreviewCard{Image: v.Picture(), Title: wishTitle(v), Sub: v.SeriesName, Note: "On the wishlist"}
	if v.MSRPValid() {
		price := money.Display(v.MSRPAmount(), v.Currency)
		if each := v.Each(); each != "" {
			price += " (" + each + " each)"
		}
		card.Facts = append(card.Facts, PreviewFact{"List price", price})
	}
	s.renderCard(w, r, card)
}

func (s *Server) previewTrip(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		cardNotFound(w)
		return
	}
	t, err := s.db.GetTrip(id)
	if db.IsNotFound(err) {
		cardNotFound(w)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	items, err := s.db.TripItems(id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	count := 0
	for _, it := range items {
		count += it.Quantity
	}
	card := PreviewCard{Image: nullImage(t.PhotoPath), Title: tripTitle(t)}
	card.Facts = append(card.Facts, PreviewFact{"Items", strconv.Itoa(count)})
	if t.PaidCents.Valid {
		card.Facts = append(card.Facts, PreviewFact{"Paid", money.Display(t.PaidCents.Int64, t.Currency)})
	}
	s.renderCard(w, r, card)
}
