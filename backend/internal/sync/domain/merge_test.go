package domain

import (
	"encoding/json"
	"testing"
)

func f(s string) map[string]json.RawMessage {
	m, err := ParseFields([]byte(s))
	if err != nil {
		panic(err)
	}
	return m
}

func TestMerge(t *testing.T) {
	t.Run("nothing newer is applied", func(t *testing.T) {
		r := Merge(f(`{"a":1}`), nil)
		if r.Status(nil) != StatusApplied || len(r.Apply) != 1 || len(r.Conflicts) != 0 {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("different fields merge", func(t *testing.T) {
		newer := []Applied{{Version: 2, Fields: f(`{"b":2}`)}}
		r := Merge(f(`{"a":1}`), newer)
		if r.Status(newer) != StatusMerged || len(r.Apply) != 1 {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("same field, other value conflicts and others still apply", func(t *testing.T) {
		newer := []Applied{{Version: 2, Fields: f(`{"a":9}`)}}
		r := Merge(f(`{"a":1,"c":3}`), newer)
		if r.Status(newer) != StatusConflict || len(r.Conflicts) != 1 || r.Conflicts[0].Field != "a" {
			t.Fatalf("%+v", r)
		}
		if _, ok := r.Apply["c"]; !ok || len(r.Apply) != 1 {
			t.Fatalf("non-clashing field must apply: %+v", r.Apply)
		}
	})
	t.Run("same value on both sides is no conflict", func(t *testing.T) {
		newer := []Applied{{Version: 2, Fields: f(`{"a": 1.0}`)}}
		if r := Merge(f(`{"a":1}`), newer); len(r.Conflicts) != 0 {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("latest newer value wins the comparison", func(t *testing.T) {
		newer := []Applied{
			{Version: 3, Fields: f(`{"a":1}`)},
			{Version: 2, Fields: f(`{"a":9}`)},
		}
		if r := Merge(f(`{"a":1}`), newer); len(r.Conflicts) != 0 {
			t.Fatalf("v3 (a=1) is the latest: %+v", r)
		}
	})
}

func TestParseFields(t *testing.T) {
	for _, bad := range []string{``, `null`, `[]`, `"x"`, `1`, `{}x`, `{} {}`, `{"a":`} {
		if _, err := ParseFields([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := ParseFields([]byte(`{"a":1,"b":{"c":[1,2]}}`)); err != nil {
		t.Fatal(err)
	}
}
