package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port      string
	DataDir   string
	AdminUser string
	AdminPass string
}

func FromEnv() Config {
	return Config{
		Port:      env("PORT", "8080"),
		DataDir:   env("DATA_DIR", "data"),
		AdminUser: env("ADMIN_USER", "admin"),
		AdminPass: os.Getenv("ADMIN_PASS"),
	}
}

func (c Config) Addr() string {
	return ":" + c.Port
}

func (c Config) DBPath() string {
	return c.DataDir + "/shelf.db"
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
