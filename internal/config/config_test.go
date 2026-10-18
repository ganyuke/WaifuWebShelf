package config

import "testing"

func TestValidatePassword(t *testing.T) {
	c := Config{Port: "8080", DataDir: "data"}
	if err := c.Validate(); err != nil {
		t.Fatalf("no password means admin off, got %v", err)
	}
	c.AdminPass = "changeme"
	if err := c.Validate(); err == nil {
		t.Fatal("a short password should be refused")
	}
	c.AdminPass = "set-a-real-password"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
