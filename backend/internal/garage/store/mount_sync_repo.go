package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store/sqlcgen"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Synced variants (F1-13): the device chose the id and the times; the caller validated them.

// CreateMountWithID: an overlap with another mount of the engine or the car is the
// exclusion constraint (23P01) and comes back as domain.ErrMountConflict.
func (Repo) CreateMountWithID(ctx context.Context, tx db.DBTX, team, user, id uuid.UUID, m domain.EngineMount) (domain.EngineMount, error) {
	row, err := sqlcgen.New(tx).CreateMountWithID(ctx, sqlcgen.CreateMountWithIDParams{
		ID: id, TeamID: team, EngineID: m.EngineID, CarID: m.CarID, MountedAt: m.MountedAt, CreatedBy: &user,
	})
	if err != nil {
		return domain.EngineMount{}, mapError("create engine mount", err)
	}
	return mountFromCreate(sqlcgen.CreateMountRow(row)), nil
}

// CloseMountAt reports whether this call closed the mount (false: it was already closed).
func (Repo) CloseMountAt(ctx context.Context, tx db.DBTX, team, id uuid.UUID, at time.Time) (bool, error) {
	n, err := sqlcgen.New(tx).CloseMountAt(ctx, sqlcgen.CloseMountAtParams{TeamID: team, ID: id, UnmountedAt: &at})
	if err != nil {
		return false, mapError("close engine mount", err)
	}
	return n > 0, nil
}

func (Repo) GetMount(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.EngineMount, error) {
	row, err := sqlcgen.New(tx).GetMountByID(ctx, sqlcgen.GetMountByIDParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EngineMount{}, fmt.Errorf("get engine mount: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.EngineMount{}, fmt.Errorf("get engine mount: %w", err)
	}
	return mountFromCreate(sqlcgen.CreateMountRow(row)), nil
}

func (Repo) CreatePartInstallWithID(ctx context.Context, tx db.DBTX, team, user, id uuid.UUID, in domain.NewInstall, at time.Time) (domain.PartInstall, error) {
	row, err := sqlcgen.New(tx).CreatePartInstallWithID(ctx, sqlcgen.CreatePartInstallWithIDParams{
		ID: id, TeamID: team, PartID: in.PartID, CarID: in.CarID, EngineID: in.EngineID, InstalledAt: at, CreatedBy: &user,
	})
	if err != nil {
		return domain.PartInstall{}, mapError("create part install", err)
	}
	return installFromRow(sqlcgen.CreatePartInstallRow(row)), nil
}

func (Repo) ClosePartInstallAt(ctx context.Context, tx db.DBTX, team, id uuid.UUID, removal domain.Removal, at time.Time) (domain.PartInstall, error) {
	var reason *string
	if removal.Reason != "" {
		reason = &removal.Reason
	}
	row, err := sqlcgen.New(tx).ClosePartInstallAt(ctx, sqlcgen.ClosePartInstallAtParams{
		TeamID: team, ID: id, RemovalDestination: &removal.Destination, RemovedAt: &at, RemovalReason: reason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PartInstall{}, domain.ErrInstallConflict
	}
	if err != nil {
		return domain.PartInstall{}, mapError("close part install", err)
	}
	return installFromRow(sqlcgen.CreatePartInstallRow(row)), nil
}

func (Repo) GetPartInstall(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.PartInstall, error) {
	row, err := sqlcgen.New(tx).GetPartInstall(ctx, sqlcgen.GetPartInstallParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PartInstall{}, fmt.Errorf("get part install: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.PartInstall{}, fmt.Errorf("get part install: %w", err)
	}
	return installFromRow(sqlcgen.CreatePartInstallRow(row)), nil
}
