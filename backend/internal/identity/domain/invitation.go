package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// InvitationTTL is the maximum lifetime of an invitation (the database CHECK
	// enforces the same limit).
	InvitationTTL = 7 * 24 * time.Hour
	// MaxPendingInvitations is the maximum number of pending invitations per team.
	MaxPendingInvitations = 50
	// tokenBytes is 256 bits of entropy.
	tokenBytes = 32
	// TokenLength is the length of the base64url token without padding.
	TokenLength = 43
	maxEmail    = 320
)

// Invitation is a pending invitation. The plaintext token is never part of it.
type Invitation struct {
	ID         uuid.UUID
	TeamID     uuid.UUID
	Email      *string
	Role       Role
	TokenHash  []byte
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	AcceptedBy *uuid.UUID
	RevokedAt  *time.Time
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
}

// Pending reports whether the invitation can still be accepted at now.
func (i Invitation) Pending(now time.Time) bool {
	return i.AcceptedAt == nil && i.RevokedAt == nil && now.Before(i.ExpiresAt)
}

// NewInvitationToken returns the plaintext token and its SHA-256 hash. The token
// is 32 random bytes in base64url without padding; only the hash is stored.
func NewInvitationToken() (string, []byte, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate invitation token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), hashToken(raw), nil
}

// HashInvitationToken validates the wire token and returns its SHA-256 hash. An
// invalid token is the same ErrInvalidInvitation as an expired one.
func HashInvitationToken(token string) ([]byte, error) {
	if len(token) != TokenLength {
		return nil, ErrInvalidInvitation
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return nil, ErrInvalidInvitation
	}
	return hashToken(raw), nil
}

func hashToken(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}

// InvitationEmail normalises an email used to bind an invitation to an account.
// The contract has already checked the format; this trims and lower-cases it.
func InvitationEmail(email *string) (*string, error) {
	if email == nil {
		return nil, nil
	}
	value := strings.ToLower(strings.TrimSpace(*email))
	if value == "" || utf8.RuneCountInString(value) > maxEmail || !strings.Contains(value, "@") {
		return nil, fmt.Errorf("%w: invitation email", ErrInvalid)
	}
	return &value, nil
}

// InvitationExpiry returns the expiry of a new invitation.
func InvitationExpiry(now time.Time) time.Time { return now.Add(InvitationTTL) }

// InvitationAcceptance is the result of accepting an invitation. Consumed is
// false when the user was already a member or the invitation was already
// accepted by them: nothing changed.
type InvitationAcceptance struct {
	TeamID   uuid.UUID
	Role     Role
	Consumed bool
}
