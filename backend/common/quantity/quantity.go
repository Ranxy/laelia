// Package quantity parses and compares the k8s-quantity subset machine
// provisioning uses (plain integers/decimals, decimal SI suffixes
// m/k/M/G/T/P, binary SI suffixes Ki/Mi/Gi/Ti/Pi). The manager keeps itself
// k8s-library-free, so provisioner-validated quantities are re-checked here
// for fail-fast validation and min/max bounds comparison; the k8s API server
// remains the authoritative validator.
//
// Values normalize to milli-units (one unit = 1000), the resolution resource
// sizing needs; magnitudes beyond ~9.2 P (decimal) or ~8 Pi (binary) overflow
// int64 and are rejected — no machine workload is provisioned at exabyte
// scale.
package quantity

import (
	"math"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// Milli is one quantity normalized to thousandths of a unit.
type Milli int64

// milliMax is the largest representable milli value.
const milliMax = Milli(math.MaxInt64)

// milliTable maps a quantity suffix to the milli-units one full unit of that
// suffix equals (e.g. "k" = 10^3 units = 10^6 milli). Decimal suffixes follow
// SI case conventions (lowercase k, uppercase rest); there is deliberately no
// bare "K", no exponent notation, and no suffix smaller than m or larger than
// P — machine sizing never uses them, and E-scale values overflow Milli.
var milliTable = map[string]Milli{
	"":   1_000,                     // one unit
	"m":  1,                         // 10^-3 units
	"k":  1_000_000,                 // 10^3 units
	"M":  1_000_000_000,             // 10^6 units
	"G":  1_000_000_000_000,         // 10^9 units
	"T":  1_000_000_000_000_000,     // 10^12 units
	"P":  1_000_000_000_000_000_000, // 10^15 units
	"Ki": 1000 << 10,
	"Mi": 1000 << 20,
	"Gi": 1000 << 30,
	"Ti": 1000 << 40,
	"Pi": 1000 << 50,
}

// Parse parses one quantity and returns its milli-unit value. The input must
// be a non-negative number with an optional suffix and no whitespace: "500m",
// "2", "0.5", "10Gi". More than three fractional digits exceed the milli
// resolution and are rejected (never silently rounded).
func Parse(s string) (Milli, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty quantity")
	}
	if s[0] == '+' || s[0] == '-' {
		return 0, errors.Errorf("quantity %q must be non-negative", s)
	}

	// Split the numeric head from the suffix at the first letter.
	head := s
	for i := 0; i < len(s); i++ {
		if s[i] != '.' && (s[i] < '0' || s[i] > '9') {
			head = s[:i]
			break
		}
	}
	suffix := strings.TrimPrefix(s, head)
	mult, ok := milliTable[suffix]
	if !ok {
		return 0, errors.Errorf("quantity %q has an unsupported suffix %q (want one of m, k, M, G, T, P, Ki, Mi, Gi, Ti, Pi)", s, suffix)
	}

	whole, frac, err := parseHead(head, s)
	if err != nil {
		return 0, err
	}
	if suffix == "m" && frac != 0 {
		return 0, errors.Errorf("quantity %q is below the milli resolution", s)
	}
	// value = whole*mult + frac*(mult/1000); mult is always divisible by 1000
	// here because the sub-milli "m" case with a fraction is rejected above.
	if whole > milliMax/mult {
		return 0, errors.Errorf("quantity %q is too large", s)
	}
	return whole*mult + frac*(mult/1000), nil
}

// parseHead converts "12" or "12.345" into whole units and the milli
// fraction. head is the digit/dot prefix cut by Parse: it must start with a
// digit, contain at most one dot, and carry at most three fractional digits.
func parseHead(head, full string) (Milli, Milli, error) {
	if head == "" || head[0] == '.' {
		return 0, 0, errors.Errorf("quantity %q must start with a digit", full)
	}
	wholeStr, fracStr, hasDot := strings.Cut(head, ".")
	if hasDot && fracStr == "" {
		return 0, 0, errors.Errorf("quantity %q has a trailing decimal point", full)
	}
	if len(fracStr) > 3 {
		return 0, 0, errors.Errorf("quantity %q exceeds the milli resolution (more than three fractional digits)", full)
	}
	whole64, err := strconv.ParseUint(wholeStr, 10, 63)
	if err != nil {
		return 0, 0, errors.Errorf("quantity %q has an invalid or oversized integer part", full)
	}
	frac := Milli(0)
	if fracStr != "" {
		frac64, err := strconv.ParseUint(fracStr, 10, 32)
		if err != nil {
			return 0, 0, errors.Errorf("quantity %q has an invalid fractional part", full)
		}
		// Left-pad the fraction to milli precision: "0.5" → 500.
		frac = Milli(frac64)
		for i := len(fracStr); i < 3; i++ {
			frac *= 10
		}
	}
	return Milli(whole64), frac, nil
}
