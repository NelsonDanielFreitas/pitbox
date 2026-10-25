package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxMakeModel   = 80
	maxSerial      = 64
	maxNickname    = 40
	maxTransponder = 64
	maxNotes       = 2000
	maxLabel       = 20
	maxCompound    = 40
	maxBrandRef    = 80
	maxDescription = 200
	maxUnit        = 16
	maxNote        = 500
	maxSpecsBytes  = 16 << 10
	maxSpecsDepth  = 4
)

func requiredString(v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || utf8.RuneCountInString(v) > max {
		return "", ErrInvalid
	}
	return v, nil
}

func optionalString(v *string, max int) (*string, error) {
	if v == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*v)
	if value == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(value) > max {
		return nil, ErrInvalid
	}
	return &value, nil
}

func validDate(v *string) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(*v))
	if err != nil {
		return nil, fmt.Errorf("%w: date", ErrInvalid)
	}
	return &t, nil
}

func validateSpecs(specs map[string]any) (map[string]any, error) {
	if specs == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(specs)
	if err != nil || len(raw) > maxSpecsBytes || depth(specs) > maxSpecsDepth {
		return nil, fmt.Errorf("%w: specs", ErrInvalid)
	}
	return specs, nil
}

func depth(v any) int {
	switch value := v.(type) {
	case map[string]any:
		max := 0
		for _, child := range value {
			if d := depth(child); d > max {
				max = d
			}
		}
		return max + 1
	case []any:
		max := 0
		for _, child := range value {
			if d := depth(child); d > max {
				max = d
			}
		}
		return max + 1
	default:
		return 0
	}
}

func trimSpace(v string) string { return strings.TrimSpace(v) }

var (
	scalePattern      = regexp.MustCompile(`^1/[1-9][0-9]?$`)
	powertrainPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

func validCatalogCode(scale, powertrain string) error {
	if !scalePattern.MatchString(scale) || !powertrainPattern.MatchString(powertrain) {
		return fmt.Errorf("%w: catalogue", ErrInvalid)
	}
	return nil
}
