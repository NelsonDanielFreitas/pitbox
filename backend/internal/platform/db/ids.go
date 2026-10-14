package db

import (
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidScope means a user or team id is not a canonical, non-nil UUID. It is returned
// before anything is sent to the database, and never repeats the value.
var ErrInvalidScope = errors.New("db: invalid user or team id")

// UserID and TeamID are different types on purpose: both are UUIDs, and a call that swaps them
// (WithTeamTx(ctx, team, user, ...)) is a compile error, not a leak between tenants.
type (
	UserID uuid.UUID
	TeamID uuid.UUID
)

// ParseUserID accepts only the canonical form: 36 characters with hyphens, not nil. The
// library's parser would also take braces, a "urn:uuid:" prefix and 32 bare hex digits.
func ParseUserID(s string) (UserID, error) {
	u, err := parseCanonical(s)
	return UserID(u), err
}

// ParseTeamID is ParseUserID for a team.
func ParseTeamID(s string) (TeamID, error) {
	u, err := parseCanonical(s)
	return TeamID(u), err
}

func parseCanonical(s string) (uuid.UUID, error) {
	if len(s) != 36 {
		return uuid.Nil, ErrInvalidScope
	}
	u, err := uuid.Parse(s)
	if err != nil || u == uuid.Nil {
		return uuid.Nil, ErrInvalidScope
	}
	return u, nil
}

func (u UserID) String() string { return uuid.UUID(u).String() }
func (t TeamID) String() string { return uuid.UUID(t).String() }

// IsZero reports whether the id is the nil UUID (the zero value of the type).
func (u UserID) IsZero() bool { return uuid.UUID(u) == uuid.Nil }

// IsZero reports whether the id is the nil UUID (the zero value of the type).
func (t TeamID) IsZero() bool { return uuid.UUID(t) == uuid.Nil }
