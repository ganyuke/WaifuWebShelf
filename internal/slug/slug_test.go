package slug

import "testing"

func TestMake(t *testing.T) {
	if Make("Spice and Wolf") != "spice-and-wolf" {
		t.Fatal(Make("Spice and Wolf"))
	}
	if Make("Re:Zero") != "rezero" {
		t.Fatal(Make("Re:Zero"))
	}
}

func TestUnique(t *testing.T) {
	exists := func(s string) bool { return s == "spice-and-wolf" }
	if Unique("spice-and-wolf", exists) != "spice-and-wolf-2" {
		t.Fatal(Unique("spice-and-wolf", exists))
	}
}

func TestForWorkAndEdition(t *testing.T) {
	for got, want := range map[string]string{
		ForWork("", "manga"):              "manga",
		ForWork("", "light_novel"):        "light-novel",
		ForWork("K-On! Shuffle", "manga"): "k-on-shuffle",
		ForEdition("English", ""):         "english",
		ForEdition("English", "3-in-1"):   "english-3-in-1",
		ForEdition("", ""):                "edition",
	} {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
