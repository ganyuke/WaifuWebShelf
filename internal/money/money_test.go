package money_test

import (
	"testing"

	"waifuwebshelf/internal/money"
)

func TestParseCAD(t *testing.T) {
	got, err := money.Parse("12.99", "CAD")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != 1299 {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeEmpty(t *testing.T) {
	if money.NormalizeCurrency("") != money.Default {
		t.Fatalf("empty %q", money.NormalizeCurrency(""))
	}
	if money.Default != "USD" {
		t.Fatalf("default %q", money.Default)
	}
}

func TestParseEmpty(t *testing.T) {
	got, err := money.Parse("  ", "CAD")
	if err != nil || got != nil {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestParseJPY(t *testing.T) {
	got, err := money.Parse("1500", "JPY")
	if err != nil || got == nil || *got != 1500 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := money.Parse("1500.00", "JPY"); err == nil {
		t.Fatal("expected error for JPY decimals")
	}
}

func TestParsePad(t *testing.T) {
	got, err := money.Parse("12.9", "USD")
	if err != nil || got == nil || *got != 1290 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestFormat(t *testing.T) {
	if money.Format(1299, "CAD") != "CAD 12.99" {
		t.Fatalf("%q", money.Format(1299, "CAD"))
	}
	if money.Format(1500, "JPY") != "JPY 1500" {
		t.Fatalf("%q", money.Format(1500, "JPY"))
	}
	if money.FormatAmount(1299, "CAD") != "12.99" {
		t.Fatalf("%q", money.FormatAmount(1299, "CAD"))
	}
	if money.FormatAmountDelta(3300, "CAD") != "+33.00" {
		t.Fatalf("%q", money.FormatAmountDelta(3300, "CAD"))
	}
}

func TestNegativeRejected(t *testing.T) {
	if _, err := money.Parse("-1", "CAD"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDisplay(t *testing.T) {
	cases := []struct {
		amount   int64
		currency string
		want     string
	}{
		{109940, "JPY", "¥109,940"},
		{1699, "CAD", "CA$16.99"},
		{123456789, "usd", "US$1,234,567.89"},
		{500, "CHF", "CHF 5.00"},
		{-8992, "CAD", "-CA$89.92"},
		{0, "JPY", "¥0"},
	}
	for _, c := range cases {
		if got := money.Display(c.amount, c.currency); got != c.want {
			t.Errorf("Display(%d, %q) = %q, want %q", c.amount, c.currency, got, c.want)
		}
	}
}
