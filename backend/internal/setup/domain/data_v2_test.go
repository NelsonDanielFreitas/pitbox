package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	sampleID = "01a1990b-b4dd-777f-9c5b-e31aaf013560"
	engineID = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60"
)

func v2(values map[string]any) map[string]any {
	return map[string]any{"template": map[string]any{"id": sampleID, "version": float64(1)}, "values": values}
}

func TestValidateV2(t *testing.T) {
	long := strings.Repeat("x", 51)
	tests := []struct {
		name string
		data map[string]any
		rule string
	}{
		{"empty values", v2(map[string]any{}), ""},
		{"valid", v2(map[string]any{"shock_pos": "2", "disk": true, "diff_oil": float64(7000), "caster": 2.5, "notes": "ok\nline"}), ""},
		{"nil data", nil, RuleType},
		{"unknown top key", map[string]any{"template": map[string]any{"id": sampleID, "version": float64(1)}, "extra": 1}, RuleUnknown},
		{"canonical from client", func() map[string]any {
			d := v2(map[string]any{})
			d["canonical"] = map[string]any{"diffs.front_cst": float64(500)}
			return d
		}(), RuleUnknown},
		{"no template", map[string]any{"values": map[string]any{}}, RuleTemplateUnknown},
		{"wrong template id", map[string]any{"template": map[string]any{"id": engineID, "version": float64(1)}, "values": map[string]any{}}, RuleTemplateUnknown},
		{"wrong template version", map[string]any{"template": map[string]any{"id": sampleID, "version": float64(2)}, "values": map[string]any{}}, RuleTemplateUnknown},
		{"extra template key", map[string]any{"template": map[string]any{"id": sampleID, "version": float64(1), "x": 1}, "values": map[string]any{}}, RuleUnknown},
		{"values not object", map[string]any{"template": map[string]any{"id": sampleID, "version": float64(1)}, "values": []any{}}, RuleType},
		{"unknown key", v2(map[string]any{"nope": 1}), RuleUnknown},
		{"null value", v2(map[string]any{"disk": nil}), RuleType},
		{"option unknown", v2(map[string]any{"shock_pos": "4"}), RuleOptionUnknown},
		{"choice wrong type", v2(map[string]any{"shock_pos": float64(2)}), RuleType},
		{"flag wrong type", v2(map[string]any{"disk": "yes"}), RuleType},
		{"int fractional", v2(map[string]any{"diff_oil": 500.5}), RuleType},
		{"int below", v2(map[string]any{"diff_oil": float64(99)}), RuleRange},
		{"int above", v2(map[string]any{"diff_oil": float64(1000001)}), RuleRange},
		{"step ok", v2(map[string]any{"caster": 0.5}), ""},
		{"step off", v2(map[string]any{"caster": 0.7}), RuleStep},
		{"above max", v2(map[string]any{"caster": float64(6.5)}), RuleRange},
		{"text too long", v2(map[string]any{"notes": long}), RuleLength},
		{"text control char", v2(map[string]any{"notes": "a\x00"}), RuleType},
		{"refs ok", func() map[string]any {
			d := v2(map[string]any{})
			d["refs"] = map[string]any{"engine_id": engineID}
			return d
		}(), ""},
		{"refs unknown", func() map[string]any { d := v2(map[string]any{}); d["refs"] = map[string]any{"x": engineID}; return d }(), RuleUnknown},
		{"refs bad uuid", func() map[string]any {
			d := v2(map[string]any{})
			d["refs"] = map[string]any{"engine_id": "no"}
			return d
		}(), RuleUUID},
		{"too deep", v2(map[string]any{"notes": []any{[]any{[]any{[]any{[]any{[]any{[]any{[]any{1}}}}}}}}}), RuleDepth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateV2(sample(), tc.data)
			if tc.rule == "" {
				require.NoError(t, err)
				return
			}
			require.Equal(t, tc.rule, ruleOf(t, err))
		})
	}
}

func TestValidateV2ErrorsNeverEchoValues(t *testing.T) {
	secret := "SECRET-VALUE-123"
	for _, data := range []map[string]any{
		v2(map[string]any{"shock_pos": secret}),
		v2(map[string]any{secret: 1}),
		v2(map[string]any{"notes": 12345}),
		{"template": map[string]any{"id": secret, "version": float64(1)}},
	} {
		_, err := ValidateV2(sample(), data)
		require.Error(t, err)
		require.NotContains(t, err.Error(), secret)
	}
}

func TestMarshalV2ComputesCanonical(t *testing.T) {
	raw, err := MarshalV2(sample(), v2(map[string]any{"diff_oil": float64(7000), "disk": true}))
	require.NoError(t, err)
	require.JSONEq(t, `{
		"template": {"id":"`+sampleID+`","version":1},
		"values": {"diff_oil":7000,"disk":true},
		"canonical": {"diffs.front_cst":7000}
	}`, string(raw))
}

func TestMarshalV2OutputRevalidates(t *testing.T) {
	raw, err := MarshalV2(sample(), v2(map[string]any{"caster": 2.5}))
	require.NoError(t, err)
	stored, err := DecodeData(raw)
	require.NoError(t, err)
	// Stored data carries canonical, which a client may never send.
	_, err = ValidateV2(sample(), stored)
	require.Equal(t, RuleUnknown, ruleOf(t, err))
}

func TestValidateV2DoesNotMutateInput(t *testing.T) {
	data := v2(map[string]any{"diff_oil": float64(7000)})
	before, _ := json.Marshal(data)
	_, err := ValidateV2(sample(), data)
	require.NoError(t, err)
	after, _ := json.Marshal(data)
	require.JSONEq(t, string(before), string(after))
}

// v1 and the generic template must accept and refuse the same data. The two
// known differences (cross-field bars rule, control characters in text) are
// not part of the template format and are tested separately.
func TestV1EquivalentToGenericV2(t *testing.T) {
	generic, err := GenericTemplateFromV1()
	require.NoError(t, err)
	cases := []map[string]any{
		{},
		validData(),
		{"bars": map[string]any{}},
		{"nope": true},
		{"diffs": map[string]any{"front_cst": "500"}},
		{"diffs": map[string]any{"front_cst": float64(99)}},
		{"diffs": map[string]any{"front_cst": float64(1000001)}},
		{"diffs": map[string]any{"front_cst": 500.5}},
		{"diffs": nil},
		{"diffs": "x"},
		{"diffs": map[string]any{"front_cst": map[string]any{"a": 1}}},
		{"shocks": map[string]any{"front": map[string]any{"oil_cst": float64(2001)}}},
		{"shocks_front_oil_cst": float64(400)}, // flat key must not slip through
		{"engine": map[string]any{"engine_id": engineID}},
		{"engine": map[string]any{"engine_id": "not-a-uuid"}},
		{"tyres": map[string]any{"tyre_set_id": engineID}, "bars": map[string]any{"front_removed": true}},
		{"diffs": map[string]any{"fill_notes": strings.Repeat("x", 501)}},
		{"diffs": map[string]any{"fill_notes": strings.Repeat("x", 500)}},
		{"geometry": map[string]any{"front_camber_deg": -5.0, "caster_deg": 30.0}},
		{"geometry": map[string]any{"front_camber_deg": -5.1}},
		{"radio": map[string]any{"steering_expo_pct": -100.0}},
	}
	for i, data := range cases {
		_, errV1 := ValidateV1(data)
		values, refs := V1ToValues(data)
		v2data := map[string]any{"template": map[string]any{"id": generic.ID, "version": float64(1)}, "values": values}
		if len(refs) > 0 {
			v2data["refs"] = refs
		}
		_, errV2 := ValidateV2(generic, v2data)
		require.Equal(t, errV1 == nil, errV2 == nil, "case %d: v1=%v v2=%v", i, errV1, errV2)
	}
}

func TestV1ToValuesRenamesAndSplitsRefs(t *testing.T) {
	values, refs := V1ToValues(map[string]any{
		"diffs":  map[string]any{"front_cst": float64(500)},
		"shocks": map[string]any{"front": map[string]any{"oil_cst": float64(400)}},
		"engine": map[string]any{"engine_id": engineID},
		"x":      float64(1),
	})
	require.Equal(t, map[string]any{"diffs_front_cst": float64(500), "shocks_front_oil_cst": float64(400), "v1:x": float64(1)}, values)
	require.Equal(t, map[string]any{"engine_id": engineID}, refs)
}

func TestGenericCanonicalIsIdentityOnV1(t *testing.T) {
	generic, err := GenericTemplateFromV1()
	require.NoError(t, err)
	data := validData()
	values, _ := V1ToValues(data)
	canonical := ComputeCanonical(generic, values)
	require.Equal(t, map[string]any{
		"diffs.front_cst": float64(500), "diffs.rear_cst": float64(300), "shocks.front.oil_cst": float64(400),
		"transmission.clutch_bell_teeth": float64(13), "transmission.spur_teeth": float64(46),
	}, canonical)
}

func TestKnownDivergencesFromV1(t *testing.T) {
	generic, err := GenericTemplateFromV1()
	require.NoError(t, err)
	// bars: v1 refuses removed + mm together; a template has no cross-field rules.
	_, err = ValidateV1(map[string]any{"bars": map[string]any{"front_removed": true, "front_mm": float64(3)}})
	require.Error(t, err)
	_, err = ValidateV2(generic, map[string]any{
		"template": map[string]any{"id": generic.ID, "version": float64(1)},
		"values":   map[string]any{"bars_front_removed": true, "bars_front_mm": float64(3)},
	})
	require.NoError(t, err)
}

func FuzzValidateV2(f *testing.F) {
	f.Add([]byte(`{"template":{"id":"` + sampleID + `","version":1},"values":{"shock_pos":"2","caster":2.5}}`))
	f.Add([]byte(`{"template":{"id":"` + sampleID + `","version":1},"canonical":{}}`))
	f.Add([]byte(`[]`))
	tpl := sample()
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 2*MaxDataBytes {
			return
		}
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil {
			return
		}
		stored, err := MarshalV2(tpl, data)
		if err != nil {
			return
		}
		decoded, err := DecodeData(stored)
		require.NoError(t, err)
		// What the server stores has canonical; stripping it must revalidate
		// to the same canonical (idempotent).
		delete(decoded, "canonical")
		again, err := MarshalV2(tpl, decoded)
		require.NoError(t, err)
		require.JSONEq(t, string(stored), string(again))
	})
}

// FuzzValidateV1VsGeneric keeps v1 and the generic template in step on random
// input, apart from the two documented divergences.
func FuzzValidateV1VsGeneric(f *testing.F) {
	f.Add([]byte(`{"diffs":{"front_cst":500}}`))
	f.Add([]byte(`{"engine":{"engine_id":"` + engineID + `"}}`))
	generic, err := GenericTemplateFromV1()
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > MaxDataBytes {
			return
		}
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil {
			return
		}
		if strings.ContainsFunc(string(raw), func(r rune) bool { return r < 0x20 && r != '\n' && r != '\t' }) ||
			strings.Contains(string(raw), `\u00`) || strings.Contains(string(raw), `bars`) {
			return
		}
		_, errV1 := ValidateV1(data)
		values, refs := V1ToValues(data)
		d := map[string]any{"template": map[string]any{"id": generic.ID, "version": float64(1)}, "values": values}
		if len(refs) > 0 {
			d["refs"] = refs
		}
		_, errV2 := ValidateV2(generic, d)
		if (errV1 == nil) != (errV2 == nil) {
			t.Fatalf("v1 and generic v2 disagree: v1=%v v2=%v", errV1, errV2)
		}
	})
}
