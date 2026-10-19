package names

import "testing"

func TestVolumeNames(t *testing.T) {
	kon := Naming{SeriesName: "K-On!", Medium: "manga", Language: "en"}
	shuffle := Naming{SeriesName: "K-On!", WorkTitle: "K-On! Shuffle", Medium: "manga", Language: "en"}
	fma := Naming{SeriesName: "Fullmetal Alchemist", Medium: "manga", Language: "en", EditionName: "3-in-1"}
	rascal := Naming{SeriesName: "Rascal Does Not Dream", Medium: "light_novel", Language: "en"}
	anthology := Naming{SeriesName: "Rent-A-Girlfriend", WorkTitle: "Official Comic Anthology", Medium: "other"}
	for _, c := range []struct {
		n                     Naming
		number, title         string
		wantName, wantCaption string
	}{
		{kon, "1", "", "K-On! vol. 1", "English manga"},
		{shuffle, "1", "", "K-On! Shuffle vol. 1", "English manga"},
		{fma, "3", "", "Fullmetal Alchemist 3-in-1 vol. 3", "English manga"},
		{rascal, "2", "Rascal Does Not Dream of Petite Devil Kohai", "Rascal Does Not Dream of Petite Devil Kohai", "vol. 2, English light novel"},
		{anthology, "1", "", "Official Comic Anthology vol. 1", ""},
	} {
		if got := c.n.Volume(c.number, c.title); got != c.wantName {
			t.Errorf("name: got %q want %q", got, c.wantName)
		}
		if got := c.n.VolumeCaption(c.number, c.title); got != c.wantCaption {
			t.Errorf("caption: got %q want %q", got, c.wantCaption)
		}
	}
}

func TestHeadings(t *testing.T) {
	if got := WorkHeading("", "light_novel"); got != "Light novel" {
		t.Errorf("got %q", got)
	}
	if got := WorkHeading("K-On! Shuffle", "manga"); got != "K-On! Shuffle" {
		t.Errorf("got %q", got)
	}
	for in, want := range map[[2]string]string{
		{"en", ""}:       "English",
		{"en", "3-in-1"}: "English 3-in-1",
		{"", "Deluxe"}:   "Deluxe",
		{"", ""}:         "Edition",
	} {
		if got := EditionLabel(in[0], in[1]); got != want {
			t.Errorf("%v: got %q want %q", in, got, want)
		}
	}
}

func TestURLs(t *testing.T) {
	n := Naming{SeriesSlug: "k-on", WorkSlug: "k-on-shuffle", EditionSlug: "english"}
	if got := n.VolumeURL("8.5"); got != "/series/k-on/k-on-shuffle/english/8.5" {
		t.Errorf("got %q", got)
	}
	if got := n.EditionURL(); got != "/series/k-on#k-on-shuffle-english" {
		t.Errorf("got %q", got)
	}
}

func TestDescribe(t *testing.T) {
	for _, c := range []struct{ title, kind, lang, name, want string }{
		{"", "manga", "en", "", "English manga"},
		{"K-On! Shuffle", "manga", "en", "3-in-1", "K-On! Shuffle, English 3-in-1 manga"},
		{"", "light_novel", "", "", "Light novel"},
		{"Official Comic Anthology", "other", "ja", "", "Official Comic Anthology, Japanese"},
		{"", "other", "", "", "Other"},
	} {
		if got := Describe(c.title, c.kind, c.lang, c.name); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}
