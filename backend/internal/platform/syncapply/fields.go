package syncapply

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// Fields reads the JSON fields of an operation strictly. The device is not trusted: a key
// the entity does not know is an error (no mass assignment), a value of the wrong type is an
// error, and "absent" is told apart from "null" (null clears an optional field).
type Fields map[string]json.RawMessage

// Only fails with ErrInvalidFields if any key is not in allowed.
func (f Fields) Only(allowed ...string) error {
	ok := make(map[string]struct{}, len(allowed))
	for _, k := range allowed {
		ok[k] = struct{}{}
	}
	for k := range f {
		if _, found := ok[k]; !found {
			return fmt.Errorf("unknown field: %w", ErrInvalidFields)
		}
	}
	return nil
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// String returns (set, value). set is false when the key is absent; value is nil for null.
func (f Fields) String(key string) (bool, *string, error) {
	raw, ok := f[key]
	if !ok {
		return false, nil, nil
	}
	if isNull(raw) {
		return true, nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return true, nil, fmt.Errorf("field %s: %w", key, ErrInvalidFields)
	}
	return true, &s, nil
}

// RequiredString fails when the key is absent, null or not a string.
func (f Fields) RequiredString(key string) (string, error) {
	set, v, err := f.String(key)
	if err != nil {
		return "", err
	}
	if !set || v == nil {
		return "", fmt.Errorf("field %s is required: %w", key, ErrInvalidFields)
	}
	return *v, nil
}

// Int64 returns (set, value) for an integer. 1.5, 1e3 written as a float, strings and values
// outside int64 are errors.
func (f Fields) Int64(key string) (bool, *int64, error) {
	raw, ok := f[key]
	if !ok {
		return false, nil, nil
	}
	if isNull(raw) {
		return true, nil, nil
	}
	// json.Number would also accept the string "7"; only a bare numeric literal is an integer.
	if t := bytes.TrimSpace(raw); len(t) == 0 || (t[0] != '-' && (t[0] < '0' || t[0] > '9')) {
		return true, nil, fmt.Errorf("field %s: %w", key, ErrInvalidFields)
	}
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&n); err != nil {
		return true, nil, fmt.Errorf("field %s: %w", key, ErrInvalidFields)
	}
	i, err := n.Int64()
	if err != nil {
		return true, nil, fmt.Errorf("field %s: %w", key, ErrInvalidFields)
	}
	return true, &i, nil
}

// Int32 is Int64 limited to the int32 range.
func (f Fields) Int32(key string) (bool, *int32, error) {
	set, v, err := f.Int64(key)
	if err != nil || v == nil {
		return set, nil, err
	}
	if *v < math.MinInt32 || *v > math.MaxInt32 {
		return true, nil, fmt.Errorf("field %s: %w", key, ErrInvalidFields)
	}
	n := int32(*v)
	return true, &n, nil
}

// Bool returns (set, value). Only a JSON true/false is accepted (not "true" or 1).
func (f Fields) Bool(key string) (bool, *bool, error) {
	raw, ok := f[key]
	if !ok {
		return false, nil, nil
	}
	if isNull(raw) {
		return true, nil, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return true, nil, fmt.Errorf("field %s: %w", key, ErrInvalidFields)
	}
	return true, &b, nil
}

// OptionalObject returns (set, value) for a JSON object; null is an error here (an object
// field is replaced as a whole, never cleared).
func (f Fields) OptionalObject(key string) (bool, map[string]any, error) {
	if _, ok := f[key]; !ok {
		return false, nil, nil
	}
	m, err := f.Object(key)
	return true, m, err
}

// Object decodes a JSON object value (for example the data of a setup version).
func (f Fields) Object(key string) (map[string]any, error) {
	raw, ok := f[key]
	if !ok || isNull(raw) {
		return nil, fmt.Errorf("field %s is required: %w", key, ErrInvalidFields)
	}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, fmt.Errorf("field %s: %w", key, ErrInvalidFields)
	}
	return m, nil
}
