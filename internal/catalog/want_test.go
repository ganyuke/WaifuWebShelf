package catalog

import "testing"

func TestParseVolumeSpan(t *testing.T) {
	from, to, err := ParseVolumeSpan("4", "12")
	if err != nil || from != "4" || to != "12" {
		t.Fatalf("%s %s %v", from, to, err)
	}
	from, to, err = ParseVolumeSpan("7", "")
	if err != nil || from != "7" || to != "7" {
		t.Fatalf("single %s %s %v", from, to, err)
	}
	from, to, err = ParseVolumeSpan("8.5", "")
	if err != nil || from != "8.5" || to != "8.5" {
		t.Fatalf("extra %s %s %v", from, to, err)
	}
	if _, _, err := ParseVolumeSpan("4", "8.5"); err == nil {
		t.Fatal("expected mixed range error")
	}
	if _, _, err := ParseVolumeSpan("9", "2"); err == nil {
		t.Fatal("expected backwards error")
	}
}

func TestSpanRemainingAndFormat(t *testing.T) {
	owned := map[string]bool{"4": true, "5": true, "9": true}
	r := SpanRemaining("4", "12", owned)
	if FormatRemaining(r) != "6–8, 10–12" {
		t.Fatalf("got %q", FormatRemaining(r))
	}
	if r.Count() != 6 {
		t.Fatalf("count %d", r.Count())
	}
	r = SpanRemaining("8.5", "", owned)
	if FormatRemaining(r) != "8.5" {
		t.Fatalf("extra %q", FormatRemaining(r))
	}
	r = SpanRemaining("4", "5", owned)
	if !r.Empty() {
		t.Fatalf("expected empty %+v", r)
	}
}

func TestRemainingMSRP(t *testing.T) {
	n := int64(1200)
	got := RemainingMSRP(&n, 3)
	if got == nil || *got != 3600 {
		t.Fatalf("got %+v", got)
	}
	if RemainingMSRP(&n, 0) != nil {
		t.Fatal("empty remaining")
	}
}

func TestMergeRemaining(t *testing.T) {
	got := MergeRemaining(
		Remaining{Numbers: []int{4, 5}, Keys: []string{"8.5"}},
		Remaining{Numbers: []int{5, 9}},
	)
	if FormatRemaining(got) != "4–5, 9, 8.5" {
		t.Fatalf("got %q", FormatRemaining(got))
	}
}
