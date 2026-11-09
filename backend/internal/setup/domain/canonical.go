package domain

import "sort"

// canonicalSpec is the v1 leaf a template field can map to. Types and ranges
// come from schema_v1.json, never from a hand-written list.
type canonicalSpec struct {
	Type   string
	Min    *float64
	Max    *float64
	MaxLen *int
}

var canonicalSpecs, v1GroupPaths = buildCanonicalSpecs()

func buildCanonicalSpecs() (map[string]canonicalSpec, map[string]struct{}) {
	out := map[string]canonicalSpec{}
	groups := map[string]struct{}{}
	var walk func(prefix string, fields map[string]fieldRule)
	walk = func(prefix string, fields map[string]fieldRule) {
		for key, rule := range fields {
			path := joinPath(prefix, key)
			switch rule.Type {
			case "group":
				groups[path] = struct{}{}
				walk(path, rule.Fields)
			case "uuid":
				// References live in data.refs, not in template values.
			default:
				out[path] = canonicalSpec{Type: rule.Type, Min: rule.Min, Max: rule.Max, MaxLen: rule.MaxLen}
			}
		}
	}
	walk("", schemaV1.Fields)
	return out, groups
}

// CanonicalPaths returns the v1 leaf paths a template field may use as
// `canonical`, sorted. UUID references are not included.
func CanonicalPaths() []string {
	paths := make([]string, 0, len(canonicalSpecs))
	for p := range canonicalSpecs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// canonicalCompatible reports whether every value the field accepts is also a
// valid v1 value at the canonical path, so a canonical map is always valid v1.
func canonicalCompatible(f TemplateField, spec canonicalSpec) bool {
	switch spec.Type {
	case "int":
		return f.Type == FieldInt && withinRange(f, spec)
	case "number":
		return (f.Type == FieldNumber || f.Type == FieldInt) && withinRange(f, spec)
	case "bool":
		return f.Type == FieldFlag
	case "string":
		if spec.MaxLen == nil {
			return f.Type == FieldText || f.Type == FieldChoice
		}
		switch f.Type {
		case FieldText:
			return f.MaxLen <= *spec.MaxLen
		case FieldChoice:
			for _, o := range f.Options {
				if runeLen(o) > *spec.MaxLen {
					return false
				}
			}
			return true
		}
	}
	return false
}

func withinRange(f TemplateField, spec canonicalSpec) bool {
	if f.Min == nil || f.Max == nil {
		return false
	}
	if spec.Min != nil && *f.Min < *spec.Min {
		return false
	}
	if spec.Max != nil && *f.Max > *spec.Max {
		return false
	}
	return true
}

// ComputeCanonical maps the filled values of a template to v1 paths. The
// server stores the result in data.canonical; the client never sends it.
func ComputeCanonical(t Template, values map[string]any) map[string]any {
	out := map[string]any{}
	for _, g := range t.Groups {
		for _, f := range g.Fields {
			if f.Canonical == "" {
				continue
			}
			if v, ok := values[f.Key]; ok {
				out[f.Canonical] = v
			}
		}
	}
	return out
}
