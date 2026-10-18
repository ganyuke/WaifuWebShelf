package config

import (
	"os"
	"strings"
	"testing"
)

func TestAnyPasswordIsAccepted(t *testing.T) {
	c := Config{Port: "8080", DataDir: t.TempDir(), AdminPass: "x"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if gen, err := c.ResolveAdminPass(); err != nil || gen || c.AdminPass != "x" {
		t.Fatalf("ADMIN_PASS should be used as is: %v %v %q", gen, err, c.AdminPass)
	}
	if _, err := os.Stat(c.AdminPassPath()); err == nil {
		t.Fatal("nothing should be saved when ADMIN_PASS is set")
	}
}

func TestGeneratedPassword(t *testing.T) {
	dir := t.TempDir()
	c := Config{DataDir: dir}
	gen, err := c.ResolveAdminPass()
	if err != nil || !gen || len(c.AdminPass) < 16 {
		t.Fatalf("want a new password: %v %v %q", gen, err, c.AdminPass)
	}
	info, err := os.Stat(c.AdminPassPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("saved password file %v %v", info, err)
	}
	// The next start reuses it.
	again := Config{DataDir: dir}
	if gen, err := again.ResolveAdminPass(); err != nil || gen || again.AdminPass != c.AdminPass {
		t.Fatalf("want the saved password back: %v %v", gen, err)
	}
}

func TestDisableLoginIgnoresPassword(t *testing.T) {
	t.Setenv("DISABLE_LOGIN", "true")
	t.Setenv("ADMIN_PASS", "secret")
	t.Setenv("DATA_DIR", t.TempDir())
	c := FromEnv()
	if c.LoginEnabled() {
		t.Fatal("login should be off")
	}
	if gen, err := c.ResolveAdminPass(); err != nil || gen || c.AdminPass != "" {
		t.Fatalf("password should be ignored: %v %v %q", gen, err, c.AdminPass)
	}
	if _, err := os.Stat(c.AdminPassPath()); err == nil {
		t.Fatal("no password should be made with login off")
	}

	t.Setenv("DISABLE_LOGIN", "nope")
	if err := FromEnv().Validate(); err == nil || !strings.Contains(err.Error(), "DISABLE_LOGIN") {
		t.Fatalf("want a DISABLE_LOGIN error, got %v", err)
	}
}
