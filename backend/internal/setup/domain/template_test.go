package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const genericFile = "../templates/generic-1-8-nitro.v1.json"

func marshalIndented(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	return append(raw, '\n')
}

func TestGenericTemplateFileMatchesGenerated(t *testing.T) {
	generic, err := GenericTemplateFromV1()
	require.NoError(t, err)
	want := marshalIndented(t, generic)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		require.NoError(t, os.WriteFile(genericFile, want, 0o600))
	}
	got, err := os.ReadFile(genericFile)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), "run UPDATE_GOLDEN=1 go test ./internal/setup/domain to regenerate")
	parsed, err := ParseTemplate(got)
	require.NoError(t, err)
	require.Equal(t, generic, parsed)
}

func TestGenericCoversEveryCanonicalPath(t *testing.T) {
	generic, err := GenericTemplateFromV1()
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, g := range generic.Groups {
		for _, f := range g.Fields {
			seen[f.Canonical] = true
		}
	}
	require.Len(t, seen, len(CanonicalPaths()))
	for _, p := range CanonicalPaths() {
		require.True(t, seen[p], p)
	}
}

// mugen builds a valid template to mutate in the table tests.
func sample() Template {
	f64 := func(v float64) *float64 { return &v }
	return Template{
		Format: 1, ID: "01a1990b-b4dd-777f-9c5b-e31aaf013560", Slug: "mugen-mbx8r", Version: 1,
		Make: "Mugen Seiki", Model: "MBX8R", SourceURL: "https://example.com/sheet.pdf", Scale: "1/8", Powertrain: "nitro",
		Name: LocalText{PT: "Mugen MBX8R"},
		Groups: []TemplateGroup{{
			Key: "front", Label: LocalText{PT: "Dianteira"},
			Fields: []TemplateField{
				{Key: "shock_pos", Label: LocalText{PT: "Posição"}, Type: FieldChoice, Options: []string{"1", "2", "3"}, Kit: "1",
					Diagram: &Diagram{Kind: "hole_row", Count: 3}},
				{Key: "disk", Label: LocalText{PT: "Disco"}, Type: FieldFlag, PartRef: "T2128", Kit: false},
				{Key: "diff_oil", Label: LocalText{PT: "Óleo"}, Type: FieldInt, Unit: "cSt", Min: f64(100), Max: f64(1000000), Kit: float64(7000), Canonical: "diffs.front_cst"},
				{Key: "caster", Label: LocalText{PT: "Caster"}, Type: FieldNumber, Unit: "mm", Min: f64(0), Max: f64(6), Step: f64(0.5), Kit: float64(2), Help: &LocalText{PT: "Mais caster: mais estável."}},
				{Key: "notes", Label: LocalText{PT: "Notas"}, Type: FieldText, MaxLen: 50},
			},
		}},
	}
}

func ruleOf(t *testing.T, err error) string {
	t.Helper()
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	return ve.RuleID
}

// asMulti turns the first field of the sample into a multi-choice one.
func asMulti(f *TemplateField) {
	f.Type, f.Diagram, f.Kit = FieldMulti, nil, nil
	f.Options = []string{"1", "2", "3"}
}

// tableOf lays the first group of the sample out as a two-column table.
func tableOf(t *Template, rows [][]string) {
	tb := &GroupTable{Columns: []LocalText{{PT: "Frente"}, {PT: "Trás"}}}
	for i, cells := range rows {
		tb.Rows = append(tb.Rows, TableRow{Label: LocalText{PT: fmt.Sprintf("Linha %d", i+1)}, Cells: cells})
	}
	t.Groups[0].Table = tb
}

func TestValidateTemplate(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }
	field := func(t *Template) *TemplateField { return &t.Groups[0].Fields[0] }
	tests := []struct {
		name   string
		mutate func(*Template)
		rule   string
	}{
		{"valid", func(*Template) {}, ""},
		{"bad format", func(t *Template) { t.Format = 2 }, RuleTemplateInvalid},
		{"nil uuid", func(t *Template) { t.ID = "00000000-0000-0000-0000-000000000000" }, RuleTemplateInvalid},
		{"uppercase uuid", func(t *Template) { t.ID = strings.ToUpper(t.ID) }, RuleTemplateInvalid},
		{"bad slug", func(t *Template) { t.Slug = "Bad Slug" }, RuleTemplateInvalid},
		{"version zero", func(t *Template) { t.Version = 0 }, RuleTemplateInvalid},
		{"http url", func(t *Template) { t.SourceURL = "http://example.com" }, RuleTemplateInvalid},
		{"no groups", func(t *Template) { t.Groups = nil }, RuleTemplateLimits},
		{"too many groups", func(t *Template) {
			for i := 0; i <= MaxTemplateGroups; i++ {
				t.Groups = append(t.Groups, TemplateGroup{Key: "g" + strings.Repeat("a", i%20) + string(rune('a'+i/20)), Label: LocalText{PT: "x"}, Fields: []TemplateField{{Key: "k" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Label: LocalText{PT: "x"}, Type: FieldFlag}}})
			}
		}, RuleTemplateLimits},
		{"too many fields", func(t *Template) {
			for i := 0; i < MaxTemplateFields; i++ {
				t.Groups[0].Fields = append(t.Groups[0].Fields, TemplateField{Key: "f_" + string(rune('a'+i/26/26%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i%26)), Label: LocalText{PT: "x"}, Type: FieldFlag})
			}
		}, RuleTemplateLimits},
		{"bad key", func(t *Template) { field(t).Key = "Bad-Key" }, RuleTemplateInvalid},
		{"duplicate key", func(t *Template) { t.Groups[0].Fields[1].Key = "shock_pos" }, RuleKeyDuplicate},
		{"empty label", func(t *Template) { field(t).Label.PT = " " }, RuleTemplateInvalid},
		{"label too long", func(t *Template) { field(t).Label.PT = strings.Repeat("a", MaxLabelLen+1) }, RuleTemplateInvalid},
		{"unknown type", func(t *Template) { field(t).Type = "slider" }, RuleTemplateInvalid},
		{"one option", func(t *Template) { field(t).Options = []string{"1"}; field(t).Diagram = nil }, RuleTemplateLimits},
		{"too many options", func(t *Template) {
			field(t).Diagram = nil
			field(t).Options = nil
			for i := 0; i <= MaxChoiceOptions; i++ {
				field(t).Options = append(field(t).Options, string(rune('a'+i)))
			}
		}, RuleTemplateLimits},
		{"duplicate option", func(t *Template) { field(t).Options = []string{"1", "1", "3"} }, RuleKeyDuplicate},
		{"option too long", func(t *Template) { field(t).Options = []string{"1", strings.Repeat("x", MaxOptionLen+1), "3"} }, RuleTemplateInvalid},
		{"options on flag", func(t *Template) { t.Groups[0].Fields[1].Options = []string{"a", "b"} }, RuleTemplateInvalid},
		{"range on choice", func(t *Template) { field(t).Min = f64(0) }, RuleTemplateInvalid},
		{"min above max", func(t *Template) { t.Groups[0].Fields[3].Min = f64(7) }, RuleTemplateInvalid},
		{"missing range", func(t *Template) { t.Groups[0].Fields[3].Max = nil }, RuleTemplateInvalid},
		{"fractional int range", func(t *Template) { t.Groups[0].Fields[2].Min = f64(100.5) }, RuleTemplateInvalid},
		{"zero step", func(t *Template) { t.Groups[0].Fields[3].Step = f64(0) }, RuleTemplateInvalid},
		{"step on int", func(t *Template) { t.Groups[0].Fields[2].Step = f64(1) }, RuleTemplateInvalid},
		{"text without max_len", func(t *Template) { t.Groups[0].Fields[4].MaxLen = 0 }, RuleTemplateInvalid},
		{"text max_len too big", func(t *Template) { t.Groups[0].Fields[4].MaxLen = MaxTextFieldLen + 1 }, RuleTemplateInvalid},
		{"part_ref on text", func(t *Template) { t.Groups[0].Fields[4].PartRef = "T1" }, RuleTemplateInvalid},
		{"kit not an option", func(t *Template) { field(t).Kit = "9" }, RuleOptionUnknown},
		{"kit out of range", func(t *Template) { t.Groups[0].Fields[3].Kit = float64(9) }, RuleRange},
		{"unknown canonical", func(t *Template) { t.Groups[0].Fields[2].Canonical = "diffs.nope" }, RuleCanonicalUnknown},
		{"uuid is not canonical", func(t *Template) { t.Groups[0].Fields[2].Canonical = "engine.engine_id" }, RuleCanonicalUnknown},
		{"canonical type mismatch", func(t *Template) { t.Groups[0].Fields[1].Canonical = "diffs.front_cst" }, RuleCanonicalType},
		{"canonical range wider than v1", func(t *Template) { t.Groups[0].Fields[2].Max = f64(2000000) }, RuleCanonicalType},
		{"canonical used twice", func(t *Template) { t.Groups[0].Fields[3].Canonical = "diffs.front_cst" }, RuleCanonicalType},
		{"help too long", func(t *Template) { field(t).Help = &LocalText{PT: strings.Repeat("a", MaxHelpLen+1)} }, RuleHelpLength},
		{"help control char", func(t *Template) { field(t).Help = &LocalText{PT: "a\x00b"} }, RuleHelpLength},
		{"help with markup is plain text", func(t *Template) { field(t).Help = &LocalText{PT: "<script>alert(1)</script>"} }, ""},
		{"unknown diagram", func(t *Template) { field(t).Diagram = &Diagram{Kind: "svg"} }, RuleDiagramUnknown},
		{"hole_row count mismatch", func(t *Template) { field(t).Diagram = &Diagram{Kind: "hole_row", Count: 4} }, RuleDiagramParams},
		{"hole_row on number", func(t *Template) { t.Groups[0].Fields[3].Diagram = &Diagram{Kind: "hole_row", Count: 3} }, RuleDiagramParams},
		{"hole_row foreign param", func(t *Template) { field(t).Diagram = &Diagram{Kind: "hole_row", Count: 3, Axis: "toe"} }, RuleDiagramParams},
		{"hole_grid ok", func(t *Template) {
			field(t).Options = []string{"1", "2", "3", "4"}
			field(t).Kit = "1"
			field(t).Diagram = &Diagram{Kind: "hole_grid", Rows: 2, Cols: 2}
		}, ""},
		{"hole_grid mismatch", func(t *Template) { field(t).Diagram = &Diagram{Kind: "hole_grid", Rows: 2, Cols: 2} }, RuleDiagramParams},
		{"angle bad axis", func(t *Template) { t.Groups[0].Fields[3].Diagram = &Diagram{Kind: "angle", Axis: "roll"} }, RuleDiagramParams},
		{"angle ok", func(t *Template) { t.Groups[0].Fields[3].Diagram = &Diagram{Kind: "angle", Axis: "caster"} }, ""},
		{"length ok", func(t *Template) { t.Groups[0].Fields[2].Diagram = &Diagram{Kind: "length", Measure: "droop"} }, ""},
		{"part ok", func(t *Template) { t.Groups[0].Fields[1].Diagram = &Diagram{Kind: "part", Shape: "disk"} }, ""},
		{"part on choice", func(t *Template) { field(t).Diagram = &Diagram{Kind: "part", Shape: "disk"} }, RuleDiagramParams},
		{"multi ok", func(t *Template) { asMulti(field(t)); field(t).Kit = []any{"1", "3"} }, ""},
		{"multi one option", func(t *Template) { asMulti(field(t)); field(t).Options = []string{"1"} }, RuleTemplateLimits},
		{"multi duplicate option", func(t *Template) { asMulti(field(t)); field(t).Options = []string{"1", "1"} }, RuleKeyDuplicate},
		{"multi kit not an option", func(t *Template) { asMulti(field(t)); field(t).Kit = []any{"9"} }, RuleOptionUnknown},
		{"multi part_ref", func(t *Template) { asMulti(field(t)); field(t).PartRef = "T1" }, RuleTemplateInvalid},
		{"multi diagram", func(t *Template) { asMulti(field(t)); field(t).Diagram = &Diagram{Kind: "hole_row", Count: 3} }, RuleDiagramParams},
		{"multi canonical", func(t *Template) { asMulti(field(t)); field(t).Canonical = "diffs.front_cst" }, RuleCanonicalType},
		{"table ok", func(t *Template) {
			tableOf(t, [][]string{{"shock_pos", "disk"}, {"diff_oil", "caster"}, {"notes", ""}})
		}, ""},
		{"table one column", func(t *Template) {
			tableOf(t, [][]string{{"shock_pos", "disk"}, {"diff_oil", "caster"}, {"notes", ""}})
			t.Groups[0].Table.Columns = t.Groups[0].Table.Columns[:1]
		}, RuleTemplateLimits},
		{"table five columns", func(t *Template) {
			tableOf(t, [][]string{{"shock_pos", "disk"}, {"diff_oil", "caster"}, {"notes", ""}})
			for i := 0; i < 3; i++ {
				t.Groups[0].Table.Columns = append(t.Groups[0].Table.Columns, LocalText{PT: "x"})
			}
		}, RuleTemplateLimits},
		{"table no rows", func(t *Template) { tableOf(t, nil) }, RuleTemplateLimits},
		{"table row width mismatch", func(t *Template) { tableOf(t, [][]string{{"shock_pos"}, {"diff_oil", "caster"}, {"notes", "disk"}}) }, RuleTableLayout},
		{"table unknown key", func(t *Template) {
			tableOf(t, [][]string{{"shock_pos", "disk"}, {"diff_oil", "caster"}, {"notes", "nope"}})
		}, RuleTableLayout},
		{"table key used twice", func(t *Template) {
			tableOf(t, [][]string{{"shock_pos", "disk"}, {"diff_oil", "caster"}, {"notes", "disk"}})
		}, RuleTableLayout},
		{"table leaves a field out", func(t *Template) { tableOf(t, [][]string{{"shock_pos", "disk"}, {"diff_oil", "caster"}}) }, RuleTableLayout},
		{"table key of another group", func(t *Template) {
			tableOf(t, [][]string{{"shock_pos", "disk"}, {"diff_oil", "caster"}, {"notes", "other"}})
			t.Groups = append(t.Groups, TemplateGroup{Key: "back", Label: LocalText{PT: "Traseira"},
				Fields: []TemplateField{{Key: "other", Label: LocalText{PT: "Outro"}, Type: FieldFlag}}})
		}, RuleTableLayout},
		{"table row label empty", func(t *Template) {
			tableOf(t, [][]string{{"shock_pos", "disk"}, {"diff_oil", "caster"}, {"notes", ""}})
			t.Groups[0].Table.Rows[0].Label.PT = ""
		}, RuleTemplateInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tpl := sample()
			tc.mutate(&tpl)
			err := ValidateTemplate(tpl)
			if tc.rule == "" {
				require.NoError(t, err)
				return
			}
			require.Equal(t, tc.rule, ruleOf(t, err))
		})
	}
}

func TestParseTemplateStrictness(t *testing.T) {
	good := marshalIndented(t, sample())
	_, err := ParseTemplate(good)
	require.NoError(t, err)

	tests := []struct {
		name string
		raw  string
		rule string
	}{
		{"unknown attribute", strings.Replace(string(good), `"slug"`, `"extra": 1, "slug"`, 1), RuleTemplateInvalid},
		{"duplicate key", strings.Replace(string(good), `"slug"`, `"slug": "x-y", "slug"`, 1), RuleKeyDuplicate},
		{"duplicate key by case", strings.Replace(string(good), `"slug"`, `"Slug": "x-y", "slug"`, 1), RuleKeyDuplicate},
		{"trailing data", string(good) + `{}`, RuleTemplateInvalid},
		{"not json", `nope`, RuleTemplateInvalid},
		{"too big", strings.Repeat(" ", MaxTemplateBytes+1), RuleTemplateLimits},
		{"too deep", strings.Repeat("[", 100) + strings.Repeat("]", 100), RuleTemplateLimits},
		{"diagram with url", strings.Replace(string(good), `"kind": "hole_row"`, `"kind": "hole_row", "href": "https://x"`, 1), RuleTemplateInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTemplate([]byte(tc.raw))
			require.Equal(t, tc.rule, ruleOf(t, err))
		})
	}
}

func FuzzParseTemplate(f *testing.F) {
	seed, _ := json.Marshal(sample())
	f.Add(seed)
	f.Add([]byte(`{"format":1}`))
	f.Add([]byte(`[]`))
	if raw, err := os.ReadFile(genericFile); err == nil {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		tpl, err := ParseTemplate(raw)
		if err != nil {
			return
		}
		// An accepted template re-validates and survives a round trip.
		require.NoError(t, ValidateTemplate(tpl))
		again, err := json.Marshal(tpl)
		require.NoError(t, err)
		back, err := ParseTemplate(again)
		require.NoError(t, err)
		require.Equal(t, tpl, back)
	})
}

// Every file in the catalogue directory is a valid template named after its content, and no
// two files share an id: this is what scripts/template-migration.py turns into migrations.
func TestCatalogueFilesAreValid(t *testing.T) {
	files, err := filepath.Glob("../templates/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	ids := map[string]string{}
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // fixed repository glob, not user input
		require.NoError(t, err, f)
		tpl, err := ParseTemplate(raw)
		require.NoError(t, err, f)
		require.Equal(t, fmt.Sprintf("%s.v%d.json", tpl.Slug, tpl.Version), filepath.Base(f))
		if other, dup := ids[tpl.ID]; dup {
			require.Equal(t, tpl.Slug, other, "id shared by two slugs")
		}
		ids[tpl.ID] = tpl.Slug
	}
}

func TestDuplicateTemplate(t *testing.T) {
	src := sample()
	id := uuid.MustParse("01a1990b-b4dd-777f-9c5b-e31aaf0135aa")
	dup, err := DuplicateTemplate(src, id)
	require.NoError(t, err)
	require.Equal(t, id.String(), dup.ID)
	require.Equal(t, "mugen-mbx8r-af0135aa", dup.Slug)
	require.Equal(t, 1, dup.Version)
	require.Equal(t, src.Groups, dup.Groups)
	require.Equal(t, "mugen-mbx8r", src.Slug, "the source is untouched")

	src.Slug = strings.Repeat("a", 60)
	dup, err = DuplicateTemplate(src, id)
	require.NoError(t, err)
	require.LessOrEqual(t, len(dup.Slug), 63)

	_, err = DuplicateTemplate(sample(), uuid.Nil)
	require.ErrorIs(t, err, ErrInvalid)
}
