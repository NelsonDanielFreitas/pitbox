package cursor_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
)

type token struct {
	Value string `json:"value"`
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	got := cursor.Encode(token{Value: "abc"})
	var decoded token
	if err := cursor.Decode(got, &decoded); err != nil || decoded.Value != "abc" {
		t.Fatalf("Decode: %+v, %v", decoded, err)
	}
}

func TestRejectsInvalid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"!!!!",
		"ew",
		strings.Repeat("a", cursor.MaxTokenLength+1),
	} {
		var decoded token
		err := cursor.Decode(raw, &decoded)
		if !errors.Is(err, apperr.ErrInvalidArgument) {
			t.Errorf("Decode(%q) = %v, want ErrInvalidArgument", raw, err)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add("")
	f.Add("e30")
	f.Add("not-base64!")
	f.Fuzz(func(t *testing.T, raw string) {
		var decoded token
		err := cursor.Decode(raw, &decoded)
		if err != nil && !errors.Is(err, apperr.ErrInvalidArgument) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
