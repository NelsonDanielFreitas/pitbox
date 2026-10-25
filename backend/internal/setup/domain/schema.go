package domain

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

//go:embed schema_v1.json
var schemaV1JSON []byte

const (
	SchemaVersion = 1
	MaxDataBytes  = 64 << 10
	MaxDataDepth  = 8
)

type fieldRule struct {
	Type   string               `json:"type"`
	Min    *float64             `json:"min,omitempty"`
	Max    *float64             `json:"max,omitempty"`
	MaxLen *int                 `json:"max_len,omitempty"`
	Fields map[string]fieldRule `json:"fields,omitempty"`
}

type schemaFile struct {
	Version int                  `json:"version"`
	Fields  map[string]fieldRule `json:"fields"`
}

var schemaV1, schemaV1Err = loadSchema()

func loadSchema() (schemaFile, error) {
	var s schemaFile
	if err := json.Unmarshal(schemaV1JSON, &s); err != nil {
		return schemaFile{}, fmt.Errorf("setup: invalid schema_v1.json: %w", err)
	}
	if s.Version != SchemaVersion || len(s.Fields) == 0 {
		return schemaFile{}, fmt.Errorf("setup: schema_v1.json has the wrong version or no fields")
	}
	return s, nil
}

// ValidateV1 checks data against the v1 schema. It returns the same map so the
// caller can reuse it; it never mutates the input.
func ValidateV1(data map[string]any) (map[string]any, error) {
	return ValidateData(SchemaVersion, data)
}

// ValidateData checks data against the known schema version.
func ValidateData(version int, data map[string]any) (map[string]any, error) {
	if schemaV1Err != nil {
		return nil, fmt.Errorf("setup: schema unavailable: %w", ErrInvalid)
	}
	if version != SchemaVersion {
		return nil, &ValidationError{Path: "schema_version", RuleID: RuleSchemaVersion}
	}
	if data == nil {
		data = map[string]any{}
	}
	if exceedsDepth(data, MaxDataDepth) {
		return nil, &ValidationError{Path: "data", RuleID: RuleDepth}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, &ValidationError{Path: "data", RuleID: RuleType}
	}
	if len(raw) > MaxDataBytes {
		return nil, &ValidationError{Path: "data", RuleID: RuleSize}
	}
	if err := validateFields("", data, schemaV1.Fields); err != nil {
		return nil, err
	}
	if err := validateBars(data); err != nil {
		return nil, err
	}
	return normalizeMap(data), nil
}

// normalizeMap removes empty groups so {"bars": {}} and {} have the same
// canonical representation. It copies the map; the caller's value is untouched.
func normalizeMap(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for key, value := range data {
		switch v := value.(type) {
		case map[string]any:
			if normalized := normalizeMap(v); len(normalized) > 0 {
				out[key] = normalized
			}
		case []any:
			out[key] = normalizeSlice(v)
		default:
			out[key] = value
		}
	}
	return out
}

func normalizeSlice(values []any) []any {
	out := make([]any, len(values))
	for i, value := range values {
		switch v := value.(type) {
		case map[string]any:
			out[i] = normalizeMap(v)
		case []any:
			out[i] = normalizeSlice(v)
		default:
			out[i] = value
		}
	}
	return out
}

// MarshalV1 validates and returns the canonical JSON stored in setup_version.data.
func MarshalV1(data map[string]any) ([]byte, error) {
	validated, err := ValidateV1(data)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(validated)
	if err != nil {
		return nil, &ValidationError{Path: "data", RuleID: RuleType}
	}
	if len(raw) > MaxDataBytes {
		return nil, &ValidationError{Path: "data", RuleID: RuleSize}
	}
	return raw, nil
}

// DecodeData parses JSONB data read from the database. The database value was
// validated on write; this only refuses a broken shape so a read fails safely.
func DecodeData(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil || data == nil {
		return nil, fmt.Errorf("setup: invalid stored data: %w", ErrInvalid)
	}
	return data, nil
}

func validateFields(path string, data map[string]any, fields map[string]fieldRule) error {
	keys := sortedKeys(data)
	for _, key := range keys {
		value := data[key]
		rule, ok := fields[key]
		if !ok {
			return &ValidationError{Path: joinPath(path, key), RuleID: RuleUnknown}
		}
		if err := validateValue(joinPath(path, key), value, rule); err != nil {
			return err
		}
	}
	return nil
}

func validateValue(path string, value any, rule fieldRule) error {
	if value == nil {
		return &ValidationError{Path: path, RuleID: RuleType}
	}
	switch rule.Type {
	case "group":
		obj, ok := value.(map[string]any)
		if !ok {
			return &ValidationError{Path: path, RuleID: RuleType}
		}
		return validateFields(path, obj, rule.Fields)
	case "int":
		n, ok := finiteNumber(value)
		if !ok || math.Trunc(n) != n {
			return &ValidationError{Path: path, RuleID: RuleType}
		}
		return checkRange(path, n, rule)
	case "number":
		n, ok := finiteNumber(value)
		if !ok {
			return &ValidationError{Path: path, RuleID: RuleType}
		}
		return checkRange(path, n, rule)
	case "string":
		s, ok := value.(string)
		if !ok {
			return &ValidationError{Path: path, RuleID: RuleType}
		}
		if rule.MaxLen != nil && utf8.RuneCountInString(s) > *rule.MaxLen {
			return &ValidationError{Path: path, RuleID: RuleLength}
		}
		return nil
	case "bool":
		if _, ok := value.(bool); !ok {
			return &ValidationError{Path: path, RuleID: RuleType}
		}
		return nil
	case "uuid":
		s, ok := value.(string)
		if !ok {
			return &ValidationError{Path: path, RuleID: RuleType}
		}
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			return &ValidationError{Path: path, RuleID: RuleUUID}
		}
		return nil
	default:
		return fmt.Errorf("setup: unknown schema rule type %q", rule.Type)
	}
}

func checkRange(path string, n float64, rule fieldRule) error {
	if (rule.Min != nil && n < *rule.Min) || (rule.Max != nil && n > *rule.Max) {
		return &ValidationError{Path: path, RuleID: RuleRange}
	}
	return nil
}

func finiteNumber(v any) (float64, bool) {
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

func validateBars(data map[string]any) error {
	bars, ok := data["bars"].(map[string]any)
	if !ok {
		return nil
	}
	for _, side := range []string{"front", "rear"} {
		removed, _ := bars[side+"_removed"].(bool)
		_, hasMM := bars[side+"_mm"]
		if removed && hasMM {
			return &ValidationError{Path: "bars." + side + "_mm", RuleID: RuleBarsConflict}
		}
	}
	return nil
}

func exceedsDepth(v any, max int) bool {
	type item struct {
		value any
		depth int
	}
	stack := []item{{v, 1}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch value := it.value.(type) {
		case map[string]any:
			if it.depth > max {
				return true
			}
			for _, child := range value {
				stack = append(stack, item{child, it.depth + 1})
			}
		case []any:
			if it.depth > max {
				return true
			}
			for _, child := range value {
				stack = append(stack, item{child, it.depth + 1})
			}
		}
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// NormalizeName trims and bounds a setup name (1..120 runes).
func NormalizeName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || utf8.RuneCountInString(v) > 120 {
		return "", fmt.Errorf("%w: name", ErrInvalid)
	}
	return v, nil
}

// NormalizeNotes trims an optional setup note and bounds it at 2000 runes.
func NormalizeNotes(v *string) (*string, error) {
	return normalizeOptional(v, 2000)
}

// NormalizeChangeSummary trims an optional version summary (max 500 runes).
func NormalizeChangeSummary(v *string) (*string, error) {
	return normalizeOptional(v, 500)
}

func normalizeOptional(v *string, max int) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(s) > max {
		return nil, fmt.Errorf("%w: text", ErrInvalid)
	}
	return &s, nil
}
