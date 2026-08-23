package main

import (
	"log"
	"net/http"
	"os"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/web"
)

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

	srv, err := web.New(cfg, store)
	if err != nil {
		log.Fatal(err)
	}

	if !cfg.AdminEnabled() {
		log.Printf("ADMIN_PASS is not set; /admin is disabled")
	} else {
		log.Printf("admin user %q (form login at /login)", cfg.AdminUser)
	}
	log.Printf("listening on %s (data %s)", cfg.Addr(), cfg.DataDir)
	if err := http.ListenAndServe(cfg.Addr(), srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
