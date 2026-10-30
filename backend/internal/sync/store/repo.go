// Package store is the persistence layer of the sync module (sqlc).
package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/store/sqlcgen"
)

// Repo keeps no state; each method uses the transaction opened by the use case.
type Repo struct{}

func NewRepo() *Repo { return &Repo{} }

func (Repo) InsertDevice(ctx context.Context, tx db.DBTX, team, user uuid.UUID, name, platform string, publicKey []byte) (uuid.UUID, error) {
	id, err := sqlcgen.New(tx).InsertDeviceKey(ctx, sqlcgen.InsertDeviceKeyParams{
		TeamID: team, UserID: user, DeviceName: name, PublicKey: publicKey,
		Platform: sqlcgen.IdentityDevicePlatform(platform),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert device key: %w", err)
	}
	return id, nil
}

func (Repo) DeviceByPublicKey(ctx context.Context, tx db.DBTX, team, user uuid.UUID, publicKey []byte) (uuid.UUID, bool, error) {
	id, err := sqlcgen.New(tx).GetDeviceKeyByPublicKey(ctx, sqlcgen.GetDeviceKeyByPublicKeyParams{
		TeamID: team, UserID: user, PublicKey: publicKey,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, false, nil
	case err != nil:
		return uuid.Nil, false, fmt.Errorf("get device by key: %w", err)
	}
	return id, true, nil
}

func (Repo) ActiveDevices(ctx context.Context, tx db.DBTX, team, user uuid.UUID) (int64, error) {
	n, err := sqlcgen.New(tx).CountActiveDevices(ctx, sqlcgen.CountActiveDevicesParams{TeamID: team, UserID: user})
	if err != nil {
		return 0, fmt.Errorf("count devices: %w", err)
	}
	return n, nil
}

func (Repo) Device(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Device, error) {
	r, err := sqlcgen.New(tx).GetDeviceKey(ctx, sqlcgen.GetDeviceKeyParams{TeamID: team, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Device{}, domain.ErrNotFound
	case err != nil:
		return domain.Device{}, fmt.Errorf("get device key: %w", err)
	}
	return domain.Device{ID: r.ID, UserID: r.UserID, PublicKey: r.PublicKey, Revoked: r.RevokedAt != nil}, nil
}

func (Repo) TouchDevice(ctx context.Context, tx db.DBTX, team, id uuid.UUID) error {
	if err := sqlcgen.New(tx).TouchDeviceKey(ctx, sqlcgen.TouchDeviceKeyParams{TeamID: team, ID: id}); err != nil {
		return fmt.Errorf("touch device key: %w", err)
	}
	return nil
}

// RevokeDevice revokes the key and every pass of the device in the same transaction.
func (Repo) RevokeDevice(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (bool, error) {
	q := sqlcgen.New(tx)
	_, err := q.RevokeDeviceKey(ctx, sqlcgen.RevokeDeviceKeyParams{TeamID: team, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("revoke device key: %w", err)
	}
	if err := q.RevokeDevicePasses(ctx, sqlcgen.RevokeDevicePassesParams{TeamID: team, DeviceKeyID: id}); err != nil {
		return false, fmt.Errorf("revoke device passes: %w", err)
	}
	return true, nil
}

// --- offline pass ------------------------------------------------------------

func (Repo) InsertPass(ctx context.Context, tx db.DBTX, team, user, device uuid.UUID, role string, ttl time.Duration) (domain.Pass, error) {
	r, err := sqlcgen.New(tx).InsertOfflinePass(ctx, sqlcgen.InsertOfflinePassParams{
		TeamID: team, UserID: user, DeviceKeyID: device, Role: sqlcgen.IdentityTeamRole(role), TtlSeconds: ttl.Seconds(),
	})
	if err != nil {
		return domain.Pass{}, fmt.Errorf("insert offline pass: %w", err)
	}
	return domain.Pass{ID: r.ID, UserID: user, DeviceID: device, Role: role, IssuedAt: r.IssuedAt, ExpiresAt: r.ExpiresAt}, nil
}

func (Repo) Pass(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Pass, error) {
	r, err := sqlcgen.New(tx).GetOfflinePass(ctx, sqlcgen.GetOfflinePassParams{TeamID: team, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Pass{}, domain.ErrNotFound
	case err != nil:
		return domain.Pass{}, fmt.Errorf("get offline pass: %w", err)
	}
	return domain.Pass{
		ID: r.ID, UserID: r.UserID, DeviceID: r.DeviceKeyID, Role: r.Role,
		IssuedAt: r.IssuedAt, ExpiresAt: r.ExpiresAt, Revoked: r.RevokedAt != nil,
	}, nil
}

// --- operations --------------------------------------------------------------

var kindNames = map[domain.Kind]sqlcgen.SyncOpKind{
	domain.KindCreate: sqlcgen.SyncOpKindCreate,
	domain.KindPatch:  sqlcgen.SyncOpKindPatch,
	domain.KindAppend: sqlcgen.SyncOpKindAppend,
	domain.KindDelete: sqlcgen.SyncOpKindDelete,
}

func int4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func int4Ptr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func (Repo) Operation(ctx context.Context, tx db.DBTX, opID uuid.UUID) (domain.StoredOp, bool, error) {
	r, err := sqlcgen.New(tx).GetOperation(ctx, opID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.StoredOp{}, false, nil
	case err != nil:
		return domain.StoredOp{}, false, fmt.Errorf("get operation: %w", err)
	}
	return domain.StoredOp{
		OpID: r.OpID, TeamID: r.TeamID, UserID: r.UserID, DeviceID: r.DeviceKeyID,
		Entity: r.Entity, EntityID: r.EntityID, Status: domain.Status(r.Status),
		Reason: r.Reason, ResultVersion: int4Ptr(r.ResultVersion),
	}, true, nil
}

func (Repo) InsertOperation(ctx context.Context, tx db.DBTX, op domain.StoredOp) error {
	kind, ok := kindNames[op.Kind]
	if !ok {
		return fmt.Errorf("insert operation: %w", domain.ErrInvalid)
	}
	err := sqlcgen.New(tx).InsertOperation(ctx, sqlcgen.InsertOperationParams{
		OpID: op.OpID, TeamID: op.TeamID, UserID: op.UserID, DeviceKeyID: op.DeviceID,
		Entity: op.Entity, EntityID: op.EntityID, Kind: kind, BaseVersion: int4(op.BaseVersion),
		Fields: op.Fields, ClientTs: op.ClientTS, Status: sqlcgen.SyncOpStatus(op.Status),
		Reason: op.Reason, DeviceSignature: op.Signature, ResultVersion: int4(op.ResultVersion),
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.ErrDuplicateOp
	}
	if err != nil {
		return fmt.Errorf("insert operation: %w", err)
	}
	return nil
}

func (Repo) AppliedSince(ctx context.Context, tx db.DBTX, team uuid.UUID, entity string, entityID uuid.UUID, version int32) ([]domain.Applied, error) {
	rows, err := sqlcgen.New(tx).ListAppliedSince(ctx, sqlcgen.ListAppliedSinceParams{
		TeamID: team, Entity: entity, EntityID: entityID, ResultVersion: int4(&version),
	})
	if err != nil {
		return nil, fmt.Errorf("list applied since: %w", err)
	}
	out := make([]domain.Applied, 0, len(rows))
	for _, r := range rows {
		fields, err := domain.ParseFields(r.Fields)
		if err != nil {
			return nil, fmt.Errorf("stored operation fields: %w", err)
		}
		out = append(out, domain.Applied{Version: r.ResultVersion.Int32, Fields: fields})
	}
	return out, nil
}

// --- conflicts ---------------------------------------------------------------

func (Repo) InsertConflict(ctx context.Context, tx db.DBTX, team uuid.UUID, c domain.Conflict) error {
	err := sqlcgen.New(tx).InsertConflict(ctx, sqlcgen.InsertConflictParams{
		TeamID: team, OpID: c.OpID, Entity: c.Entity, EntityID: c.EntityID, Field: c.Field,
		ServerValue: c.ServerValue, ClientValue: c.ClientValue,
	})
	if err != nil {
		return fmt.Errorf("insert conflict: %w", err)
	}
	return nil
}

func (Repo) OpenConflicts(ctx context.Context, tx db.DBTX, team uuid.UUID) ([]domain.Conflict, error) {
	rows, err := sqlcgen.New(tx).ListOpenConflicts(ctx, team)
	if err != nil {
		return nil, fmt.Errorf("list open conflicts: %w", err)
	}
	out := make([]domain.Conflict, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.Conflict{
			ID: r.ID, OpID: r.OpID, Entity: r.Entity, EntityID: r.EntityID, Field: r.Field,
			ServerValue: r.ServerValue, ClientValue: r.ClientValue, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func (Repo) Conflict(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Conflict, bool, error) {
	r, err := sqlcgen.New(tx).GetConflict(ctx, sqlcgen.GetConflictParams{TeamID: team, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Conflict{}, false, nil
	case err != nil:
		return domain.Conflict{}, false, fmt.Errorf("get conflict: %w", err)
	}
	return domain.Conflict{
		ID: r.ID, OpID: r.OpID, Entity: r.Entity, EntityID: r.EntityID, Field: r.Field,
		ServerValue: r.ServerValue, ClientValue: r.ClientValue, CreatedAt: r.CreatedAt,
		Resolved: r.ResolvedAt != nil,
	}, true, nil
}

// ResolveConflict returns false when the conflict was already resolved (or is not visible).
func (Repo) ResolveConflict(ctx context.Context, tx db.DBTX, team, id uuid.UUID, resolution string, value []byte, by uuid.UUID) (domain.Conflict, bool, error) {
	r, err := sqlcgen.New(tx).ResolveConflict(ctx, sqlcgen.ResolveConflictParams{
		TeamID: team, ID: id, Resolution: &resolution, ResolvedValue: value, ResolvedBy: &by,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Conflict{}, false, nil
	case err != nil:
		return domain.Conflict{}, false, fmt.Errorf("resolve conflict: %w", err)
	}
	return domain.Conflict{
		ID: r.ID, OpID: r.OpID, Entity: r.Entity, EntityID: r.EntityID, Field: r.Field,
		ServerValue: r.ServerValue, ClientValue: r.ClientValue, CreatedAt: r.CreatedAt, Resolved: true,
	}, true, nil
}

// --- change log --------------------------------------------------------------

func (Repo) Changes(ctx context.Context, tx db.DBTX, team uuid.UUID, after domain.Cursor, limit int32) ([]domain.Change, error) {
	rows, err := sqlcgen.New(tx).PullChanges(ctx, sqlcgen.PullChangesParams{
		TeamID: team, AfterXid: strconv.FormatUint(after.Xid, 10), AfterSeq: after.Seq, Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("pull changes: %w", err)
	}
	out := make([]domain.Change, 0, len(rows))
	for _, r := range rows {
		xid, err := strconv.ParseUint(r.Xid, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("change log xid: %w", err)
		}
		out = append(out, domain.Change{
			Cursor: domain.Cursor{Xid: xid, Seq: r.Seq}, Entity: r.Entity, EntityID: r.EntityID, Deleted: r.Op == "D",
		})
	}
	return out, nil
}
