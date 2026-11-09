package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Fixed identity of the generic template. It is written once and never
// changes: the phone and the server must agree on it without a network.
const (
	GenericTemplateID   = "01a1990b-b4dd-777f-9c5b-e31aaf01355f"
	GenericTemplateSlug = "generic-1-8-nitro"
)

// V1RefPaths are the v1 uuid leaves; in v2 they live in data.refs.
var v1RefPaths = map[string]string{
	"engine.engine_id":  "engine_id",
	"tyres.tyre_set_id": "tyre_set_id",
}

var genericGroupLabels = map[string]string{
	"diffs": "Diferenciais", "shocks": "Amortecedores", "geometry": "Geometria", "bars": "Barras",
	"aero": "Aerodinâmica", "transmission": "Transmissão", "engine": "Motor", "tyres": "Pneus",
	"weight": "Peso", "radio": "Rádio",
}

// orderedRule reads schema_v1.json keeping the order of its keys, which a Go
// map would lose (the sheet must show fields in the file order).
type orderedRule struct {
	fieldRule
	Ordered []orderedEntry
}

type orderedEntry struct {
	Key  string
	Rule orderedRule
}

func (r *orderedRule) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &r.fieldRule); err != nil {
		return err
	}
	var raw struct {
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(b, &raw); err != nil || len(raw.Fields) == 0 {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw.Fields))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var child orderedRule
		if err := dec.Decode(&child); err != nil {
			return err
		}
		r.Ordered = append(r.Ordered, orderedEntry{Key: kt.(string), Rule: child})
	}
	return nil
}

// GenericTemplateFromV1 builds the generic template from the embedded
// schema_v1.json. Every field has canonical = its own v1 path, so converting
// v1 data is only a rename of keys.
func GenericTemplateFromV1() (Template, error) {
	if schemaV1Err != nil {
		return Template{}, schemaV1Err
	}
	var root struct {
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(schemaV1JSON, &root); err != nil {
		return Template{}, fmt.Errorf("setup: generic template: %w", err)
	}
	top := orderedRule{}
	if err := top.UnmarshalJSON([]byte(`{"fields":` + string(root.Fields) + `}`)); err != nil {
		return Template{}, fmt.Errorf("setup: generic template: %w", err)
	}

	t := Template{
		Format: templateFormat, ID: GenericTemplateID, Slug: GenericTemplateSlug, Version: 1,
		Make: "Genérico", Model: "1/8 TT nitro", Scale: "1/8", Powertrain: "nitro",
		Name: LocalText{PT: "Genérico 1/8 TT nitro", EN: "Generic 1/8 off-road nitro"},
	}
	for _, g := range top.Ordered {
		group := TemplateGroup{Key: g.Key, Label: LocalText{PT: genericGroupLabels[g.Key], EN: titleWords(g.Key)}}
		var add func(prefix string, entries []orderedEntry)
		add = func(prefix string, entries []orderedEntry) {
			for _, e := range entries {
				path := joinPath(prefix, e.Key)
				switch e.Rule.Type {
				case "group":
					add(path, e.Rule.Ordered)
				case "uuid":
				default:
					group.Fields = append(group.Fields, genericField(path, e.Rule.fieldRule))
				}
			}
		}
		add(g.Key, g.Rule.Ordered)
		t.Groups = append(t.Groups, group)
	}
	if err := ValidateTemplate(t); err != nil {
		return Template{}, fmt.Errorf("setup: generic template is invalid: %w", err)
	}
	return t, nil
}

func genericField(path string, r fieldRule) TemplateField {
	label := titleWords(strings.ReplaceAll(path, ".", " "))
	f := TemplateField{Key: strings.ReplaceAll(path, ".", "_"), Label: LocalText{PT: label, EN: label}, Canonical: path}
	switch r.Type {
	case "int":
		f.Type, f.Min, f.Max, f.Unit = FieldInt, r.Min, r.Max, unitFor(path)
	case "number":
		f.Type, f.Min, f.Max, f.Unit = FieldNumber, r.Min, r.Max, unitFor(path)
	case "bool":
		f.Type = FieldFlag
	case "string":
		f.Type, f.MaxLen = FieldText, *r.MaxLen
	}
	return f
}

func unitFor(path string) string {
	for _, u := range []struct{ suffix, unit string }{
		{"_gf_mm", "gf/mm"}, {"_cst", "cSt"}, {"_ml", "ml"}, {"_mm", "mm"}, {"_deg", "°"},
		{"_c", "°C"}, {"_g", "g"}, {"_pct", "%"}, {"_kpa", "kPa"}, {"_min", "min"},
	} {
		if strings.HasSuffix(path, u.suffix) {
			return u.unit
		}
	}
	return ""
}

func titleWords(s string) string {
	words := strings.Fields(strings.ReplaceAll(s, "_", " "))
	if len(words) == 0 {
		return s
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ")
}

// V1ToValues converts v1 data to template values and refs, in memory only:
// stored v1 rows are never rewritten. A path that is not a known v1 leaf keeps
// a "v1:" prefix, which is never a valid template key, so ValidateV2 refuses
// it exactly like v1 would.
func V1ToValues(data map[string]any) (values, refs map[string]any) {
	values, refs = map[string]any{}, map[string]any{}
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			path := joinPath(prefix, k)
			if child, ok := v.(map[string]any); ok {
				// An empty object is only valid where v1 has a group.
				if _, isGroup := v1GroupPaths[path]; len(child) > 0 || isGroup {
					walk(path, child)
					continue
				}
			}
			switch {
			case v1RefPaths[path] != "":
				refs[v1RefPaths[path]] = v
			case hasCanonical(path):
				values[strings.ReplaceAll(path, ".", "_")] = v
			default:
				values["v1:"+path] = v
			}
		}
	}
	walk("", data)
	return values, refs
}

func hasCanonical(path string) bool { _, ok := canonicalSpecs[path]; return ok }
