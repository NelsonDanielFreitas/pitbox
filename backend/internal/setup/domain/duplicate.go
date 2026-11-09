package domain

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// DuplicateTemplate copies a template for a team under a new id. Everything that makes the
// copy self-describing changes with the id: the slug (unique per team) is derived from the
// source slug and the LAST 8 hex digits of the new id (the random part of a UUIDv7; the first
// digits are a timestamp), and the version restarts at 1. The phone computes the same slug
// offline, so a duplicate made without network equals the one the server stores.
func DuplicateTemplate(src Template, newID uuid.UUID) (Template, error) {
	if newID == uuid.Nil {
		return Template{}, fmt.Errorf("%w: template id", ErrInvalid)
	}
	base := src.Slug
	if len(base) > 54 {
		base = strings.TrimRight(base[:54], "-")
	}
	out := src
	out.ID = newID.String()
	out.Slug = base + "-" + strings.ReplaceAll(out.ID, "-", "")[24:]
	out.Version = 1
	// Groups are shared with src: the copy is read-only from here, so no deep copy is needed.
	if err := ValidateTemplate(out); err != nil {
		return Template{}, err
	}
	return out, nil
}
