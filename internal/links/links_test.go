package links

import "testing"

func TestIcon(t *testing.T) {
	cases := map[string]string{
		"https://myanimelist.net/manga/1":             "mal",
		"https://www.myfigurecollection.net/item/123": "mfc",
		"https://anilist.co/manga/1":                  "anilist",
		"https://isbnsearch.org/isbn/978000":          "isbn",
		"https://www.amazon.ca/dp/x":                  "amazon",
		"https://example.com/foo":                     "generic",
	}
	for u, want := range cases {
		if got := Icon(u); got != want {
			t.Errorf("%s: got %s want %s", u, got, want)
		}
	}
}

func TestNormalizeURL(t *testing.T) {
	u, err := NormalizeURL("myanimelist.net/manga/1")
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://myanimelist.net/manga/1" {
		t.Fatalf("%q", u)
	}
}
