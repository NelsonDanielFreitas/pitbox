// Package cursor encodes and decodes the opaque pagination cursors used by list RPCs.
package cursor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

// MaxTokenLength bounds the wire cursor before it is decoded.
const MaxTokenLength = 512

// Encode returns a URL-safe, opaque cursor. It returns "" only if v cannot be marshalled,
// which is a programming error in the caller.
func Encode(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Decode parses a cursor into v. An empty token is valid and leaves v untouched; any
// malformed, oversized or non-canonical token is INVALID_ARGUMENT.
func Decode(token string, v any) error {
	if token == "" {
		return nil
	}
	if len(token) > MaxTokenLength {
		return fmt.Errorf("page token: %w", apperr.ErrInvalidArgument)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || json.Unmarshal(raw, v) != nil {
		return fmt.Errorf("page token: %w", apperr.ErrInvalidArgument)
	}
	return nil
}
