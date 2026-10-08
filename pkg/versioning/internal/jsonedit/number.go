package jsonedit

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// numberText converts JSON number text to the form serde_json prints.
func numberText(raw string) (string, error) {
	if !strings.ContainsAny(raw, ".eE") {
		if raw == "-0" {
			return "-0.0", nil // serde_json parses "-0" as the float -0.0
		}
		if raw[0] == '-' {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return strconv.FormatInt(n, 10), nil
			}
		} else if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return strconv.FormatUint(n, 10), nil
		}
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(f, 0) {
		return "", fmt.Errorf("number out of range: %s", raw)
	}
	return formatFloat(f), nil
}

// formatFloat prints f like Rust's ryu (the serde_json float formatter).
func formatFloat(f float64) string {
	sign := ""
	if math.Signbit(f) {
		sign = "-"
		f = -f
	}
	if f == 0 {
		return sign + "0.0"
	}
	// 'e' with -1 precision yields the shortest digits: d.ddde±XX
	s := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expText, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expText)
	digits := strings.Replace(mant, ".", "", 1)
	n := len(digits)
	exp10 := exp - (n - 1) // value = digits * 10^exp10
	kk := n + exp10
	switch {
	case exp10 >= 0 && kk <= 16:
		return sign + digits + strings.Repeat("0", exp10) + ".0"
	case kk > 0 && kk <= 16:
		return sign + digits[:kk] + "." + digits[kk:]
	case kk > -5 && kk <= 0:
		return sign + "0." + strings.Repeat("0", -kk) + digits
	case n == 1:
		return sign + digits + "e" + strconv.Itoa(kk-1)
	default:
		return sign + digits[:1] + "." + digits[1:] + "e" + strconv.Itoa(kk-1)
	}
}
