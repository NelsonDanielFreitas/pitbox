package domain

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
)

// ParseFields decodes the signed fields as a JSON object. Numbers stay as raw text
// (json.RawMessage) so nothing is rounded through float64 on the way.
func ParseFields(b []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, ErrBadFields
	}
	if dec.More() { // trailing data after the object
		return nil, ErrBadFields
	}
	if _, err := dec.Token(); err == nil {
		return nil, ErrBadFields
	}
	return m, nil
}

// Applied is an operation already applied to the entity after the client's base version.
type Applied struct {
	Version int32
	Fields  map[string]json.RawMessage
}

// FieldConflict is a field the device and the server both changed to different values.
type FieldConflict struct {
	Field  string
	Server json.RawMessage
	Client json.RawMessage
}

// MergeResult is what to apply now and what the mechanic must decide.
type MergeResult struct {
	Apply     map[string]json.RawMessage
	Conflicts []FieldConflict
}

// Status reports the outcome of a merge: applied when nothing changed since the base,
// merged when other fields changed but none of ours, conflict when a field clashed.
func (r MergeResult) Status(newer []Applied) Status {
	switch {
	case len(r.Conflicts) > 0:
		return StatusConflict
	case len(newer) > 0:
		return StatusMerged
	default:
		return StatusApplied
	}
}

// Merge does a field-level three-way merge of a patch. newer lists the operations applied
// after the patch's base version. A field is a conflict only when a newer operation set it
// to a different value than the patch; the same value on both sides is not a clash. Fields
// that do not clash are applied, so one conflicting field never blocks the others.
func Merge(patch map[string]json.RawMessage, newer []Applied) MergeResult {
	// Latest value per field among the newer operations.
	latest := map[string]json.RawMessage{}
	ordered := append([]Applied(nil), newer...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version < ordered[j].Version })
	for _, a := range ordered {
		for k, v := range a.Fields {
			latest[k] = v
		}
	}

	res := MergeResult{Apply: map[string]json.RawMessage{}}
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic conflict order
	for _, k := range keys {
		client := patch[k]
		server, changed := latest[k]
		if changed && !jsonEqual(server, client) {
			res.Conflicts = append(res.Conflicts, FieldConflict{Field: k, Server: server, Client: client})
			continue
		}
		res.Apply[k] = client
	}
	return res
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
