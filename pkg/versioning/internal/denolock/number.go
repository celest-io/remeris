package denolock

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// The number handling below is the one of pkg/versioning/internal/jsonedit (a port of serde_json's number parser and
// printer), copied because denolock imports nothing but the standard library and denosemver. Numbers only matter
// in the free-form JSON that deno.lock keeps verbatim (npm overrides and peerDependenciesMeta).

// errNumberRange is serde_json's "number out of range" error.
var errNumberRange = errors.New("number out of range")

// pow10 is serde_json's POW10 table: 1e0 to 1e308.
var pow10 = func() [309]float64 {
	var p [309]float64
	for i := range p {
		p[i], _ = strconv.ParseFloat("1e"+strconv.Itoa(i), 64)
	}
	return p
}()

// overflowU64 reports whether sig*10+digit overflows a uint64 (serde_json's overflow! macro).
func overflowU64(sig, digit uint64) bool {
	return sig >= math.MaxUint64/10 && (sig > math.MaxUint64/10 || digit > math.MaxUint64%10)
}

// numberText converts the text of a valid JSON number to the form serde_json prints after parsing it into a Value
// without arbitrary_precision and without float_roundtrip. That parser is not correctly rounded: it accumulates a
// uint64 significand, converts it to float64 and multiplies or divides by a power of ten (f64_from_parts in
// serde_json/src/de.rs).
func numberText(raw string) (string, error) {
	i := 0
	positive := raw[0] != '-'
	if !positive {
		i++
	}
	digitsAt := func() bool { return i < len(raw) && raw[i] >= '0' && raw[i] <= '9' }

	// Integer part: a lone 0, or digits while the significand fits.
	sig := uint64(raw[i] - '0')
	i++
	isInt, exp := true, 0
	if sig != 0 {
		for digitsAt() {
			d := uint64(raw[i] - '0')
			if overflowU64(sig, d) {
				isInt = false
				for ; digitsAt(); i++ { // the remaining digits only scale the significand
					exp++
				}
				break
			}
			i++
			sig = sig*10 + d
		}
	}
	if i < len(raw) && raw[i] == '.' {
		isInt = false
		i++
		for digitsAt() {
			d := uint64(raw[i] - '0')
			if overflowU64(sig, d) {
				for digitsAt() { // further digits are ignored
					i++
				}
				break
			}
			i++
			sig = sig*10 + d
			exp--
		}
	}
	if i < len(raw) && (raw[i] == 'e' || raw[i] == 'E') {
		isInt = false
		e, zeroOrErr, err := parseExponent(raw[i+1:], sig == 0)
		if err != nil {
			return "", err
		}
		if zeroOrErr {
			if positive {
				return "0.0", nil
			}
			return "-0.0", nil
		}
		exp = satAdd(exp, e)
	}
	if isInt {
		if positive {
			return strconv.FormatUint(sig, 10), nil
		}
		if neg := -int64(sig); neg < 0 { //nolint:gosec // wrapping negation, like Rust's wrapping_neg
			return strconv.FormatInt(neg, 10), nil
		}
		return formatFloat(-float64(sig)), nil // "-0" and below i64::MIN become floats
	}
	f, err := floatFromParts(sig, exp)
	if err != nil {
		return "", err
	}
	if !positive {
		f = -f
	}
	return formatFloat(f), nil
}

// parseExponent reads the exponent after the 'e' as a signed value. When it does not fit in an i32, serde_json
// fails for a nonzero significand with a positive exponent and otherwise yields a signed zero (zero is true).
func parseExponent(s string, zeroSig bool) (exp int, zero bool, err error) {
	pos := true
	if s[0] == '+' || s[0] == '-' {
		pos = s[0] == '+'
		s = s[1:]
	}
	for i := range len(s) {
		d := int(s[i] - '0')
		if exp > (math.MaxInt32-d)/10 {
			if !zeroSig && pos {
				return 0, false, errNumberRange
			}
			return 0, true, nil
		}
		exp = exp*10 + d
	}
	if !pos {
		exp = -exp
	}
	return exp, false, nil
}

// floatFromParts is serde_json's f64_from_parts without float_roundtrip, for a positive value.
func floatFromParts(sig uint64, exp int) (float64, error) {
	f := float64(sig)
	for {
		if a := abs(exp); a < len(pow10) {
			if exp >= 0 {
				f *= pow10[a]
				if math.IsInf(f, 0) {
					return 0, errNumberRange
				}
			} else {
				f /= pow10[a]
			}
			return f, nil
		}
		if f == 0 {
			return 0, nil
		}
		if exp >= 0 {
			return 0, errNumberRange
		}
		f /= 1e308
		exp += 308
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// satAdd adds two exponents, saturating at the i32 range like Rust's saturating_add.
func satAdd(a, b int) int {
	return int(max(math.MinInt32, min(math.MaxInt32, int64(a)+int64(b))))
}

// formatFloat prints f like serde_json does (zmij since 1.0.147; ryu before): the shortest digits that round-trip,
// plain notation for decimal exponents from -5 to 15, and otherwise scientific notation with an explicit sign
// on positive exponents (1e+16, 1.5e-7).
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
	}
	e := strconv.Itoa(kk - 1)
	if kk-1 >= 0 {
		e = "+" + e
	}
	if n == 1 {
		return sign + digits + "e" + e
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + e
}
