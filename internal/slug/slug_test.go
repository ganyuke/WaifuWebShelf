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

func TestForBookLine(t *testing.T) {
	if ForBookLine("manga", "") != "manga" {
		t.Fatal(ForBookLine("manga", ""))
	}
	if ForBookLine("light_novel", "") != "light-novel" {
		t.Fatal(ForBookLine("light_novel", ""))
	}
	if ForBookLine("manga", "Official Comic Anthology") != "official-comic-anthology" {
		t.Fatal(ForBookLine("manga", "Official Comic Anthology"))
	}
}
