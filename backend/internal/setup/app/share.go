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

func shareTTL(expiry domain.ShareExpiry) *string {
	var ttl string
	switch expiry {
	case domain.ShareExpiry24h:
		ttl = "24 hours"
	case domain.ShareExpiry7d:
		ttl = "7 days"
	case domain.ShareExpiryNever:
		return nil
	default:
		return nil
	}
	return &ttl
}
