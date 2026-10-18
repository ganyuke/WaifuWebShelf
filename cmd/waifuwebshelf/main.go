package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"waifuwebshelf/internal/config"
	"waifuwebshelf/internal/db"
	"waifuwebshelf/internal/photos"
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

	srv, err := web.New(cfg, store)
	if err != nil {
		_ = store.Close()
		log.Fatal(err)
	}

	if !cfg.AdminEnabled() {
		log.Printf("ADMIN_PASS is not set; /admin is disabled")
	} else {
		log.Printf("admin user %q (form login at /login)", cfg.AdminUser)
	}
	if photos.WebP() {
		log.Printf("new photos are saved as WebP")
	} else {
		log.Printf("new photos are saved as JPEG. Install libwebp to save them as WebP, which are smaller")
	}
	log.Printf("listening on %s (data %s)", cfg.Addr(), cfg.DataDir)

	httpSrv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	// Closing the database on the way out folds pending writes into shelf.db,
	// so a stopped service leaves one complete file to back up.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-stop
		log.Printf("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	err = httpSrv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-done
		err = nil
	}
	if cerr := store.Close(); cerr != nil {
		log.Printf("close database: %v", cerr)
	}
	if err != nil {
		log.Fatal(err)
	}
}
