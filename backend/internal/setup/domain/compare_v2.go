package domain

import (
	"reflect"
	"sort"
)

// VersionView is what CompareVersions needs of a setup version: the template
// it was made with and its filled values and references.
type VersionView struct {
	Template Template
	Values   map[string]any
	Refs     map[string]any
}

// CompareResult lists the differences and, when two different templates are
// compared, the keys that had no counterpart and were left out.
type CompareResult struct {
	Differences []Difference
	Skipped     []string
}

// ViewFromV1 reads stored v1 data as the generic template.
func ViewFromV1(generic Template, data map[string]any) VersionView {
	values, refs := V1ToValues(data)
	return VersionView{Template: generic, Values: values, Refs: refs}
}

// ViewFromV2 reads stored v2 data (already validated on write).
func ViewFromV2(t Template, data map[string]any) VersionView {
	values, _ := data["values"].(map[string]any)
	refs, _ := data["refs"].(map[string]any)
	return VersionView{Template: t, Values: values, Refs: refs}
}

// CompareVersions compares two versions. Same template id: by key, whatever
// the version number (keys are stable). Different templates: by canonical path
// only, and Skipped says what could not be compared.
func CompareVersions(a, b VersionView) CompareResult {
	var left, right map[string]any
	var multiDiffs []Difference
	skipped := map[string]struct{}{}
	if a.Template.ID == b.Template.ID {
		// A multi value is one value (the set of ticked options), not a list to compare by
		// position: ticking E0159 in front of E0160 must not read as two changes.
		multi := multiKeys(a.Template, b.Template)
		left, right, multiDiffs = withoutKeys(a.Values, multi), withoutKeys(b.Values, multi), compareMulti(a.Values, b.Values, multi)
	} else {
		left, right = ComputeCanonical(a.Template, a.Values), ComputeCanonical(b.Template, b.Values)
		for _, v := range []VersionView{a, b} {
			canon := map[string]struct{}{}
			for _, g := range v.Template.Groups {
				for _, f := range g.Fields {
					if f.Canonical != "" {
						canon[f.Key] = struct{}{}
					}
				}
			}
			for k := range v.Values {
				if _, ok := canon[k]; !ok {
					skipped[k] = struct{}{}
				}
			}
		}
	}
	diffs := append(Compare(left, right), multiDiffs...)
	diffs = append(diffs, prefixed("refs", Compare(a.Refs, b.Refs))...)
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })

	res := CompareResult{Differences: diffs, Skipped: make([]string, 0, len(skipped))}
	for k := range skipped {
		res.Skipped = append(res.Skipped, k)
	}
	sort.Strings(res.Skipped)
	return res
}

// multiKeys lists the keys of the multi fields of either version of the template.
func multiKeys(templates ...Template) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range templates {
		for _, g := range t.Groups {
			for _, f := range g.Fields {
				if f.Type == FieldMulti {
					out[f.Key] = struct{}{}
				}
			}
		}
	}
	return out
}

func withoutKeys(values map[string]any, drop map[string]struct{}) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		if _, skip := drop[k]; !skip {
			out[k] = v
		}
	}
	return out
}

// compareMulti compares multi values as wholes. Values are normalised (option order) when
// stored, so equal ticks are equal lists.
func compareMulti(a, b map[string]any, keys map[string]struct{}) []Difference {
	var out []Difference
	for k := range keys {
		av, aok := a[k]
		bv, bok := b[k]
		switch {
		case aok && bok:
			if !reflect.DeepEqual(av, bv) {
				out = append(out, Difference{Path: k, Kind: DifferenceChanged, Before: av, After: bv})
			}
		case bok:
			out = append(out, Difference{Path: k, Kind: DifferenceAdded, After: bv})
		case aok:
			out = append(out, Difference{Path: k, Kind: DifferenceRemoved, Before: av})
		}
	}
	return out
}

func prefixed(prefix string, in []Difference) []Difference {
	for i := range in {
		in[i].Path = prefix + "." + in[i].Path
	}
	return in
}
