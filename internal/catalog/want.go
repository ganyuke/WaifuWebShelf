package catalog

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func ParseVolumeSpan(from, to string) (string, string, error) {
	from = normalizeNumber(from)
	to = normalizeNumber(to)
	if from == "" {
		return "", "", fmt.Errorf("Enter a volume number.")
	}
	if to == "" || to == from {
		return from, from, nil
	}
	a, errA := strconv.Atoi(from)
	b, errB := strconv.Atoi(to)
	if errA != nil || errB != nil || a < 1 || b < 1 {
		return "", "", fmt.Errorf("Enter whole numbers for a range.")
	}
	if a > b {
		return "", "", fmt.Errorf("Put the lower volume number first.")
	}
	return from, to, nil
}

func OwnedVolumeKeys(numbers []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range numbers {
		if n != "" {
			out[n] = true
		}
	}
	return out
}

type Remaining struct {
	Numbers []int
	Keys    []string
}

func (r Remaining) Count() int {
	return len(r.Numbers) + len(r.Keys)
}

func (r Remaining) Empty() bool {
	return r.Count() == 0
}

func (r Remaining) NumberSet() map[int]bool {
	out := map[int]bool{}
	for _, n := range r.Numbers {
		out[n] = true
	}
	return out
}

func SpanRemaining(from, to string, owned map[string]bool) Remaining {
	if owned == nil {
		owned = map[string]bool{}
	}
	from, to, err := ParseVolumeSpan(from, to)
	if err != nil {
		return Remaining{}
	}
	a, errA := strconv.Atoi(from)
	b, errB := strconv.Atoi(to)
	if errA == nil && errB == nil && a >= 1 && b >= a {
		var nums []int
		for n := a; n <= b; n++ {
			if !owned[strconv.Itoa(n)] {
				nums = append(nums, n)
			}
		}
		return Remaining{Numbers: nums}
	}
	if owned[from] {
		return Remaining{}
	}
	return Remaining{Keys: []string{from}}
}

func FormatRemaining(r Remaining) string {
	var parts []string
	if len(r.Numbers) > 0 {
		start := r.Numbers[0]
		prev := r.Numbers[0]
		flush := func() {
			if start == prev {
				parts = append(parts, itoa(start))
			} else {
				parts = append(parts, itoa(start)+"–"+itoa(prev))
			}
		}
		for _, n := range r.Numbers[1:] {
			if n == prev+1 {
				prev = n
				continue
			}
			flush()
			start = n
			prev = n
		}
		flush()
	}
	parts = append(parts, r.Keys...)
	return strings.Join(parts, ", ")
}

func RemainingMSRP(msrpCents *int64, remaining int) *int64 {
	if msrpCents == nil || remaining <= 0 {
		return nil
	}
	v := *msrpCents * int64(remaining)
	return &v
}

func MergeRemaining(parts ...Remaining) Remaining {
	seenN := map[int]bool{}
	seenK := map[string]bool{}
	var out Remaining
	for _, p := range parts {
		for _, n := range p.Numbers {
			if seenN[n] {
				continue
			}
			seenN[n] = true
			out.Numbers = append(out.Numbers, n)
		}
		for _, k := range p.Keys {
			if seenK[k] {
				continue
			}
			seenK[k] = true
			out.Keys = append(out.Keys, k)
		}
	}
	sort.Ints(out.Numbers)
	sort.Strings(out.Keys)
	return out
}

func normalizeNumber(s string) string {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return strconv.Itoa(n)
	}
	return s
}
