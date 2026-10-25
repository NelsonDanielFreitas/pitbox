package app

import (
	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

type CreateShareLinkInput struct {
	RequestID     uuid.UUID
	SetupID       uuid.UUID
	Expiry        domain.ShareExpiry
	PinnedVersion *int32
}
