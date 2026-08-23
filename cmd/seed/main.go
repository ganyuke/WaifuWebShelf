package main

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/db"
)

//go:embed merch
var merchFS embed.FS

const seriesSlug = "rent-a-girlfriend"

func main() {
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.ImagesDir(), 0o755); err != nil {
		log.Fatal(err)
	}

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	client := newHTTP()
	log.Print("fetching MangaDex covers")
	mainCovers, err := fetchDexCovers(client, dexMain)
	if err != nil {
		log.Fatal(err)
	}
	shyCovers, err := fetchDexCovers(client, dexShy)
	if err != nil {
		log.Fatal(err)
	}
	anthCovers, err := fetchDexCovers(client, dexAnth)
	if err != nil {
		log.Fatal(err)
	}

	if existing, err := store.GetSeriesBySlug(seriesSlug); err == nil {
		if err := store.DeleteSeries(existing.ID); err != nil {
			log.Fatal(err)
		}
		log.Printf("replaced existing %s", seriesSlug)
	} else if !db.IsNotFound(err) {
		log.Fatal(err)
	}

	seriesImg := sql.NullString{}
	if fn, ok := pickCover(mainCovers, "1", "en", "ja"); ok {
		name, err := saveCover(client, cfg.ImagesDir(), "rag-series.jpg", dexMain, fn)
		if err != nil {
			log.Printf("series cover: %v", err)
		} else {
			seriesImg = sql.NullString{String: name, Valid: true}
		}
	}

	seriesID, err := store.CreateSeries("Rent-A-Girlfriend", seriesSlug, "彼女、お借りします", seriesImg)
	if err != nil {
		log.Fatal(err)
	}

	must(store.CreateLink("series", seriesID, "", "MAL", "https://myanimelist.net/manga/108407/Kanojo_Okarishimasu"))
	must(store.CreateLink("series", seriesID, "", "Kodansha", "https://kodansha.us/series/rent-a-girlfriend/"))
	must(store.CreateLink("series", seriesID, "", "Wikipedia", "https://en.wikipedia.org/wiki/Rent-A-Girlfriend"))

	enTotal, jpTotal, shyTotal, anthTotal := int64(37), int64(46), int64(3), int64(1)
	enMSRP, jpMSRP := int64(1699), int64(770)

	enID := must(store.CreateBookLine(seriesID, db.BookLineWrite{
		Kind: "manga", Label: "English", TotalVolumes: &enTotal,
		DefaultMSRPCents: &enMSRP, DefaultCurrency: "CAD",
	}))
	jpID := must(store.CreateBookLine(seriesID, db.BookLineWrite{
		Kind: "manga", Label: "Japanese", TotalVolumes: &jpTotal,
		DefaultMSRPCents: &jpMSRP, DefaultCurrency: "JPY",
	}))
	shyID := must(store.CreateBookLine(seriesID, db.BookLineWrite{
		Kind: "manga", Label: "Rent-A-(Really Shy!)-Girlfriend", TotalVolumes: &shyTotal,
		DefaultMSRPCents: &enMSRP, DefaultCurrency: "CAD",
	}))
	anthID := must(store.CreateBookLine(seriesID, db.BookLineWrite{
		Kind: "other", Label: "Official Comic Anthology", TotalVolumes: &anthTotal,
		DefaultMSRPCents: &enMSRP, DefaultCurrency: "CAD",
	}))

	must(store.CreateLink("book_line", enID, "", "Kodansha", "https://kodansha.us/series/rent-a-girlfriend/"))
	must(store.CreateLink("book_line", shyID, "", "MAL", "https://myanimelist.net/manga/128534/Kanojo_Hitomishirimasu"))

	enOwned := append(append(seq(1, 12), 14, 16, 17, 18), append(seq(21, 24), 30, 31)...)
	jpOwned := append(seq(1, 8), 12, 15, 18, 20)
	mustErr(addVols(store, enID, seq(1, 12), buy("CAD", "Indigo", "2023-06-11", 1699, 1699, 1924, "")))
	mustErr(addVols(store, enID, []int{14, 16, 17, 18}, buy("CAD", "Book Off", "2024-09-02", 1699, 800, 800, "used")))
	mustErr(addVols(store, enID, seq(21, 24), buy("CAD", "Amazon", "2025-03-18", 1699, 1499, 1694, "")))
	mustErr(addVols(store, enID, []int{30, 31}, buy("CAD", "Kinokuniya", "2026-01-09", 1699, 1699, 1924, "")))

	mustErr(addVols(store, jpID, seq(1, 8), buy("JPY", "Animate", "2024-04-20", 770, 770, 770, "")))
	mustErr(addVols(store, jpID, []int{12, 15, 18, 20}, buy("JPY", "Kinokuniya", "2025-11-03", 770, 770, 770, "")))

	for _, copy := range []db.Purchase{
		buy("CAD", "Book Off", "2024-08-17", 1699, 200, 200, "used"),
		buy("CAD", "Book Off", "2024-08-17", 1699, 1000, 1000, ""),
		buy("CAD", "Book Off", "2025-02-08", 1699, 200, 200, "used"),
	} {
		if _, err := store.CreateVolume(shyID, "1", 100, copy); err != nil {
			log.Fatal(err)
		}
	}
	mustErr(addVols(store, shyID, []int{2}, buy("CAD", "Book Off", "2025-02-08", 1699, 800, 800, "used")))
	mustErr(addVols(store, anthID, []int{1}, buy("CAD", "Kinokuniya", "2023-11-04", 1699, 1699, 1924, "")))

	n := applyCovers(store, client, cfg.ImagesDir(), enID, dexMain, mainCovers, []string{"en", "ja"}, enOwned, "rag-en")
	log.Printf("english covers %d/%d", n, len(enOwned))
	n = applyCovers(store, client, cfg.ImagesDir(), jpID, dexMain, mainCovers, []string{"ja", "en"}, jpOwned, "rag-jp")
	log.Printf("japanese covers %d/%d", n, len(jpOwned))
	n = applyCovers(store, client, cfg.ImagesDir(), shyID, dexShy, shyCovers, []string{"en", "ja"}, []int{1, 2}, "rag-shy")
	log.Printf("shy covers %d/2", n)
	n = applyCovers(store, client, cfg.ImagesDir(), anthID, dexAnth, anthCovers, []string{"ja", "en"}, []int{1}, "rag-anth")
	log.Printf("anthology covers %d/1", n)

	type merchItem struct {
		src, dest, name, kind, date, store, currency string
		msrp, paid                                   int64
	}
	for _, item := range []merchItem{
		{"sumi-bowling.png", "rag-sumi-bowling.png", "Sumi Sakurasawa bowling figure", "figure", "2024-06-08", "AmiAmi", "JPY", 16500, 14850},
		{"sumi-nendoroid.png", "rag-sumi-nendoroid.png", "Sumi Sakurasawa Nendoroid", "nendoroid", "2024-05-12", "AmiAmi", "JPY", 6500, 5850},
		{"sumi-dakimakura.png", "rag-sumi-dakimakura.png", "Sumi Sakurasawa dakimakura", "dakimakura", "2025-01-22", "Mandarake", "JPY", 12000, 8800},
		{"sumi-bunny.png", "rag-sumi-bunny.png", "Sumi Sakurasawa bunny figure", "figure", "2025-04-03", "AmiAmi", "JPY", 28000, 25200},
		{"chizuru-nendoroid.png", "rag-chizuru-nendoroid.png", "Chizuru Mizuhara Nendoroid", "nendoroid", "2024-05-12", "AmiAmi", "JPY", 8900, 7200},
		{"chizuru-bunny.png", "rag-chizuru-bunny.png", "Chizuru Mizuhara bunny figure", "figure", "2025-08-16", "AmiAmi", "JPY", 28800, 25920},
	} {
		img, err := installMerchImage(cfg.ImagesDir(), item.src, item.dest)
		if err != nil {
			log.Printf("merch image %s: %v", item.src, err)
		}
		msrp, paid := item.msrp, item.paid
		must(store.CreateMerch(seriesID, item.name, item.kind, 1, db.Purchase{
			PurchasedAt: item.date, Store: item.store, Currency: item.currency,
			MSRPCents: &msrp, PriceExTaxCents: &paid, PriceIncTaxCents: &paid,
			ImagePath: img,
		}))
	}

	log.Printf("seeded Rent-A-Girlfriend into %s", cfg.DBPath())
}

func applyCovers(store *db.DB, client *http.Client, imagesDir string, lineID int64, mangaID string, covers map[string]map[string]string, locales []string, numbers []int, prefix string) int {
	ok := 0
	for _, n := range numbers {
		fn, found := pickCover(covers, volKey(n), locales...)
		if !found {
			continue
		}
		name := fmt.Sprintf("%s-%d.jpg", prefix, n)
		if _, err := saveCover(client, imagesDir, name, mangaID, fn); err != nil {
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

func installMerchImage(imagesDir, src, dest string) (sql.NullString, error) {
	b, err := merchFS.ReadFile("merch/" + src)
	if err != nil {
		return sql.NullString{}, err
	}
	if err := os.WriteFile(filepath.Join(imagesDir, dest), b, 0o644); err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: dest, Valid: true}, nil
}

func seq(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for n := from; n <= to; n++ {
		out = append(out, n)
	}
	return out
}

func addVols(store *db.DB, lineID int64, numbers []int, p db.Purchase) error {
	for _, n := range numbers {
		if _, err := store.CreateVolume(lineID, strconv.Itoa(n), n*100, p); err != nil {
			return fmt.Errorf("vol %d: %w", n, err)
		}
	}
	return nil
}

func buy(currency, storeName, date string, msrp, ex, inc int64, notes string) db.Purchase {
	p := db.Purchase{Currency: currency, Store: storeName, PurchasedAt: date, Notes: notes}
	if msrp > 0 {
		p.MSRPCents = &msrp
	}
	if ex > 0 {
		p.PriceExTaxCents = &ex
	}
	if inc > 0 {
		p.PriceIncTaxCents = &inc
	}
	return p
}

func must(id int64, err error) int64 {
	if err != nil {
		log.Fatal(err)
	}
	return id
}

func mustErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
