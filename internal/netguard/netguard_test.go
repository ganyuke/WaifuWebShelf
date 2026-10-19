package netguard

import "testing"

func TestPublicOnly(t *testing.T) {
	cases := map[string]bool{
		"93.184.215.14:443":          true,
		"[2606:4700::6810:84e5]:443": true,
		"127.0.0.1:80":               false,
		"10.1.2.3:80":                false,
		"100.64.0.1:80":              false,
		"0.1.2.3:80":                 false,
		"169.254.169.254:80":         false,
		"198.18.0.1:80":              false,
		"[::1]:80":                   false,
		"[::ffff:192.168.1.1]:80":    false,
		"[64:ff9b::a00:1]:80":        false,
		"[2002:a00:1::]:80":          false,
		"[fd00::1]:80":               false,
		"[fe80::1]:80":               false,
	}
	for addr, want := range cases {
		if got := PublicOnly("tcp", addr, nil) == nil; got != want {
			t.Errorf("%s: allowed=%v want %v", addr, got, want)
		}
	}
}
