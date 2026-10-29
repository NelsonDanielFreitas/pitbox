package domain

import (
	"fmt"
	"math"
	"unicode/utf8"
)

// maxSpecString is the longest text value allowed in part specs.
const maxSpecString = 40

// specField describes one allowed key of a part kind's specs (ADR 0021).
type specField struct {
	text     bool    // true: short string; false: number
	min, max float64 // numbers only; min is exclusive when exclusiveMin is set
	exclMin  bool
}

var (
	oilSpecs   = map[string]specField{"viscosity_cst": {min: 0, max: 100_000, exclMin: true}}
	fuelSpecs  = map[string]specField{"nitro_pct": {min: 0, max: 40}, "oil_pct": {min: 0, max: 30}}
	plugSpecs  = map[string]specField{"heat_range": {text: true}}
	springSpec = map[string]specField{"rate": {text: true}}
)

// specsByKind lists the allowed keys per part kind. A kind that is not listed takes none.
var specsByKind = map[string]map[string]specField{
	PartShockOil: oilSpecs, PartDiffOil: oilSpecs, PartFuel: fuelSpecs,
	PartGlowPlug: plugSpecs, PartSpring: springSpec,
}

// validateSpecsForKind rejects unknown keys, wrong types and out-of-range values.
func validateSpecsForKind(kind string, specs map[string]any) error {
	allowed := specsByKind[kind]
	for key, value := range specs {
		field, ok := allowed[key]
		if !ok {
			return fmt.Errorf("%w: specs key", ErrInvalid)
		}
		if field.text {
			s, ok := value.(string)
			if !ok || s == "" || utf8.RuneCountInString(s) > maxSpecString {
				return fmt.Errorf("%w: specs value", ErrInvalid)
			}
			continue
		}
		n, ok := specNumber(value)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n > field.max || n < field.min || (field.exclMin && n == field.min) {
			return fmt.Errorf("%w: specs value", ErrInvalid)
		}
	}
	return nil
}

func specNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}

// ValidUnit reports whether unit is one of the supported part units.
func ValidUnit(unit string) bool {
	switch unit {
	case "un", "ml", "l", "g":
		return true
	}
	return false
}
