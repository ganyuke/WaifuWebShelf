package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"strconv"

	"waifuwebshelf/internal/db"
)

// moreSeries were drawn at random from MangaDex's most-followed Japanese
// manga, so the shelf has a spread of sizes instead of one hand-picked series.
// Wandering Witch is there for a light novel next to its manga. Volumes owned,
// stores, and dates are random too, from a fixed seed.
var moreSeries = []struct {
	dexID, name, slug, notes string
	mal                      int
	lines                    []moreLine
}{
	{"37f5cce0-8070-4ada-96e5-fa24b1bd4ff9", "Kaguya-sama: Love Is War", "kaguya-sama", "かぐや様は告らせたい", 90125,
		[]moreLine{{label: "English", total: 28, currency: "CAD", msrp: 1499, isbns: map[int]string{
			1: "9781974702404", 2: "9781974703272", 3: "9781974700325", 4: "9781974700493", 5: "9781974707584", 6: "9781974701384",
			7: "9781974701391", 8: "9781974711420", 9: "9781974705092", 10: "9781974713769", 11: "9781974707799", 12: "9781974717590",
			13: "9781974710713", 14: "9781974714728", 15: "9781974721344", 16: "9781974717101", 17: "9781974723799", 18: "9781974725526",
			19: "9781974722860", 20: "9781974724031", 21: "9781974725182", 22: "9781974728473", 23: "9781974733583", 24: "9781974734481",
			25: "9781974738267", 26: "9781974738755", 27: "9781974741052", 28: "9781974745197",
		}}}},
	{"e18fe8c6-f6dc-4f05-8462-7b2083ff9a6c", "The Apothecary Diaries", "apothecary-diaries", "薬屋のひとりごと", 107562,
		[]moreLine{
			{label: "English", locales: []string{"en"}, currency: "CAD", msrp: 1699, isbns: map[int]string{
				10: "9781646091362", 12: "9781646092963", 13: "9781646093472",
			}},
			{label: "Japanese", locales: []string{"ja"}, currency: "JPY", msrp: 748},
		}},
	{"8352a9ca-22e0-4a1c-bf1f-89f23d95262a", "Campfire Cooking in Another World with My Absurd Skill", "campfire-cooking", "とんでもスキルで異世界放浪メシ", 105747,
		[]moreLine{{label: "English", locales: []string{"en"}, currency: "CAD", msrp: 1699}}},
	{"bd6d0982-0091-4945-ad70-c028ed3c0917", "Mushoku Tensei: Jobless Reincarnation", "mushoku-tensei", "無職転生", 70259,
		[]moreLine{{label: "English", locales: []string{"en"}, currency: "CAD", msrp: 1699, isbns: map[int]string{
			1: "9781626922358", 4: "9781626923423", 5: "9781626924543", 6: "9781626925625", 7: "9781626927230", 8: "9781626929456",
			9: "9781642751192", 10: "9781645052043", 11: "9781645057406", 12: "9781648270772", 13: "9781648272837",
		}}}},
	{"9d3d3403-1a87-4737-9803-bc3d99db1424", "The Guy She Was Interested in Wasn't a Guy at All", "the-guy-she-was-interested-in", "気になってる人が男じゃなかった", 155861,
		[]moreLine{{label: "English", locales: []string{"en"}, currency: "CAD", msrp: 2199, isbns: map[int]string{
			1: "9781975399689", 2: "9798855412598", 3: "9798855427974",
		}}}},
	{"37838287-3fe9-443d-9d22-eafc1089140e", "I Got a Cheat Skill in Another World and Became Unrivaled in the Real World, Too", "cheat-skill-real-world", "異世界でチート能力を手にした俺は、現実世界をも無双する", 124912,
		[]moreLine{{label: "English", locales: []string{"en"}, currency: "CAD", msrp: 1699}}},
	{"b0c50912-623b-4bd7-84fc-7811f7961518", "Wandering Witch: The Journey of Elaina", "wandering-witch", "魔女の旅々", 116831,
		[]moreLine{
			{kind: "light_novel", label: "English", known: 8, currency: "CAD", msrp: 2000, isbns: map[int]string{
				1: "9781975309565", 3: "9781975309589", 4: "9781975309602", 5: "9781975309626", 6: "9781975309640", 7: "9781975309664",
			}},
			{label: "English", total: 6, currency: "CAD", msrp: 1699, isbns: map[int]string{1: "9781646090358", 2: "9781646090365"}},
		}},
}

// moreLine is one format, manga unless kind says otherwise. Without a total
// the series is still running, and known (or the volumes with a cover in any
// language) stands in for the volumes out so far. Covers come from MangaDex
// in the given locales, then from Open Library by ISBN where MangaDex has none.
type moreLine struct {
	kind     string
	label    string
	total    int
	known    int
	locales  []string
	isbns    map[int]string
	currency string
	msrp     int64
}

var moreStores = []string{"Book Off", "Book Off", "Indigo", "Kinokuniya", "Amazon"}

func seedMore(store *db.DB, client *http.Client, imagesDir string) {
	rng := rand.New(rand.NewPCG(20261019, 1))
	for _, s := range moreSeries {
		covers, err := fetchDexCovers(client, s.dexID)
		if err != nil {
			log.Printf("%s covers: %v", s.name, err)
			continue
		}
		replaceSeries(store, s.slug)

		// The series picture is volume 1 of the first format, in that format's language.
		img := sql.NullString{}
		first := s.lines[0]
		if isbn, ok := first.isbns[1]; ok {
			name := s.slug + "-series.jpg"
			if err := download(client, "https://covers.openlibrary.org/b/isbn/"+isbn+"-L.jpg?default=false", filepath.Join(imagesDir, name)); err == nil {
				img = sql.NullString{String: name, Valid: true}
			}
		} else if fn, ok := pickCover(covers, "1", first.locales...); ok {
			if name, err := saveCover(client, imagesDir, s.slug+"-series.jpg", s.dexID, fn); err == nil {
				img = sql.NullString{String: name, Valid: true}
			}
		}
		seriesID := must(store.CreateSeries(s.name, s.slug, s.notes, img))
		must(store.CreateLink("series", seriesID, "", "MAL", fmt.Sprintf("https://myanimelist.net/manga/%d", s.mal)))

		for _, l := range s.lines {
			known := l.total
			if known == 0 {
				known = l.known
			}
			if known == 0 {
				known = coverCount(covers, []string{"ja", "en"})
			}
			if known == 0 {
				continue
			}
			kind := l.kind
			if kind == "" {
				kind = "manga"
			}
			w := db.BookLineWrite{Kind: kind, Label: l.label, DefaultMSRPCents: &l.msrp, DefaultCurrency: l.currency}
			if l.total > 0 {
				total := int64(l.total)
				w.TotalVolumes = &total
			}
			lineID := must(store.CreateBookLine(seriesID, w))

			owned := pickOwned(rng, known)
			buyInBatches(store, rng, lineID, owned, l)
			if next := len(owned) + 1; next <= known && rng.IntN(2) == 0 {
				to := min(known, next+rng.IntN(3))
				msrp := l.msrp
				must(store.CreateWishlist(db.WishlistWrite{
					Kind: "volumes", SeriesID: seriesID, BookLineID: sql.NullInt64{Int64: lineID, Valid: true},
					VolFrom: strconv.Itoa(next), VolTo: strconv.Itoa(to), Quantity: 1, MSRPCents: &msrp, Currency: l.currency,
				}))
			}
			prefix := s.slug + "-" + kind + "-" + l.label
			var n int
			if l.locales != nil {
				n = applyCovers(store, client, imagesDir, lineID, s.dexID, covers, l.locales, seq(1, known), prefix)
			}
			if l.isbns != nil {
				n += applyISBNCovers(store, client, imagesDir, lineID, l.isbns, prefix+"-isbn")
			}
			log.Printf("%s %s %s: %d owned, %d covers", s.name, l.label, kind, len(owned), n)
		}
	}
}

func applyISBNCovers(store *db.DB, client *http.Client, imagesDir string, lineID int64, isbns map[int]string, prefix string) int {
	ok := 0
	for n, isbn := range isbns {
		name := fmt.Sprintf("%s-%d.jpg", prefix, n)
		u := "https://covers.openlibrary.org/b/isbn/" + isbn + "-L.jpg?default=false"
		if err := download(client, u, filepath.Join(imagesDir, name)); err != nil {
			log.Printf("cover %s vol %d: %v", prefix, n, err)
			continue
		}
		if err := store.SetVolumeCover(lineID, volKey(n), sql.NullString{String: name, Valid: true}); err != nil {
			log.Printf("cover %s vol %d: %v", prefix, n, err)
			continue
		}
		ok++
	}
	return ok
}

func replaceSeries(store *db.DB, slug string) {
	existing, err := store.GetSeriesBySlug(slug)
	if db.IsNotFound(err) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	if err := store.DeleteSeries(existing.ID); err != nil {
		log.Fatal(err)
	}
}

func coverCount(covers map[string]map[string]string, locales []string) int {
	n := 0
	for v := 1; ; v++ {
		if _, ok := pickCover(covers, volKey(v), locales...); !ok {
			return n
		}
		n = v
	}
}

// pickOwned reads from volume 1 up to a random point, then skips a few here
// and there, the way a shelf fills up over several store visits.
func pickOwned(rng *rand.Rand, known int) []int {
	upTo := 1 + rng.IntN(known)
	var out []int
	for v := 1; v <= upTo; v++ {
		if v > 2 && rng.IntN(6) == 0 {
			continue
		}
		out = append(out, v)
	}
	return out
}

func buyInBatches(store *db.DB, rng *rand.Rand, lineID int64, owned []int, l moreLine) {
	for len(owned) > 0 {
		k := min(len(owned), 1+rng.IntN(5))
		shop := moreStores[rng.IntN(len(moreStores))]
		if l.currency == "JPY" {
			shop = "Kinokuniya"
		}
		date := fmt.Sprintf("%d-%02d-%02d", 2023+rng.IntN(4), 1+rng.IntN(12), 1+rng.IntN(28))
		if date > "2026-10-01" {
			date = "2026-09-" + date[8:]
		}
		paid := l.msrp
		notes := ""
		if shop == "Book Off" {
			paid = []int64{200, 500, 800}[rng.IntN(3)]
			notes = "used"
		}
		mustErr(addVols(store, lineID, owned[:k], buy(l.currency, shop, date, l.msrp, paid, paid, notes)))
		owned = owned[k:]
	}
}
