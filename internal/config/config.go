package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultTrustedProxies = "127.0.0.1,::1"

type Config struct {
	// Host is the address to listen on. Empty means every interface.
	Host      string
	Port      string
	DataDir   string
	AdminUser string
	AdminPass string
	// DisableLogin makes the site view-only. AdminPass is then ignored.
	DisableLogin bool
	// disableLoginRaw keeps what DISABLE_LOGIN said, for Validate.
	disableLoginRaw string
	// TrustedProxies lists the reverse proxies whose X-Forwarded-* headers are believed.
	TrustedProxies string
	// SiteURL is the public address of the site, used for picture links in chat previews.
	SiteURL string
}

func FromEnv() Config {
	c := Config{
		Host:      os.Getenv("HOST"),
		Port:      env("PORT", "8080"),
		DataDir:   env("DATA_DIR", "data"),
		AdminUser: env("ADMIN_USER", "admin"),
		AdminPass: os.Getenv("ADMIN_PASS"),

		disableLoginRaw: os.Getenv("DISABLE_LOGIN"),

		TrustedProxies: env("TRUSTED_PROXIES", DefaultTrustedProxies),
		SiteURL:        strings.TrimRight(os.Getenv("SITE_URL"), "/"),
	}
	c.DisableLogin, _ = strconv.ParseBool(c.disableLoginRaw)
	return c
}

func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, c.Port)
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

func (c Config) LoginEnabled() bool {
	return !c.DisableLogin
}

// AdminPassPath is where a generated admin password is kept.
func (c Config) AdminPassPath() string {
	return filepath.Join(c.DataDir, "admin-password")
}

// ResolveAdminPass fills in AdminPass when login is on and ADMIN_PASS is
// unset: from the saved file, or a new random one that is then saved.
// generated reports a new one. With login off AdminPass is cleared.
func (c *Config) ResolveAdminPass() (generated bool, err error) {
	if c.DisableLogin {
		c.AdminPass = ""
		return false, nil
	}
	if c.AdminPass != "" {
		return false, nil
	}
	b, err := os.ReadFile(c.AdminPassPath())
	if err == nil {
		if c.AdminPass = strings.TrimSpace(string(b)); c.AdminPass != "" {
			return false, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	var buf [15]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return false, err
	}
	c.AdminPass = base64.RawURLEncoding.EncodeToString(buf[:])
	if err := os.WriteFile(c.AdminPassPath(), []byte(c.AdminPass+"\n"), 0o600); err != nil {
		return false, err
	}
	return true, nil
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
	if c.disableLoginRaw != "" {
		if _, err := strconv.ParseBool(c.disableLoginRaw); err != nil {
			return fmt.Errorf("DISABLE_LOGIN should be true or false, not %q", c.disableLoginRaw)
		}
	}
	return nil
}
