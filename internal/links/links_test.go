package links

import "testing"

func TestNormalizeURL(t *testing.T) {
	u, err := NormalizeURL("myanimelist.net/manga/1")
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://myanimelist.net/manga/1" {
		t.Fatalf("%q", u)
	}
}

func TestNormalizeURLWebOnly(t *testing.T) {
	for _, raw := range []string{"javascript://x/%0aalert(1)", "data://text/html,hi", "ftp://example.com/a"} {
		if _, err := NormalizeURL(raw); err == nil {
			t.Errorf("%s should be refused", raw)
		}
	}
	if _, err := NormalizeURL("http://example.com"); err != nil {
		t.Error(err)
	}
}
