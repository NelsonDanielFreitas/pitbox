package domain

import (
	"time"

	"github.com/google/uuid"
)

// TransferTTL bounds a pending ownership transfer.
const TransferTTL = 24 * time.Hour

// OwnershipTransfer is a pending two-sided ownership transfer.
type OwnershipTransfer struct {
	ID          uuid.UUID
	TeamID      uuid.UUID
	FromUserID  uuid.UUID
	ToUserID    uuid.UUID
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ConfirmedAt *time.Time
	CancelledAt *time.Time
}

// Pending reports whether the transfer can still be accepted at now.
func (t OwnershipTransfer) Pending(now time.Time) bool {
	return t.ConfirmedAt == nil && t.CancelledAt == nil && now.Before(t.ExpiresAt)
}

// TransferExpiry returns the expiry of a new transfer.
func TransferExpiry(now time.Time) time.Time { return now.Add(TransferTTL) }
