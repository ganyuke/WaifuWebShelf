package config

import (
	"fmt"
	"os"
	"strings"
)

const DefaultTrustedProxies = "127.0.0.1,::1"

type Config struct {
	Port      string
	DataDir   string
	AdminUser string
	AdminPass string
	// TrustedProxies lists the reverse proxies whose X-Forwarded-* headers are believed.
	TrustedProxies string
	// SiteURL is the public address of the site, used for picture links in chat previews.
	SiteURL string
}

func FromEnv() Config {
	return Config{
		Port:      env("PORT", "8080"),
		DataDir:   env("DATA_DIR", "data"),
		AdminUser: env("ADMIN_USER", "admin"),
		AdminPass: os.Getenv("ADMIN_PASS"),

		TrustedProxies: env("TRUSTED_PROXIES", DefaultTrustedProxies),
		SiteURL:        strings.TrimRight(os.Getenv("SITE_URL"), "/"),
	}
}

func (c Config) Addr() string {
	return ":" + c.Port
}

func (c Config) DBPath() string {
	return c.DataDir + "/shelf.db"
}

// ReceiptsDir holds receipt photos. Unlike ImagesDir it is never served publicly.
func (c Config) ReceiptsDir() string {
	return c.DataDir + "/receipts"
}

func (c Config) ImagesDir() string {
	return c.DataDir + "/images"
}

func (c Config) AdminEnabled() bool {
	return c.AdminPass != ""
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (c Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("PORT is empty")
	}
	if c.DataDir == "" {
		return fmt.Errorf("DATA_DIR is empty")
	}
	return nil
}
