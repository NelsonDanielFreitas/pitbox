package domain

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// The vectors are shared with the app (app/src/features/setups/domain/*.test.ts): Go and
// TypeScript must give the same answer. UPDATE_GOLDEN=1 rewrites the expected results from
// this implementation; review the diff like any other change.

const (
	templateVectorsFile = "testdata/template/vectors.json"
	compareV2File       = "testdata/compare/v2.json"
)

func loadTemplates(t *testing.T, raw map[string]json.RawMessage) map[string]Template {
	t.Helper()
	out := map[string]Template{}
	for name, r := range raw {
		tpl, err := ParseTemplate(r)
		require.NoError(t, err, name)
		out[name] = tpl
	}
	return out
}

func writeGolden(t *testing.T, file string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", " ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, append(raw, '\n'), 0o600))
}

type diffJSON struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

func diffsJSON(in []Difference) []diffJSON {
	out := make([]diffJSON, 0, len(in))
	for _, d := range in {
		out = append(out, diffJSON{d.Path, string(d.Kind), d.Before, d.After})
	}
	return out
}

func TestSharedTemplateVectors(t *testing.T) {
	raw, err := os.ReadFile(templateVectorsFile)
	require.NoError(t, err)
	var f struct {
		Templates map[string]json.RawMessage `json:"templates"`
		Validate  []struct {
			Name     string         `json:"name"`
			Template string         `json:"template"`
			Data     map[string]any `json:"data"`
			OK       bool           `json:"ok"`
			Path     string         `json:"path,omitempty"`
			Rule     string         `json:"rule,omitempty"`
			Out      map[string]any `json:"out,omitempty"`
		} `json:"validate"`
		V1 []struct {
			Name   string         `json:"name"`
			Data   map[string]any `json:"data"`
			Values map[string]any `json:"values"`
			Refs   map[string]any `json:"refs"`
		} `json:"v1_to_values"`
	}
	require.NoError(t, json.Unmarshal(raw, &f))
	tpls := loadTemplates(t, f.Templates)
	update := os.Getenv("UPDATE_GOLDEN") == "1"

	for i := range f.Validate {
		v := &f.Validate[i]
		t.Run("validate/"+v.Name, func(t *testing.T) {
			out, err := ValidateV2(tpls[v.Template], v.Data)
			var ok bool
			var path, rule string
			var ve *ValidationError
			switch {
			case err == nil:
				ok = true
			case errors.As(err, &ve):
				path, rule = ve.Path, ve.RuleID
			default:
				t.Fatalf("unexpected error type: %v", err)
			}
			if update {
				v.OK, v.Path, v.Rule, v.Out = ok, path, rule, out
				return
			}
			require.Equal(t, v.OK, ok)
			require.Equal(t, v.Path, path)
			require.Equal(t, v.Rule, rule)
			if ok {
				require.Equal(t, v.Out, out)
			}
		})
	}

	for i := range f.V1 {
		v := &f.V1[i]
		t.Run("v1/"+v.Name, func(t *testing.T) {
			values, refs := V1ToValues(v.Data)
			if update {
				v.Values, v.Refs = values, refs
				return
			}
			require.Equal(t, v.Values, values)
			require.Equal(t, v.Refs, refs)
		})
	}
	if update {
		writeGolden(t, templateVectorsFile, f)
	}
}

func TestSharedCompareV2Vectors(t *testing.T) {
	raw, err := os.ReadFile(compareV2File)
	require.NoError(t, err)
	var f struct {
		Templates map[string]json.RawMessage `json:"templates"`
		Cases     []struct {
			Name string `json:"name"`
			A    struct {
				Template string         `json:"template"`
				Values   map[string]any `json:"values"`
				Refs     map[string]any `json:"refs"`
			} `json:"a"`
			B struct {
				Template string         `json:"template"`
				Values   map[string]any `json:"values"`
				Refs     map[string]any `json:"refs"`
			} `json:"b"`
			Differences []diffJSON `json:"differences"`
			Skipped     []string   `json:"skipped"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &f))
	tpls := loadTemplates(t, f.Templates)
	update := os.Getenv("UPDATE_GOLDEN") == "1"
	for i := range f.Cases {
		c := &f.Cases[i]
		t.Run(c.Name, func(t *testing.T) {
			got := CompareVersions(
				VersionView{Template: tpls[c.A.Template], Values: c.A.Values, Refs: c.A.Refs},
				VersionView{Template: tpls[c.B.Template], Values: c.B.Values, Refs: c.B.Refs},
			)
			if update {
				c.Differences, c.Skipped = diffsJSON(got.Differences), got.Skipped
				return
			}
			require.Equal(t, c.Differences, diffsJSON(got.Differences))
			require.Equal(t, c.Skipped, got.Skipped)
		})
	}
	if update {
		writeGolden(t, compareV2File, f)
	}
}
