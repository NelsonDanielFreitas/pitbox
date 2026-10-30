// Package syncapply is the contract between the sync module and the modules that own the
// data it writes. Each module implements Applier for its entities in its api.go; sync
// calls them with the transaction it opened (team context and RLS already set) and never
// imports a module's domain, app or store (architecture rule 2).
package syncapply

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Kind is what the operation does.
type Kind int

const (
	Create Kind = iota + 1
	Patch
	Append
	Delete
)

// Errors an Applier returns. They map to a stable rejection code in the push result.
var (
	ErrInvalidFields  = errors.New("syncapply: invalid fields")
	ErrInvalidRef     = errors.New("syncapply: invalid reference")
	ErrCatalogRetired = errors.New("syncapply: retired catalogue value")
	ErrNotFound       = errors.New("syncapply: entity not found")
	ErrStale          = errors.New("syncapply: base version is stale")
	ErrForbidden      = errors.New("syncapply: not allowed for this role")
)

// Op is one operation to apply. Fields has already been parsed and size-checked; the
// Applier still validates every value (the device is not trusted).
type Op struct {
	OpID     uuid.UUID
	TeamID   db.TeamID
	UserID   db.UserID
	Role     string
	Kind     Kind
	EntityID uuid.UUID
	Fields   map[string]json.RawMessage
	// ExpectedVersion is the version the caller read for Patch/Delete. The Applier updates
	// WHERE version = ExpectedVersion and returns ErrStale when another writer got there first.
	ExpectedVersion int32
}

// Applier applies operations on one entity (for example "garage.car").
type Applier interface {
	// Entity is the allow-listed name, "schema.table".
	Entity() string
	// CurrentVersion is the entity's version now, for Patch/Delete. Create and Append
	// entities have no previous version and return 0.
	CurrentVersion(ctx context.Context, tx db.DBTX, teamID db.TeamID, entityID uuid.UUID) (int32, error)
	// Apply writes the operation and returns the entity's version afterwards. It must be
	// atomic with the caller's transaction and must not open its own.
	Apply(ctx context.Context, tx db.DBTX, op Op) (int32, error)
	// Read returns the entity's current row as a JSON object, already filtered for the
	// caller's role (a pilot never sees purchase prices, ADR 0023). found is false when it
	// is gone or soft-deleted: the Pull then sends a tombstone.
	Read(ctx context.Context, tx db.DBTX, teamID db.TeamID, role string, entityID uuid.UUID) (data json.RawMessage, found bool, err error)
}

// RoleGated is implemented by an Applier whose entity may be written by roles other than
// owner and mechanic (the driver's feedback is written by the pilot). Without it only owner
// and mechanic write. The sync module checks the caller's CURRENT role on every operation,
// and an operation of a role that may not write the entity is rejected as "forbidden".
type RoleGated interface {
	CanWrite(role string) bool
}

// Registry maps entity names to their Applier. An entity that is not registered is not
// syncable: this is the allow-list.
type Registry map[string]Applier

func NewRegistry(appliers ...Applier) Registry {
	r := Registry{}
	for _, a := range appliers {
		r[a.Entity()] = a
	}
	return r
}
