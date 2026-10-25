package domain

import (
	"encoding/json"
	"sort"
	"strconv"
)

// Compare returns the leaf differences between two setup versions, ordered by
// path. It is pure and works on the JSON shape, so future custom fields compare
// without changes.
func Compare(a, b map[string]any) []Difference {
	out := make([]Difference, 0)
	compareMaps("", a, b, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func compareMaps(path string, a, b map[string]any, out *[]Difference) {
	keys := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	for _, key := range sorted {
		av, aok := a[key]
		bv, bok := b[key]
		p := joinPath(path, key)
		switch {
		case !aok:
			addLeaves(p, bv, DifferenceAdded, out)
		case !bok:
			addLeaves(p, av, DifferenceRemoved, out)
		default:
			compareValues(p, av, bv, out)
		}
	}
}

func compareValues(path string, a, b any, out *[]Difference) {
	am, aIsMap := a.(map[string]any)
	bm, bIsMap := b.(map[string]any)
	if aIsMap && bIsMap {
		compareMaps(path, am, bm, out)
		return
	}
	aa, aIsArray := a.([]any)
	ba, bIsArray := b.([]any)
	if aIsArray && bIsArray {
		compareArrays(path, aa, ba, out)
		return
	}
	an, aIsNumber := numberValue(a)
	bn, bIsNumber := numberValue(b)
	if aIsNumber && bIsNumber {
		if an != bn {
			*out = append(*out, Difference{Path: path, Before: a, After: b, Kind: DifferenceChanged})
		}
		return
	}
	if !sameScalar(a, b) {
		*out = append(*out, Difference{Path: path, Before: a, After: b, Kind: DifferenceChanged})
	}
}

func compareArrays(path string, a, b []any, out *[]Difference) {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		p := path + "[" + strconv.Itoa(i) + "]"
		switch {
		case i >= len(a):
			addLeaves(p, b[i], DifferenceAdded, out)
		case i >= len(b):
			addLeaves(p, a[i], DifferenceRemoved, out)
		default:
			compareValues(p, a[i], b[i], out)
		}
	}
}

func addLeaves(path string, v any, kind DifferenceKind, out *[]Difference) {
	switch value := v.(type) {
	case map[string]any:
		for _, key := range sortedKeys(value) {
			addLeaves(joinPath(path, key), value[key], kind, out)
		}
	case []any:
		for i, child := range value {
			addLeaves(path+"["+strconv.Itoa(i)+"]", child, kind, out)
		}
	default:
		if kind == DifferenceAdded {
			*out = append(*out, Difference{Path: path, After: v, Kind: kind})
		} else {
			*out = append(*out, Difference{Path: path, Before: v, Kind: kind})
		}
	}
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func sameScalar(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case nil:
		return b == nil
	default:
		return false
	}
}
