package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
)

// Store is the persistence port. Every method runs in the caller's transaction.
type Store interface {
	InsertDevice(ctx context.Context, tx db.DBTX, team, user uuid.UUID, name, platform string, publicKey []byte) (uuid.UUID, error)
	DeviceByPublicKey(ctx context.Context, tx db.DBTX, team, user uuid.UUID, publicKey []byte) (uuid.UUID, bool, error)
	ActiveDevices(ctx context.Context, tx db.DBTX, team, user uuid.UUID) (int64, error)
	Device(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Device, error)
	TouchDevice(ctx context.Context, tx db.DBTX, team, id uuid.UUID) error
	RevokeDevice(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (bool, error)

	InsertPass(ctx context.Context, tx db.DBTX, team, user, device uuid.UUID, role string, ttl time.Duration) (domain.Pass, error)
	Pass(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Pass, error)

	Operation(ctx context.Context, tx db.DBTX, opID uuid.UUID) (domain.StoredOp, bool, error)
	InsertOperation(ctx context.Context, tx db.DBTX, op domain.StoredOp) error
	AppliedSince(ctx context.Context, tx db.DBTX, team uuid.UUID, entity string, entityID uuid.UUID, version int32) ([]domain.Applied, error)

	InsertConflict(ctx context.Context, tx db.DBTX, team uuid.UUID, c domain.Conflict) error
	OpenConflicts(ctx context.Context, tx db.DBTX, team uuid.UUID) ([]domain.Conflict, error)
	Conflict(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Conflict, bool, error)
	ResolveConflict(ctx context.Context, tx db.DBTX, team, id uuid.UUID, resolution string, value []byte, by uuid.UUID) (domain.Conflict, bool, error)

	Changes(ctx context.Context, tx db.DBTX, team uuid.UUID, after domain.Cursor, limit int32) ([]domain.Change, error)
}
