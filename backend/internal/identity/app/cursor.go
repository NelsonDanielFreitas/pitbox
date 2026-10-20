package app

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
)

const maxPageToken = 512

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

func encodeCursor(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(token string, v any) error {
	if token == "" {
		return nil
	}
	if len(token) > maxPageToken {
		return domain.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return domain.ErrInvalid
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return domain.ErrInvalid
	}
	return nil
}
