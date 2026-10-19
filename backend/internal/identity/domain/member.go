package domain

import (
	"time"

	"github.com/google/uuid"
)

// Member is a user's membership in a team. The email is deliberately absent.
type Member struct {
	TeamID      uuid.UUID
	UserID      uuid.UUID
	DisplayName string
	Role        Role
	JoinedAt    time.Time
	Version     int32
	InvitedBy   *uuid.UUID
}

// TeamMembership is a team together with the caller's role in it.
type TeamMembership struct {
	Team Team
	Role Role
}
