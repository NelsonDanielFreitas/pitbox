package syncapply

import (
	"encoding/json"
	"errors"
	"testing"
)

func f(s string) Fields {
	var m Fields
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}

func TestFieldsStrict(t *testing.T) {
	m := f(`{"a":"x","n":null,"i":7,"big":4294967296,"fl":1.5,"s":"7","o":{"k":1},"arr":[1]}`)

	if set, v, err := m.String("a"); !set || v == nil || *v != "x" || err != nil {
		t.Fatalf("string: %v %v %v", set, v, err)
	}
	if set, v, err := m.String("n"); !set || v != nil || err != nil {
		t.Fatalf("null must be set and nil: %v %v %v", set, v, err)
	}
	if set, _, err := m.String("missing"); set || err != nil {
		t.Fatalf("absent must not be set: %v %v", set, err)
	}
	if _, _, err := m.String("i"); !errors.Is(err, ErrInvalidFields) {
		t.Fatalf("number read as string: %v", err)
	}
	if _, v, err := m.Int32("i"); err != nil || *v != 7 {
		t.Fatalf("int: %v %v", v, err)
	}
	for _, k := range []string{"big", "fl", "s", "o", "arr"} {
		if _, _, err := m.Int32(k); !errors.Is(err, ErrInvalidFields) {
			t.Errorf("%s accepted as int32", k)
		}
	}
	if _, err := m.RequiredString("n"); !errors.Is(err, ErrInvalidFields) {
		t.Error("null accepted as required")
	}
	if _, err := m.Object("o"); err != nil {
		t.Error(err)
	}
	for _, k := range []string{"a", "n", "arr", "missing"} {
		if _, err := m.Object(k); !errors.Is(err, ErrInvalidFields) {
			t.Errorf("%s accepted as object", k)
		}
	}
	if err := m.Only("a", "n", "i", "big", "fl", "s", "o", "arr"); err != nil {
		t.Error(err)
	}
	if err := m.Only("a"); !errors.Is(err, ErrInvalidFields) {
		t.Error("unknown keys accepted")
	}
}

func FuzzFields(f *testing.F) {
	f.Add([]byte(`{"a":1}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var m Fields
		if json.Unmarshal(b, &m) != nil {
			return
		}
		_, _, _ = m.String("a")
		_, _, _ = m.Int32("a")
		_, _ = m.Object("a")
		_, _ = m.RequiredString("a")
	}) // must not panic
}

func TestFieldsInt64BoolObject(t *testing.T) {
	t.Parallel()
	f := Fields{
		"big": json.RawMessage(`9999999999`), "float": json.RawMessage(`1.5`), "str": json.RawMessage(`"7"`),
		"yes": json.RawMessage(`true`), "one": json.RawMessage(`1`), "nil": json.RawMessage(`null`),
		"obj": json.RawMessage(`{"a":1}`),
	}
	set, v, err := f.Int64("big")
	if !set || err != nil || v == nil || *v != 9999999999 {
		t.Fatalf("int64: %v %v %v", set, v, err)
	}
	if _, _, err := f.Int32("big"); err == nil {
		t.Fatal("9999999999 does not fit int32")
	}
	for _, k := range []string{"float", "str"} {
		if _, _, err := f.Int64(k); err == nil {
			t.Fatalf("%s must not be an integer", k)
		}
	}
	if set, v, err := f.Bool("yes"); !set || err != nil || v == nil || !*v {
		t.Fatalf("bool: %v %v %v", set, v, err)
	}
	if _, _, err := f.Bool("one"); err == nil {
		t.Fatal("1 is not a bool")
	}
	if set, v, err := f.Bool("nil"); !set || err != nil || v != nil {
		t.Fatalf("null bool: %v %v %v", set, v, err)
	}
	if set, m, err := f.OptionalObject("obj"); !set || err != nil || len(m) != 1 {
		t.Fatalf("object: %v %v %v", set, m, err)
	}
	if _, _, err := f.OptionalObject("nil"); err == nil {
		t.Fatal("null is not an object")
	}
	if set, _, err := f.OptionalObject("missing"); set || err != nil {
		t.Fatal("absent object is not set")
	}
}
