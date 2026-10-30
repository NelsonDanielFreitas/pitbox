package garage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
)

// Appliers for engine mounts and part installs (F1-13, ADR 0025). Both are histories with a
// start and an end: the device creates the row open and later closes it. The times are the
// device's (a mechanic who was offline mounted the engine at the track, not when the phone
// got a signal), kept inside the same window as stock movements.
//
// An installed or returned unit is a stock movement, and the app sends that movement as its
// own operation (garage.stock_movement). Installing here only writes the installation row;
// otherwise the unit would leave the stock twice.

// deviceTime parses an optional RFC 3339 time from a device. A time from the future or from
// long ago is replaced by now.
func deviceTime(raw *string, now time.Time) (time.Time, error) {
	if raw == nil {
		return now, nil
	}
	t, err := time.Parse(time.RFC3339Nano, *raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("time: %w", syncapply.ErrInvalidFields)
	}
	if t.Before(now.Add(movementMaxFuture)) && t.After(now.Add(-movementMaxAge)) {
		return t.UTC(), nil
	}
	return now, nil
}

// --------------------------------------------------------------------------- engine mounts

type EngineMountApplier struct {
	repo *store.Repo
	now  func() time.Time
}

func NewEngineMountApplier() *EngineMountApplier {
	return &EngineMountApplier{repo: store.NewRepo(), now: time.Now}
}

var _ syncapply.Applier = (*EngineMountApplier)(nil)

func (*EngineMountApplier) Entity() string { return "garage.engine_mount" }

// CurrentVersion is always 1: a mount has no version column and closing it is a state change
// that is safe to repeat (closing a closed mount succeeds), so no base version can be stale.
func (a *EngineMountApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	if _, err := a.repo.GetMount(ctx, tx, uuid.UUID(team), id); err != nil {
		return 0, carSyncErr(err)
	}
	return 1, nil
}

func (a *EngineMountApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	f := syncapply.Fields(op.Fields)
	team, user := uuid.UUID(op.TeamID), uuid.UUID(op.UserID)
	now := a.now()
	switch op.Kind {
	case syncapply.Create:
		if err := f.Only("engine_id", "car_id", "mounted_at"); err != nil {
			return 0, err
		}
		engineID, err1 := requiredUUID(f, "engine_id")
		carID, err2 := requiredUUID(f, "car_id")
		_, mountedRaw, err3 := f.String("mounted_at")
		if err := errors.Join(err1, err2, err3); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		at, err := deviceTime(mountedRaw, now)
		if err != nil {
			return 0, err
		}
		m, err := domain.NewEngineMount(engineID, carID, at)
		if err != nil {
			return 0, carSyncErr(err)
		}
		// Fixed lock order (engine, then car), like MountEngine.
		if err := a.repo.LockEngine(ctx, tx, team, engineID); err != nil {
			return 0, carSyncErr(refOf(err))
		}
		if err := a.repo.LockCar(ctx, tx, team, carID); err != nil {
			return 0, carSyncErr(refOf(err))
		}
		e, err := a.repo.GetEngine(ctx, tx, team, engineID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		c, err := a.repo.GetCar(ctx, tx, team, carID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if e.Status == domain.AssetRetired || c.Status == domain.AssetRetired {
			return 0, fmt.Errorf("retired engine or car: %w", syncapply.ErrInvalidRef)
		}
		if _, open, err := a.repo.OpenMountForEngine(ctx, tx, team, engineID); err != nil {
			return 0, err
		} else if open {
			return 0, fmt.Errorf("engine already mounted: %w", syncapply.ErrInvalidRef)
		}
		if _, open, err := a.repo.OpenMountForCar(ctx, tx, team, carID); err != nil {
			return 0, err
		} else if open {
			return 0, fmt.Errorf("car already has an engine: %w", syncapply.ErrInvalidRef)
		}
		if _, err := a.repo.CreateMountWithID(ctx, tx, team, user, op.EntityID, m); err != nil {
			return 0, carSyncErr(err)
		}
		return 1, nil
	case syncapply.Patch:
		if err := f.Only("unmounted_at"); err != nil {
			return 0, err
		}
		set, raw, err := f.String("unmounted_at")
		if err != nil || !set {
			return 0, syncapply.ErrInvalidFields
		}
		m, err := a.repo.GetMount(ctx, tx, team, op.EntityID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if !m.Open() {
			return 1, nil // closed by another device: the goal is reached
		}
		at, err := deviceTime(raw, now)
		if err != nil {
			return 0, err
		}
		if !at.After(m.MountedAt) {
			at = now
		}
		if !at.After(m.MountedAt) {
			return 0, fmt.Errorf("unmounted_at: %w", syncapply.ErrInvalidFields)
		}
		if err := a.repo.LockEngine(ctx, tx, team, m.EngineID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return 0, err
		}
		if _, err := a.repo.CloseMountAt(ctx, tx, team, op.EntityID, at); err != nil {
			return 0, carSyncErr(err)
		}
		return 1, nil
	default:
		return 0, fmt.Errorf("engine mount operation kind: %w", syncapply.ErrInvalidFields)
	}
}

// refOf turns "not found" of a referenced row into an invalid reference.
func refOf(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrMountConflict
	}
	return err
}

func requiredUUID(f syncapply.Fields, key string) (uuid.UUID, error) {
	s, err := f.RequiredString(key)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", key, syncapply.ErrInvalidFields)
	}
	return id, nil
}

type mountWire struct {
	EngineID    uuid.UUID  `json:"engine_id"`
	CarID       uuid.UUID  `json:"car_id"`
	MountedAt   time.Time  `json:"mounted_at"`
	UnmountedAt *time.Time `json:"unmounted_at"`
}

func (a *EngineMountApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	m, err := a.repo.GetMount(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(mountWire{EngineID: m.EngineID, CarID: m.CarID, MountedAt: m.MountedAt, UnmountedAt: m.UnmountedAt})
	return raw, err == nil, err
}

// --------------------------------------------------------------------------- part installs

type PartInstallApplier struct {
	repo *store.Repo
	now  func() time.Time
}

func NewPartInstallApplier() *PartInstallApplier {
	return &PartInstallApplier{repo: store.NewRepo(), now: time.Now}
}

var _ syncapply.Applier = (*PartInstallApplier)(nil)

func (*PartInstallApplier) Entity() string { return "garage.part_install" }

func (a *PartInstallApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	i, err := a.repo.GetPartInstall(ctx, tx, uuid.UUID(team), id)
	if err != nil {
		return 0, carSyncErr(err)
	}
	return i.Version, nil
}

func (a *PartInstallApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	f := syncapply.Fields(op.Fields)
	team, user := uuid.UUID(op.TeamID), uuid.UUID(op.UserID)
	now := a.now()
	switch op.Kind {
	case syncapply.Create:
		if err := f.Only("part_id", "car_id", "engine_id", "installed_at"); err != nil {
			return 0, err
		}
		partID, err := requiredUUID(f, "part_id")
		if err != nil {
			return 0, err
		}
		_, carRaw, err1 := f.String("car_id")
		_, engineRaw, err2 := f.String("engine_id")
		_, installedRaw, err3 := f.String("installed_at")
		if err := errors.Join(err1, err2, err3); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		var carID, engineID *uuid.UUID
		for _, t := range []struct {
			raw *string
			dst **uuid.UUID
		}{{carRaw, &carID}, {engineRaw, &engineID}} {
			if t.raw == nil {
				continue
			}
			id, err := uuid.Parse(*t.raw)
			if err != nil {
				return 0, syncapply.ErrInvalidFields
			}
			*t.dst = &id
		}
		at, err := deviceTime(installedRaw, now)
		if err != nil {
			return 0, err
		}
		install, err := domain.NewPartInstall(partID, carID, engineID, &at, now.Add(movementMaxFuture))
		if err != nil {
			return 0, carSyncErr(err)
		}
		// Fixed lock order (part, then target), like InstallPart.
		if err := a.repo.LockPartForStock(ctx, tx, team, partID); err != nil {
			return 0, carSyncErr(refOf(err))
		}
		status := ""
		if carID != nil {
			if err := a.repo.LockCar(ctx, tx, team, *carID); err != nil {
				return 0, carSyncErr(refOf(err))
			}
			c, err := a.repo.GetCar(ctx, tx, team, *carID)
			if err != nil {
				return 0, carSyncErr(err)
			}
			status = c.Status
		} else {
			if err := a.repo.LockEngine(ctx, tx, team, *engineID); err != nil {
				return 0, carSyncErr(refOf(err))
			}
			e, err := a.repo.GetEngine(ctx, tx, team, *engineID)
			if err != nil {
				return 0, carSyncErr(err)
			}
			status = e.Status
		}
		part, err := a.repo.GetPart(ctx, tx, team, partID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if !part.HasServiceLife || status == domain.AssetRetired {
			return 0, fmt.Errorf("part cannot be installed here: %w", syncapply.ErrInvalidRef)
		}
		created, err := a.repo.CreatePartInstallWithID(ctx, tx, team, user, op.EntityID, install, at)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return created.Version, nil
	case syncapply.Patch:
		if err := f.Only("removed_at", "removal_destination", "removal_reason"); err != nil {
			return 0, err
		}
		destination, err := f.RequiredString("removal_destination")
		if err != nil {
			return 0, err
		}
		_, reason, err1 := f.String("removal_reason")
		_, removedRaw, err2 := f.String("removed_at")
		if err := errors.Join(err1, err2); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		removal, err := domain.NewRemoval(destination, deref(reason))
		if err != nil {
			return 0, carSyncErr(err)
		}
		current, err := a.repo.GetPartInstall(ctx, tx, team, op.EntityID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if !current.Active() {
			return current.Version, nil // closed by another device: the goal is reached
		}
		at, err := deviceTime(removedRaw, now)
		if err != nil {
			return 0, err
		}
		if !at.After(current.InstalledAt) {
			at = now
		}
		if !at.After(current.InstalledAt) {
			return 0, fmt.Errorf("removed_at: %w", syncapply.ErrInvalidFields)
		}
		if err := a.repo.LockPartForStock(ctx, tx, team, current.PartID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return 0, err
		}
		if _, err := a.repo.GetPartInstallForUpdate(ctx, tx, team, op.EntityID); err != nil {
			return 0, carSyncErr(err)
		}
		closed, err := a.repo.ClosePartInstallAt(ctx, tx, team, op.EntityID, removal, at)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return closed.Version, nil
	default:
		return 0, fmt.Errorf("part install operation kind: %w", syncapply.ErrInvalidFields)
	}
}

type installWire struct {
	PartID             uuid.UUID  `json:"part_id"`
	CarID              *uuid.UUID `json:"car_id"`
	EngineID           *uuid.UUID `json:"engine_id"`
	InstalledAt        time.Time  `json:"installed_at"`
	RemovedAt          *time.Time `json:"removed_at"`
	RemovalDestination *string    `json:"removal_destination"`
	RemovalReason      *string    `json:"removal_reason"`
	RunSeconds         int32      `json:"run_seconds"`
	Sessions           int32      `json:"sessions"`
	Version            int32      `json:"version"`
}

func (a *PartInstallApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	i, err := a.repo.GetPartInstall(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	w := installWire{PartID: i.PartID, CarID: i.CarID, EngineID: i.EngineID, InstalledAt: i.InstalledAt,
		RemovedAt: i.RemovedAt, RunSeconds: i.RunSeconds, Sessions: i.Sessions, Version: i.Version}
	if i.RemovalDestination != "" {
		w.RemovalDestination = &i.RemovalDestination
	}
	if i.RemovalReason != "" {
		w.RemovalReason = &i.RemovalReason
	}
	raw, err := json.Marshal(w)
	return raw, err == nil, err
}
