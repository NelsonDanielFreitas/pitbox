package domain

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func validData() map[string]any {
	return map[string]any{
		"diffs":        map[string]any{"front_cst": float64(500), "rear_cst": float64(300)},
		"shocks":       map[string]any{"front": map[string]any{"oil_cst": float64(400)}},
		"transmission": map[string]any{"clutch_bell_teeth": float64(13), "spur_teeth": float64(46)},
	}
}

func TestValidateV1(t *testing.T) {
	tests := []struct {
		name   string
		data   map[string]any
		ruleID string
	}{
		{name: "empty", data: map[string]any{}},
		{name: "valid", data: validData()},
		{name: "empty group", data: map[string]any{"bars": map[string]any{}}},
		{name: "unknown field", data: map[string]any{"nope": true}, ruleID: RuleUnknown},
		{name: "wrong type", data: map[string]any{"diffs": map[string]any{"front_cst": "500"}}, ruleID: RuleType},
		{name: "below range", data: map[string]any{"diffs": map[string]any{"front_cst": float64(99)}}, ruleID: RuleRange},
		{name: "above range", data: map[string]any{"diffs": map[string]any{"front_cst": float64(1000001)}}, ruleID: RuleRange},
		{name: "shock oil below", data: map[string]any{"shocks": map[string]any{"front": map[string]any{"oil_cst": float64(99)}}}, ruleID: RuleRange},
		{name: "shock oil above", data: map[string]any{"shocks": map[string]any{"front": map[string]any{"oil_cst": float64(2001)}}}, ruleID: RuleRange},
		{name: "null", data: map[string]any{"diffs": nil}, ruleID: RuleType},
		{name: "bars conflict", data: map[string]any{"bars": map[string]any{"front_removed": true, "front_mm": float64(3)}}, ruleID: RuleBarsConflict},
		{name: "bad uuid", data: map[string]any{"engine": map[string]any{"engine_id": "not-a-uuid"}}, ruleID: RuleUUID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateV1(tc.data)
			if tc.ruleID == "" {
				require.NoError(t, err)
				return
			}
			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			require.Equal(t, tc.ruleID, ve.RuleID)
		})
	}
}

func TestEmptyGroupsAreNormalizedAway(t *testing.T) {
	normalized, err := ValidateV1(map[string]any{"bars": map[string]any{}, "diffs": map[string]any{"front_cst": float64(500)}})
	require.NoError(t, err)
	require.NotContains(t, normalized, "bars")
	raw, err := MarshalV1(map[string]any{"bars": map[string]any{}})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(raw))
}

func TestMarshalV1NilIsAnEmptyObject(t *testing.T) {
	raw, err := MarshalV1(nil)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(raw))
}

func TestValidateV1Limits(t *testing.T) {
	deep := any("x")
	for i := 0; i < MaxDataDepth; i++ {
		deep = map[string]any{"x": deep}
	}
	_, err := ValidateV1(map[string]any{"diffs": deep})
	require.Error(t, err)

	huge := map[string]any{"diffs": map[string]any{"fill_notes": strings.Repeat("x", MaxDataBytes)}}
	_, err = ValidateV1(huge)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	require.Equal(t, RuleSize, ve.RuleID)
}

func TestCompare(t *testing.T) {
	a := map[string]any{
		"diffs": map[string]any{"front_cst": float64(500), "rear_cst": float64(300)},
		"bars":  map[string]any{"front_mm": float64(3)},
		"engine": map[string]any{
			"engine_id": "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
		},
	}
	b := map[string]any{
		"diffs": map[string]any{"front_cst": float64(600), "center_cst": float64(400)},
		"bars":  map[string]any{"rear_mm": float64(2)},
		"engine": map[string]any{
			"engine_id": "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
		},
	}
	diffs := Compare(a, b)
	require.Equal(t, []string{"bars.front_mm", "bars.rear_mm", "diffs.center_cst", "diffs.front_cst", "diffs.rear_cst"}, paths(diffs))
	require.Equal(t, DifferenceRemoved, diffs[0].Kind)
	require.Equal(t, DifferenceAdded, diffs[1].Kind)
	require.Equal(t, DifferenceAdded, diffs[2].Kind)
	require.Equal(t, DifferenceChanged, diffs[3].Kind)
	require.Equal(t, DifferenceRemoved, diffs[4].Kind)
	require.Empty(t, Compare(a, a))
}

func TestCompareNumbersByValue(t *testing.T) {
	d := Compare(map[string]any{"n": float64(5000)}, map[string]any{"n": float64(5000.0)})
	require.Empty(t, d)
	d = Compare(map[string]any{"n": 5000}, map[string]any{"n": int64(5000)})
	require.Empty(t, d)
}

func paths(d []Difference) []string {
	out := make([]string, 0, len(d))
	for _, x := range d {
		out = append(out, x.Path)
	}
	return out
}

func FuzzValidateV1(f *testing.F) {
	f.Add([]byte(`{"diffs":{"front_cst":500}}`))
	f.Add([]byte(`{"bars":{"front_removed":true,"front_mm":2}}`))
	f.Add([]byte(`{"engine":{"engine_id":"0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60"}}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 2*MaxDataBytes {
			return
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			return
		}
		validated, err := ValidateV1(data)
		if err != nil {
			return
		}
		canonical, err := MarshalV1(validated)
		if err != nil {
			t.Fatalf("validated data does not marshal: %v", err)
		}
		decoded, err := DecodeData(canonical)
		if err != nil {
			t.Fatalf("canonical data does not decode: %v", err)
		}
		roundTrip, err := ValidateV1(decoded)
		if err != nil {
			t.Fatalf("canonical data does not validate: %v", err)
		}
		again, err := MarshalV1(roundTrip)
		if err != nil || !bytes.Equal(canonical, again) {
			t.Fatalf("round trip changed the canonical JSON: %v", err)
		}
	})
}

func FuzzCompare(f *testing.F) {
	f.Add([]byte(`{"a":1}`), []byte(`{"a":2}`))
	f.Add([]byte(`{"a":{"b":1}}`), []byte(`{"a":{}}`))
	f.Add([]byte(`{"a":[1,2]}`), []byte(`{"a":[2,3,4]}`))
	f.Fuzz(func(t *testing.T, rawA, rawB []byte) {
		var a, b map[string]any
		if json.Unmarshal(rawA, &a) != nil || json.Unmarshal(rawB, &b) != nil {
			return
		}
		if len(Compare(a, a)) != 0 {
			t.Fatal("Compare(a, a) is not empty")
		}
		ab, ba := Compare(a, b), Compare(b, a)
		if len(ab) != len(ba) {
			t.Fatalf("asymmetry: %d vs %d", len(ab), len(ba))
		}
		seen := make(map[string]struct{}, len(ab))
		for i, d := range ab {
			if _, ok := seen[d.Path]; ok {
				t.Fatalf("duplicate path %q", d.Path)
			}
			seen[d.Path] = struct{}{}
			if i > 0 && ab[i-1].Path >= d.Path {
				t.Fatalf("paths are not sorted: %q then %q", ab[i-1].Path, d.Path)
			}
			want := ba[i]
			if want.Path != d.Path {
				t.Fatalf("reverse path %q, want %q", want.Path, d.Path)
			}
			switch d.Kind {
			case DifferenceAdded:
				if want.Kind != DifferenceRemoved {
					t.Fatalf("added did not become removed: %v", want.Kind)
				}
			case DifferenceRemoved:
				if want.Kind != DifferenceAdded {
					t.Fatalf("removed did not become added: %v", want.Kind)
				}
			case DifferenceChanged:
				if want.Kind != DifferenceChanged {
					t.Fatalf("changed did not stay changed: %v", want.Kind)
				}
			}
		}
	})
}

type compareVector struct {
	Name        string         `json:"name"`
	Before      map[string]any `json:"before"`
	After       map[string]any `json:"after"`
	Differences []struct {
		Path   string `json:"path"`
		Kind   string `json:"kind"`
		Before any    `json:"before"`
		After  any    `json:"after"`
	} `json:"differences"`
}

func TestCompareVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/compare/v1.json")
	require.NoError(t, err)
	var vectors []compareVector
	require.NoError(t, json.Unmarshal(raw, &vectors))
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			got := Compare(v.Before, v.After)
			require.Len(t, got, len(v.Differences))
			for i, want := range v.Differences {
				require.Equal(t, want.Path, got[i].Path)
				require.Equal(t, DifferenceKind(want.Kind), got[i].Kind)
				require.Equal(t, want.Before, got[i].Before)
				require.Equal(t, want.After, got[i].After)
			}
		})
	}
}
