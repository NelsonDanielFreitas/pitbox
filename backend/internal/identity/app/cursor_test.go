package app

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
)

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.Must(uuid.NewV7())
	joined := time.Now().UTC().Truncate(time.Microsecond)

	var team teamsCursor
	if err := decodeCursor(encodeCursor(teamsCursor{ID: id}), &team); err != nil || team.ID != id {
		t.Fatalf("teams cursor: %v, %v", team, err)
	}
	var member membersCursor
	if err := decodeCursor(encodeCursor(membersCursor{JoinedAt: joined, UserID: id}), &member); err != nil ||
		member.UserID != id || !member.JoinedAt.Equal(joined) {
		t.Fatalf("members cursor: %v, %v", member, err)
	}
	var invitation invitationsCursor
	if err := decodeCursor(encodeCursor(invitationsCursor{ID: id}), &invitation); err != nil || invitation.ID != id {
		t.Fatalf("invitations cursor: %v, %v", invitation, err)
	}
}

func TestCursorRejectsGarbage(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"x", "!!!!", "ew", string(make([]byte, cursor.MaxTokenLength+1))} {
		var c teamsCursor
		err := decodeCursor(token, &c)
		if err == nil || !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("token %q: err = %v, want ErrInvalid", token, err)
		}
	}
}

// FuzzDecodeCursor: an arbitrary page token must never panic; it is either a
// cursor or INVALID_ARGUMENT.
func FuzzDecodeCursor(f *testing.F) {
	f.Add("")
	f.Add("e30")
	f.Add("not-base64!")
	f.Fuzz(func(t *testing.T, token string) {
		var c teamsCursor
		err := decodeCursor(token, &c)
		if err != nil && !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
