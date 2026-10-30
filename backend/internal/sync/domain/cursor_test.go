package domain

import "testing"

func TestCursorRoundTrip(t *testing.T) {
	for _, c := range []Cursor{{}, {1, 1}, {18446744073709551615, 9223372036854775807}} {
		got, err := ParseCursor(c.String())
		if err != nil || got != c {
			t.Fatalf("%+v -> %+v, %v", c, got, err)
		}
	}
	if c, err := ParseCursor(""); err != nil || c != (Cursor{}) {
		t.Fatalf("empty: %+v %v", c, err)
	}
	for _, bad := range []string{"x", "v2.AAAA", "v1.", "v1.!!!", "v1.MDE6Mg", "v1.MQ", "v1.LTE6MQ", "v1.MToteg"} {
		if _, err := ParseCursor(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func FuzzParseCursor(f *testing.F) {
	f.Add("v1.MToy")
	f.Fuzz(func(t *testing.T, s string) {
		c, err := ParseCursor(s)
		if err == nil && s != "" && c.String() != s {
			// Only canonical encodings may be accepted.
			t.Fatalf("%q parsed as %+v but re-encodes as %q", s, c, c.String())
		}
	})
}
