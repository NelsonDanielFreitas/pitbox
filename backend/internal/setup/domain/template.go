package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// FieldType is the kind of value a template field holds.
type FieldType string

const (
	FieldNumber FieldType = "number"
	FieldInt    FieldType = "int"
	FieldChoice FieldType = "choice"
	FieldFlag   FieldType = "flag"
	FieldMulti  FieldType = "multi" // several options at once (parts ticked in a group)
	FieldText   FieldType = "text"
)

var (
	keyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
)

// LocalText is a label or help text in Portuguese (required) and English.
type LocalText struct {
	PT string `json:"pt"`
	EN string `json:"en,omitempty"`
}

// Diagram asks the app to draw one of our own closed set of drawings. It has
// numeric/enum parameters only: never a URL, SVG or image.
type Diagram struct {
	Kind        string `json:"kind"`
	Count       int    `json:"count,omitempty"`
	Rows        int    `json:"rows,omitempty"`
	Cols        int    `json:"cols,omitempty"`
	Orientation string `json:"orientation,omitempty"`
	Axis        string `json:"axis,omitempty"`
	Measure     string `json:"measure,omitempty"`
	Shape       string `json:"shape,omitempty"`
}

// TemplateField is one input of a sheet.
type TemplateField struct {
	Key       string     `json:"key"`
	Label     LocalText  `json:"label"`
	Type      FieldType  `json:"type"`
	Unit      string     `json:"unit,omitempty"`
	Min       *float64   `json:"min,omitempty"`
	Max       *float64   `json:"max,omitempty"`
	Step      *float64   `json:"step,omitempty"`
	Options   []string   `json:"options,omitempty"`
	MaxLen    int        `json:"max_len,omitempty"`
	PartRef   string     `json:"part_ref,omitempty"`
	Kit       any        `json:"kit,omitempty"`
	Canonical string     `json:"canonical,omitempty"`
	Help      *LocalText `json:"help,omitempty"`
	Diagram   *Diagram   `json:"diagram,omitempty"`
}

// TableRow is one row of a group laid out as a table. Cells hold field keys
// (one per column); an empty string is an empty cell.
type TableRow struct {
	Label LocalText `json:"label"`
	Cells []string  `json:"cells"`
}

// GroupTable is a presentation hint: the fields of the group are shown in a
// grid (front/rear, front/centre/rear) as on the paper sheet. It carries keys
// only, never values, so it cannot change what is stored.
type GroupTable struct {
	Columns []LocalText `json:"columns"`
	Rows    []TableRow  `json:"rows"`
}

// TemplateGroup is an ordered section of the sheet. Groups take help text but
// no diagram: a drawing is tied to one field value.
type TemplateGroup struct {
	Key    string          `json:"key"`
	Label  LocalText       `json:"label"`
	Help   *LocalText      `json:"help,omitempty"`
	Table  *GroupTable     `json:"table,omitempty"`
	Fields []TemplateField `json:"fields"`
}

// Template is one immutable version of a sheet template.
type Template struct {
	Format        int             `json:"format"`
	ID            string          `json:"id"`
	Slug          string          `json:"slug"`
	Version       int             `json:"version"`
	Make          string          `json:"make"`
	Model         string          `json:"model"`
	SheetRevision string          `json:"sheet_revision,omitempty"`
	SourceURL     string          `json:"source_url,omitempty"`
	Scale         string          `json:"scale"`
	Powertrain    string          `json:"powertrain"`
	Name          LocalText       `json:"name"`
	Groups        []TemplateGroup `json:"groups"`
}

func (t Template) fieldIndex() map[string]TemplateField {
	idx := map[string]TemplateField{}
	for _, g := range t.Groups {
		for _, f := range g.Fields {
			idx[f.Key] = f
		}
	}
	return idx
}

func tplErr(path, rule string) error { return &ValidationError{Path: path, RuleID: rule} }

// ParseTemplate decodes and validates a template file. The decoding is strict:
// unknown attributes and duplicate JSON keys are refused (encoding/json keeps
// the last duplicate silently, and matches names case-insensitively).
func ParseTemplate(raw []byte) (Template, error) {
	if len(raw) > MaxTemplateBytes {
		return Template{}, tplErr("template", RuleTemplateLimits)
	}
	if err := checkDuplicateKeys(raw); err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return Template{}, ve
		}
		return Template{}, tplErr("template", RuleTemplateInvalid)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var t Template
	if err := dec.Decode(&t); err != nil {
		return Template{}, tplErr("template", RuleTemplateInvalid)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Template{}, tplErr("template", RuleTemplateInvalid)
	}
	if err := ValidateTemplate(t); err != nil {
		return Template{}, err
	}
	return t, nil
}

const maxTemplateJSONDepth = 12

func checkDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	return walkDuplicateKeys(dec, 0)
}

func walkDuplicateKeys(dec *json.Decoder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if depth >= maxTemplateJSONDepth {
		return tplErr("template", RuleTemplateLimits)
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := kt.(string)
			folded := strings.ToLower(key)
			if _, dup := seen[folded]; dup {
				return tplErr("template", RuleKeyDuplicate)
			}
			seen[folded] = struct{}{}
			if err := walkDuplicateKeys(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := walkDuplicateKeys(dec, depth+1); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token() // closing delimiter
	return err
}

// ValidateTemplate checks a template against every structural rule. Errors
// carry a path and a rule id, never a value.
func ValidateTemplate(t Template) error {
	if t.Format != templateFormat {
		return tplErr("format", RuleTemplateInvalid)
	}
	if id, err := uuid.Parse(t.ID); err != nil || id == uuid.Nil || id.String() != t.ID {
		return tplErr("id", RuleTemplateInvalid)
	}
	if !slugPattern.MatchString(t.Slug) {
		return tplErr("slug", RuleTemplateInvalid)
	}
	if t.Version < 1 {
		return tplErr("version", RuleTemplateInvalid)
	}
	for _, c := range []struct {
		path, v string
		max     int
	}{
		{"make", t.Make, maxMakeModelLen},
		{"model", t.Model, maxMakeModelLen},
		{"scale", t.Scale, maxUnitLen},
		{"powertrain", t.Powertrain, maxUnitLen},
	} {
		if !plainText(c.v, 1, c.max) {
			return tplErr(c.path, RuleTemplateInvalid)
		}
	}
	if t.SheetRevision != "" && !plainText(t.SheetRevision, 1, maxSheetRevisionLen) {
		return tplErr("sheet_revision", RuleTemplateInvalid)
	}
	if t.SourceURL != "" && (!strings.HasPrefix(t.SourceURL, "https://") ||
		len(t.SourceURL) > maxSourceURLLen || hasControl(t.SourceURL, false)) {
		return tplErr("source_url", RuleTemplateInvalid)
	}
	if !plainText(t.Name.PT, 1, maxTemplateNameLen) || (t.Name.EN != "" && !plainText(t.Name.EN, 1, maxTemplateNameLen)) {
		return tplErr("name", RuleTemplateInvalid)
	}
	if len(t.Groups) == 0 || len(t.Groups) > MaxTemplateGroups {
		return tplErr("groups", RuleTemplateLimits)
	}

	groupKeys := map[string]struct{}{}
	fieldKeys := map[string]struct{}{}
	canonical := map[string]struct{}{}
	total := 0
	for gi, g := range t.Groups {
		gp := fmt.Sprintf("groups[%d]", gi)
		if !keyPattern.MatchString(g.Key) {
			return tplErr(gp+".key", RuleTemplateInvalid)
		}
		if _, dup := groupKeys[g.Key]; dup {
			return tplErr(gp+".key", RuleKeyDuplicate)
		}
		groupKeys[g.Key] = struct{}{}
		if err := validLabel(gp+".label", g.Label); err != nil {
			return err
		}
		if err := validHelp(gp+".help", g.Help); err != nil {
			return err
		}
		if len(g.Fields) == 0 {
			return tplErr(gp+".fields", RuleTemplateInvalid)
		}
		total += len(g.Fields)
		if total > MaxTemplateFields {
			return tplErr(gp+".fields", RuleTemplateLimits)
		}
		for fi, f := range g.Fields {
			if err := validateField(fmt.Sprintf("%s.fields[%d]", gp, fi), f, fieldKeys, canonical); err != nil {
				return err
			}
		}
		if err := validateTable(gp+".table", g); err != nil {
			return err
		}
	}
	return nil
}

// validateTable checks the table layout of a group: every field of the group
// sits in exactly one cell and no cell names a field of another group.
func validateTable(p string, g TemplateGroup) error {
	tb := g.Table
	if tb == nil {
		return nil
	}
	bad := func() error { return tplErr(p, RuleTableLayout) }
	if len(tb.Columns) < MinTableColumns || len(tb.Columns) > MaxTableColumns ||
		len(tb.Rows) < 1 || len(tb.Rows) > MaxTableRows {
		return tplErr(p, RuleTemplateLimits)
	}
	for _, c := range tb.Columns {
		if err := validLabel(p+".columns", c); err != nil {
			return err
		}
	}
	inGroup := map[string]struct{}{}
	for _, f := range g.Fields {
		inGroup[f.Key] = struct{}{}
	}
	used := map[string]struct{}{}
	for ri, r := range tb.Rows {
		if err := validLabel(fmt.Sprintf("%s.rows[%d].label", p, ri), r.Label); err != nil {
			return err
		}
		if len(r.Cells) != len(tb.Columns) {
			return bad()
		}
		for _, key := range r.Cells {
			if key == "" {
				continue
			}
			if _, ok := inGroup[key]; !ok {
				return bad()
			}
			if _, dup := used[key]; dup {
				return bad()
			}
			used[key] = struct{}{}
		}
	}
	if len(used) != len(inGroup) { // a field left out of the table would never be shown
		return bad()
	}
	return nil
}

func validateField(p string, f TemplateField, keys, canonical map[string]struct{}) error {
	if !keyPattern.MatchString(f.Key) {
		return tplErr(p+".key", RuleTemplateInvalid)
	}
	if _, dup := keys[f.Key]; dup {
		return tplErr(p+".key", RuleKeyDuplicate)
	}
	keys[f.Key] = struct{}{}
	if err := validLabel(p+".label", f.Label); err != nil {
		return err
	}
	if err := validateFieldAttributes(p, f); err != nil {
		return err
	}
	if f.Kit != nil {
		if err := validateFieldValue(p+".kit", f, f.Kit); err != nil {
			return err
		}
	}
	if f.Canonical != "" {
		spec, ok := canonicalSpecs[f.Canonical]
		if !ok {
			return tplErr(p+".canonical", RuleCanonicalUnknown)
		}
		if !canonicalCompatible(f, spec) {
			return tplErr(p+".canonical", RuleCanonicalType)
		}
		if _, dup := canonical[f.Canonical]; dup {
			return tplErr(p+".canonical", RuleKeyDuplicate)
		}
		canonical[f.Canonical] = struct{}{}
	}
	if err := validHelp(p+".help", f.Help); err != nil {
		return err
	}
	return validateDiagram(p+".diagram", f)
}

// validateFieldAttributes checks that each type carries exactly the attributes
// that make sense for it.
func validateFieldAttributes(p string, f TemplateField) error {
	numeric := f.Type == FieldNumber || f.Type == FieldInt
	switch f.Type {
	case FieldNumber, FieldInt, FieldChoice, FieldFlag, FieldMulti, FieldText:
	default:
		return tplErr(p+".type", RuleTemplateInvalid)
	}
	switch {
	case !numeric && (f.Unit != "" || f.Min != nil || f.Max != nil):
		return tplErr(p, RuleTemplateInvalid)
	case f.Type != FieldNumber && f.Step != nil:
		return tplErr(p+".step", RuleTemplateInvalid)
	case f.Type != FieldChoice && f.Type != FieldMulti && f.Options != nil:
		return tplErr(p+".options", RuleTemplateInvalid)
	case f.Type != FieldText && f.MaxLen != 0:
		return tplErr(p+".max_len", RuleTemplateInvalid)
	case f.PartRef != "" && (f.Type == FieldInt || f.Type == FieldText || f.Type == FieldMulti):
		return tplErr(p+".part_ref", RuleTemplateInvalid)
	}
	if f.Unit != "" && !plainText(f.Unit, 1, maxUnitLen) {
		return tplErr(p+".unit", RuleTemplateInvalid)
	}
	if f.PartRef != "" && !plainText(f.PartRef, 1, MaxPartRefLen) {
		return tplErr(p+".part_ref", RuleTemplateInvalid)
	}
	switch f.Type {
	case FieldNumber, FieldInt:
		if f.Min == nil || f.Max == nil || !boundedFloat(*f.Min) || !boundedFloat(*f.Max) || *f.Min > *f.Max {
			return tplErr(p+".min", RuleTemplateInvalid)
		}
		if f.Type == FieldInt && (math.Trunc(*f.Min) != *f.Min || math.Trunc(*f.Max) != *f.Max) {
			return tplErr(p+".min", RuleTemplateInvalid)
		}
		if f.Step != nil && (!boundedFloat(*f.Step) || *f.Step <= 0 || (*f.Max-*f.Min)/(*f.Step) > 1e6) {
			return tplErr(p+".step", RuleTemplateInvalid)
		}
	case FieldChoice, FieldMulti:
		if len(f.Options) < MinChoiceOptions || len(f.Options) > MaxChoiceOptions {
			return tplErr(p+".options", RuleTemplateLimits)
		}
		seen := map[string]struct{}{}
		for _, o := range f.Options {
			if !plainText(o, 1, MaxOptionLen) {
				return tplErr(p+".options", RuleTemplateInvalid)
			}
			if _, dup := seen[o]; dup {
				return tplErr(p+".options", RuleKeyDuplicate)
			}
			seen[o] = struct{}{}
		}
	case FieldText:
		if f.MaxLen < 1 || f.MaxLen > MaxTextFieldLen {
			return tplErr(p+".max_len", RuleTemplateInvalid)
		}
	}
	return nil
}

func validateDiagram(p string, f TemplateField) error {
	d := f.Diagram
	if d == nil {
		return nil
	}
	bad := func() error { return tplErr(p, RuleDiagramParams) }
	// onlyParams: every parameter not listed must be unset.
	extra := func(count, rows, cols, orientation, axis, measure, shape bool) bool {
		return (!count && d.Count != 0) || (!rows && d.Rows != 0) || (!cols && d.Cols != 0) ||
			(!orientation && d.Orientation != "") || (!axis && d.Axis != "") ||
			(!measure && d.Measure != "") || (!shape && d.Shape != "")
	}
	switch d.Kind {
	case "hole_row":
		if f.Type != FieldChoice || extra(true, false, false, true, false, false, false) ||
			d.Count < 2 || d.Count > 8 || d.Count != len(f.Options) ||
			!oneOf(d.Orientation, "", "horizontal", "vertical") {
			return bad()
		}
	case "hole_grid":
		if f.Type != FieldChoice || extra(false, true, true, false, false, false, false) ||
			d.Rows < 1 || d.Rows > 4 || d.Cols < 2 || d.Cols > 6 || d.Rows*d.Cols != len(f.Options) {
			return bad()
		}
	case "angle":
		if f.Type != FieldNumber || extra(false, false, false, false, true, false, false) ||
			!oneOf(d.Axis, "camber", "caster", "toe", "kickup", "anti_squat") {
			return bad()
		}
	case "length":
		if (f.Type != FieldNumber && f.Type != FieldInt) || extra(false, false, false, false, false, true, false) ||
			!oneOf(d.Measure, "ride_height", "droop", "shock_length", "limiter") {
			return bad()
		}
	case "part":
		if f.Type != FieldFlag || extra(false, false, false, false, false, false, true) ||
			!oneOf(d.Shape, "disk", "brace", "spacer", "weight", "other") {
			return bad()
		}
	default:
		return tplErr(p+".kind", RuleDiagramUnknown)
	}
	return nil
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func validLabel(p string, l LocalText) error {
	if !plainText(l.PT, 1, MaxLabelLen) || (l.EN != "" && !plainText(l.EN, 1, MaxLabelLen)) {
		return tplErr(p, RuleTemplateInvalid)
	}
	return nil
}

func validHelp(p string, h *LocalText) error {
	if h == nil {
		return nil
	}
	if strings.TrimSpace(h.PT) == "" || runeLen(h.PT) > MaxHelpLen || runeLen(h.EN) > MaxHelpLen ||
		hasControl(h.PT, true) || hasControl(h.EN, true) || !utf8.ValidString(h.PT) || !utf8.ValidString(h.EN) {
		return tplErr(p, RuleHelpLength)
	}
	return nil
}

// plainText accepts valid UTF-8 with no control characters and a rune count in
// [min, max], and not only whitespace.
func plainText(s string, min, max int) bool {
	n := runeLen(s)
	return utf8.ValidString(s) && n >= min && n <= max && strings.TrimSpace(s) != "" && !hasControl(s, false)
}

// hasControl reports control characters; newline and tab are allowed when
// allowBreaks is set (help and notes may wrap).
func hasControl(s string, allowBreaks bool) bool {
	for _, r := range s {
		if allowBreaks && (r == '\n' || r == '\t') {
			continue
		}
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func boundedFloat(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && math.Abs(f) <= maxIntegerMagnitude
}
