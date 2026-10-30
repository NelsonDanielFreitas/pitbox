package domain

import (
	"time"

	"github.com/google/uuid"
)

// Device is a registered device key.
type Device struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	PublicKey []byte
	Revoked   bool
}

// Pass is the stored offline pass; the row, not the token, decides whether it is valid.
type Pass struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	DeviceID  uuid.UUID
	Role      string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// StoredOp is a recorded operation: the idempotency record.
type StoredOp struct {
	OpID          uuid.UUID
	TeamID        uuid.UUID
	UserID        uuid.UUID
	DeviceID      uuid.UUID
	Entity        string
	EntityID      uuid.UUID
	Kind          Kind
	BaseVersion   *int32
	Fields        []byte
	ClientTS      time.Time
	Signature     []byte
	Status        Status
	Reason        *string
	ResultVersion *int32
}

// Conflict is a field the device and the server changed differently.
type Conflict struct {
	ID          uuid.UUID
	OpID        uuid.UUID
	Entity      string
	EntityID    uuid.UUID
	Field       string
	ServerValue []byte
	ClientValue []byte
	CreatedAt   time.Time
	Resolved    bool
}

// Change is one row of the change log.
type Change struct {
	Cursor   Cursor
	Entity   string
	EntityID uuid.UUID
	Deleted  bool
}
