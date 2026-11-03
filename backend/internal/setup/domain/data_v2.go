package domain

import (
	"encoding/json"
	"math"

	"github.com/google/uuid"
)

const SchemaVersionV2 = 2

var refKeys = map[string]struct{}{"engine_id": {}, "tyre_set_id": {}}

// validateFieldValue checks one value against its template field. The error
// never carries the value.
func validateFieldValue(p string, f TemplateField, v any) error {
	if v == nil {
		return tplErr(p, RuleType)
	}
	switch f.Type {
	case FieldInt, FieldNumber:
		n, ok := finiteNumber(v)
		if !ok || (f.Type == FieldInt && math.Trunc(n) != n) {
			return tplErr(p, RuleType)
		}
		if f.Min == nil || f.Max == nil || n < *f.Min || n > *f.Max {
			return tplErr(p, RuleRange)
		}
		if f.Step != nil {
			q := (n - *f.Min) / *f.Step
			if math.Abs(q-math.Round(q)) > 1e-9 {
				return tplErr(p, RuleStep)
			}
		}
	case FieldChoice:
		s, ok := v.(string)
		if !ok {
			return tplErr(p, RuleType)
		}
		for _, o := range f.Options {
			if o == s {
				return nil
			}
		}
		return tplErr(p, RuleOptionUnknown)
	case FieldMulti:
		_, err := normalizeMulti(p, f, v)
		return err
	case FieldFlag:
		if _, ok := v.(bool); !ok {
			return tplErr(p, RuleType)
		}
	case FieldText:
		s, ok := v.(string)
		if !ok || hasControl(s, true) {
			return tplErr(p, RuleType)
		}
		if runeLen(s) > f.MaxLen {
			return tplErr(p, RuleLength)
		}
	default:
		return tplErr(p, RuleType)
	}
	return nil
}

// normalizeMulti checks a multi value (a non-empty list of distinct options of
// the field) and returns it in the order of the options, so two devices that
// tick the same boxes in a different order store the same value. An empty
// list is refused: "nothing ticked" is a value that is not stored.
func normalizeMulti(p string, f TemplateField, v any) ([]any, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, tplErr(p, RuleType)
	}
	if len(list) == 0 || len(list) > len(f.Options) {
		return nil, tplErr(p, RuleLength)
	}
	chosen := map[string]struct{}{}
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, tplErr(p, RuleType)
		}
		if _, dup := chosen[s]; dup {
			return nil, tplErr(p, RuleOptionUnknown)
		}
		chosen[s] = struct{}{}
	}
	out := make([]any, 0, len(chosen))
	for _, o := range f.Options {
		if _, ok := chosen[o]; ok {
			out = append(out, o)
			delete(chosen, o)
		}
	}
	if len(chosen) != 0 { // something ticked that the field does not offer
		return nil, tplErr(p, RuleOptionUnknown)
	}
	return out, nil
}

// ValidateV2 checks setup data against one template version. It accepts only
// the keys template, values and refs (canonical is computed by the server) and
// returns a normalised copy; the input is never mutated.
func ValidateV2(t Template, data map[string]any) (map[string]any, error) {
	if err := ValidateTemplate(t); err != nil {
		return nil, tplErr("template", RuleTemplateUnknown)
	}
	if data == nil {
		return nil, tplErr("data", RuleType)
	}
	if exceedsDepth(data, MaxDataDepth) {
		return nil, tplErr("data", RuleDepth)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, tplErr("data", RuleType)
	}
	if len(raw) > MaxDataBytes {
		return nil, tplErr("data", RuleSize)
	}
	for _, k := range sortedKeys(data) {
		if k != "template" && k != "values" && k != "refs" {
			return nil, tplErr("data", RuleUnknown) // never echo a client-chosen key
		}
	}

	ref, ok := data["template"].(map[string]any)
	if !ok {
		return nil, tplErr("template", RuleTemplateUnknown)
	}
	for _, k := range sortedKeys(ref) {
		if k != "id" && k != "version" {
			return nil, tplErr("template", RuleUnknown)
		}
	}
	id, _ := ref["id"].(string)
	ver, _ := finiteNumber(ref["version"])
	if id != t.ID || ver != float64(t.Version) {
		return nil, tplErr("template", RuleTemplateUnknown)
	}

	values := map[string]any{}
	if raw, present := data["values"]; present {
		in, ok := raw.(map[string]any)
		if !ok {
			return nil, tplErr("values", RuleType)
		}
		index := t.fieldIndex()
		for _, k := range sortedKeys(in) {
			f, known := index[k]
			if !known {
				return nil, tplErr("values", RuleUnknown)
			}
			if err := validateFieldValue(joinPath("values", k), f, in[k]); err != nil {
				return nil, err
			}
			values[k] = in[k]
			if f.Type == FieldMulti {
				values[k], _ = normalizeMulti("", f, in[k]) // already valid: only the order changes
			}
		}
	}

	out := map[string]any{
		"template": map[string]any{"id": t.ID, "version": float64(t.Version)},
		"values":   values,
	}
	if raw, present := data["refs"]; present {
		in, ok := raw.(map[string]any)
		if !ok {
			return nil, tplErr("refs", RuleType)
		}
		refs := map[string]any{}
		for _, k := range sortedKeys(in) {
			p := joinPath("refs", k)
			if _, known := refKeys[k]; !known {
				return nil, tplErr("refs", RuleUnknown)
			}
			s, ok := in[k].(string)
			if !ok {
				return nil, tplErr(p, RuleType)
			}
			if id, err := uuid.Parse(s); err != nil || id == uuid.Nil {
				return nil, tplErr(p, RuleUUID)
			}
			refs[k] = s
		}
		if len(refs) > 0 {
			out["refs"] = refs
		}
	}
	return out, nil
}

// MarshalV2 validates, adds the server-computed canonical map and returns the
// JSON stored in setup_version.data.
func MarshalV2(t Template, data map[string]any) ([]byte, error) {
	validated, err := ValidateV2(t, data)
	if err != nil {
		return nil, err
	}
	values, _ := validated["values"].(map[string]any)
	validated["canonical"] = ComputeCanonical(t, values)
	raw, err := json.Marshal(validated)
	if err != nil {
		return nil, tplErr("data", RuleType)
	}
	if len(raw) > MaxDataBytes {
		return nil, tplErr("data", RuleSize)
	}
	return raw, nil
}
