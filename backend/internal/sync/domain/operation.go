// Package domain holds the pure rules of synchronisation: what an operation is, how a
// device signs it and how concurrent edits are merged. It imports nothing from the
// infrastructure (no pgx, proto or Connect).
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Kind is what an operation does to its entity.
type Kind int

const (
	KindCreate Kind = iota + 1
	KindPatch
	KindAppend
	KindDelete
)

// Status is the outcome of one operation. Rejected never fails the RPC: the device
// learns the reason and the owner can review it.
type Status string

const (
	StatusApplied  Status = "applied"
	StatusMerged   Status = "merged"
	StatusConflict Status = "conflict"
	StatusRejected Status = "rejected"
)

// Rejection reasons are stable codes: they go to the client, so they never carry
// internal detail.
const (
	ReasonBadSignature   = "bad_signature"
	ReasonDeviceRevoked  = "device_revoked"
	ReasonPassInvalid    = "pass_invalid"
	ReasonForbidden      = "forbidden"
	ReasonUnknownEntity  = "unknown_entity"
	ReasonInvalidFields  = "invalid_fields"
	ReasonInvalidRef     = "invalid_reference"
	ReasonCatalogRetired = "catalog_retired"
	ReasonOpReplayed     = "op_id_reused"
	ReasonBadTimestamp   = "bad_timestamp"
	ReasonNotFound       = "not_found"
)

var (
	ErrBadSignature = errors.New("sync: bad device signature")
	ErrBadKey       = errors.New("sync: invalid device public key")
	ErrBadFields    = errors.New("sync: fields is not a JSON object")
)

// Operation is one queued write from a device.
type Operation struct {
	OpID        uuid.UUID
	Entity      string
	EntityID    uuid.UUID
	Kind        Kind
	BaseVersion *int32
	FieldsJSON  []byte // exactly the bytes the device signed
	ClientTS    time.Time
	Signature   []byte // ECDSA P-256, IEEE P1363 (64 bytes)
}
