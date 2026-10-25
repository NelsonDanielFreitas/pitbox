package app

import (
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
)

type teamsCursor struct {
	ID uuid.UUID `json:"id"`
}

type membersCursor struct {
	JoinedAt time.Time `json:"joined_at"`
	UserID   uuid.UUID `json:"user_id"`
}

type invitationsCursor struct {
	ID uuid.UUID `json:"id"`
}

func encodeCursor(v any) string { return cursor.Encode(v) }

func decodeCursor(token string, v any) error {
	if err := cursor.Decode(token, v); err != nil {
		return domain.ErrInvalid
	}
	return nil
}
