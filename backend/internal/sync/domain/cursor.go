package domain

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// Cursor is a position in the change log: the writing transaction and the sequence inside
// the log. The Pull orders and pages by (Xid, Seq), because every transaction older than the
// oldest one still running has finished: whatever commits later has a bigger Xid, so a
// device that stored this cursor can never skip a change (ADR 0025).
type Cursor struct {
	Xid uint64
	Seq int64
}

// String is the opaque form sent to the device.
func (c Cursor) String() string {
	return "v1." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", c.Xid, c.Seq)))
}

// ParseCursor reads what String produced. The empty string is the start of the log. Anything
// else that does not parse exactly is an error: a malformed cursor must not restart a Pull.
func ParseCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	enc, ok := strings.CutPrefix(s, "v1.")
	if !ok {
		return Cursor{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return Cursor{}, ErrInvalid
	}
	xs, ss, ok := strings.Cut(string(raw), ":")
	if !ok {
		return Cursor{}, ErrInvalid
	}
	xid, err1 := strconv.ParseUint(xs, 10, 64)
	seq, err2 := strconv.ParseInt(ss, 10, 64)
	if err1 != nil || err2 != nil || seq < 0 {
		return Cursor{}, ErrInvalid
	}
	c := Cursor{Xid: xid, Seq: seq}
	if c.String() != s { // reject non-canonical forms (leading zeros, padding, signs)
		return Cursor{}, ErrInvalid
	}
	return c, nil
}
