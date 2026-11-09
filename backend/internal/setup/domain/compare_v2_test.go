package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func otherTemplate() Template {
	t := sample()
	t.ID = "01a1990b-b4dd-777f-9c5b-e31aaf013561"
	t.Slug = "xray-x8"
	t.Groups[0].Fields[2].Key = "front_diff_cst" // same canonical, different key
	return t
}

func TestCompareVersionsSameTemplateUsesKeys(t *testing.T) {
	a := VersionView{Template: sample(), Values: map[string]any{"diff_oil": float64(7000), "disk": false}}
	b := VersionView{Template: sample(), Values: map[string]any{"diff_oil": float64(8000), "caster": 2.0}, Refs: map[string]any{"engine_id": engineID}}
	res := CompareVersions(a, b)
	paths := []string{}
	for _, d := range res.Differences {
		paths = append(paths, d.Path)
	}
	require.Equal(t, []string{"caster", "diff_oil", "disk", "refs.engine_id"}, paths)
	require.Empty(t, res.Skipped)
}

func TestCompareVersionsDifferentTemplatesUseCanonical(t *testing.T) {
	a := VersionView{Template: sample(), Values: map[string]any{"diff_oil": float64(7000), "caster": 2.0}}
	b := VersionView{Template: otherTemplate(), Values: map[string]any{"front_diff_cst": float64(8000)}}
	res := CompareVersions(a, b)
	require.Len(t, res.Differences, 1)
	require.Equal(t, "diffs.front_cst", res.Differences[0].Path)
	require.Equal(t, DifferenceChanged, res.Differences[0].Kind)
	require.Equal(t, []string{"caster"}, res.Skipped)
}

func TestCompareV1WithV2GenericByKey(t *testing.T) {
	generic, err := GenericTemplateFromV1()
	require.NoError(t, err)
	old := ViewFromV1(generic, map[string]any{"diffs": map[string]any{"front_cst": float64(500)}})
	cur := ViewFromV2(generic, map[string]any{"values": map[string]any{"diffs_front_cst": float64(600)}})
	res := CompareVersions(old, cur)
	require.Len(t, res.Differences, 1)
	require.Equal(t, "diffs_front_cst", res.Differences[0].Path)
}

func TestCompareV1WithOtherTemplateUsesCanonical(t *testing.T) {
	generic, err := GenericTemplateFromV1()
	require.NoError(t, err)
	old := ViewFromV1(generic, map[string]any{"diffs": map[string]any{"front_cst": float64(500)}})
	cur := ViewFromV2(otherTemplate(), map[string]any{"values": map[string]any{"front_diff_cst": float64(500)}})
	require.Empty(t, CompareVersions(old, cur).Differences)
}

func FuzzCompareVersions(f *testing.F) {
	f.Add([]byte(`{"diff_oil":1}`), []byte(`{"front_diff_cst":2}`))
	f.Add([]byte(`{"a":{"b":1}}`), []byte(`{"a":[1]}`))
	ta, tb := sample(), otherTemplate()
	f.Fuzz(func(t *testing.T, rawA, rawB []byte) {
		var va, vb map[string]any
		if json.Unmarshal(rawA, &va) != nil || json.Unmarshal(rawB, &vb) != nil {
			return
		}
		for _, pair := range [][2]Template{{ta, ta}, {ta, tb}} {
			a, b := VersionView{Template: pair[0], Values: va}, VersionView{Template: pair[1], Values: vb}
			require.Empty(t, CompareVersions(a, a).Differences)
			ab, ba := CompareVersions(a, b), CompareVersions(b, a)
			require.Len(t, ba.Differences, len(ab.Differences))
			require.Equal(t, ab.Skipped, func() []string {
				// Skipped is the union, so it is symmetric.
				return ba.Skipped
			}())
			for i := 1; i < len(ab.Differences); i++ {
				require.Less(t, ab.Differences[i-1].Path, ab.Differences[i].Path)
			}
		}
	})
}
