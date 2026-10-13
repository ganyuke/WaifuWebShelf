package money

import (
	"fmt"
	"strings"
)

const Default = "USD"

func Decimals(currency string) int {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "JPY", "KRW":
		return 0
	default:
		return 2
	}
}

func NormalizeCurrency(currency string) string {
	c := strings.ToUpper(strings.TrimSpace(currency))
	if c == "" {
		return Default
	}
	return c
}

// Parse converts a decimal string into minor units (cents for CAD/USD, yen for JPY).
// Empty input returns (nil, nil).
func Parse(s, currency string) (*int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if strings.ContainsAny(s, " \t") {
		return nil, fmt.Errorf("invalid amount")
	}
	if strings.HasPrefix(s, "-") {
		return nil, fmt.Errorf("amount cannot be negative")
	}

	dec := Decimals(currency)
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return nil, fmt.Errorf("invalid amount")
	}
	if parts[0] == "" {
		parts[0] = "0"
	}
	if strings.ContainsFunc(parts[0], func(r rune) bool { return r < '0' || r > '9' }) {
		return nil, fmt.Errorf("invalid amount")
	}

	var whole int64
	for _, ch := range parts[0] {
		whole = whole*10 + int64(ch-'0')
	}

	scale := int64(1)
	for i := 0; i < dec; i++ {
		scale *= 10
	}

	var frac int64
	if len(parts) == 2 {
		fracStr := parts[1]
		if fracStr == "" {
			return nil, fmt.Errorf("invalid amount")
		}
		if dec == 0 {
			return nil, fmt.Errorf("%s does not use decimal places", NormalizeCurrency(currency))
		}
		if len(fracStr) > dec {
			return nil, fmt.Errorf("too many decimal places")
		}
		if strings.ContainsFunc(fracStr, func(r rune) bool { return r < '0' || r > '9' }) {
			return nil, fmt.Errorf("invalid amount")
		}
		for len(fracStr) < dec {
			fracStr += "0"
		}
		for _, ch := range fracStr {
			frac = frac*10 + int64(ch-'0')
		}
	}

	total := whole*scale + frac
	return &total, nil
}

func Format(amount int64, currency string) string {
	currency = NormalizeCurrency(currency)
	dec := Decimals(currency)
	if dec == 0 {
		return fmt.Sprintf("%s %d", currency, amount)
	}
	scale := int64(1)
	for i := 0; i < dec; i++ {
		scale *= 10
	}
	whole := amount / scale
	frac := amount % scale
	if frac < 0 {
		frac = -frac
	}
	return fmt.Sprintf("%s %d.%0*d", currency, whole, dec, frac)
}

func FormatPtr(amount *int64, currency string) string {
	if amount == nil {
		return "—"
	}
	return Format(*amount, currency)
}

func FormatNull(valid bool, amount int64, currency string) string {
	if !valid {
		return "—"
	}
	return Format(amount, currency)
}

func FormatAmount(amount int64, currency string) string {
	s := Format(amount, currency)
	return strings.TrimPrefix(s, NormalizeCurrency(currency)+" ")
}

func FormatAmountDelta(amount int64, currency string) string {
	sign := ""
	if amount > 0 {
		sign = "+"
	}
	return sign + FormatAmount(amount, currency)
}

var symbols = map[string]string{
	"USD": "US$",
	"CAD": "CA$",
	"AUD": "A$",
	"JPY": "¥",
	"KRW": "₩",
	"EUR": "€",
	"GBP": "£",
}

// Display formats an amount for reading: currency symbol and thousands
// separators, e.g. "¥109,940" or "CA$16.99". Unknown currencies keep their code.
func Display(amount int64, currency string) string {
	currency = NormalizeCurrency(currency)
	num := FormatAmount(amount, currency)
	neg := strings.HasPrefix(num, "-")
	num = strings.TrimPrefix(num, "-")
	whole, frac, hasFrac := strings.Cut(num, ".")
	var b strings.Builder
	for i, ch := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	if hasFrac {
		b.WriteString("." + frac)
	}
	sym, ok := symbols[currency]
	if !ok {
		sym = currency + " "
	}
	if neg {
		return "-" + sym + b.String()
	}
	return sym + b.String()
}

func DisplayNull(valid bool, amount int64, currency string) string {
	if !valid {
		return "—"
	}
	return Display(amount, currency)
}
