// Package store is the setup module's persistence layer.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/store/sqlcgen"
)

// Querier is what the public share reader needs from the application pool.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repo is the setup persistence layer. Every method uses the transaction opened
// by the use case and never opens its own.
type Repo struct{}

func NewRepo() *Repo { return &Repo{} }

func (Repo) CreateSetup(ctx context.Context, tx db.DBTX, team, user uuid.UUID, carID uuid.UUID, name string, notes *string) (domain.Setup, error) {
	row, err := sqlcgen.New(tx).CreateSetup(ctx, sqlcgen.CreateSetupParams{
		TeamID: team, CarID: carID, Name: name, Notes: notes, CreatedBy: &user,
	})
	if err != nil {
		return domain.Setup{}, mapError("create setup", err)
	}
	return setupFromCreate(row), nil
}

// CreateSetupWithID creates a setup with an id chosen by the caller (the sync layer).
func (Repo) CreateSetupWithID(ctx context.Context, tx db.DBTX, team, user, id, carID uuid.UUID, name string, notes *string) (domain.Setup, error) {
	row, err := sqlcgen.New(tx).CreateSetupWithID(ctx, sqlcgen.CreateSetupWithIDParams{
		ID: id, TeamID: team, CarID: carID, Name: name, Notes: notes, CreatedBy: &user,
	})
	if err != nil {
		return domain.Setup{}, mapError("create setup", err)
	}
	return domain.Setup{ID: row.ID.String(), CarID: row.CarID.String(), Name: row.Name,
		CurrentVersion: row.CurrentVersion, Notes: row.Notes, CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt, Version: row.Version}, nil
}

func (Repo) GetSetup(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Setup, error) {
	row, err := sqlcgen.New(tx).GetSetup(ctx, sqlcgen.GetSetupParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Setup{}, fmt.Errorf("get setup: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Setup{}, fmt.Errorf("get setup: %w", err)
	}
	return setupFromGet(row), nil
}

func (Repo) LockSetup(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Setup, error) {
	row, err := sqlcgen.New(tx).LockSetup(ctx, sqlcgen.LockSetupParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Setup{}, fmt.Errorf("lock setup: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Setup{}, fmt.Errorf("lock setup: %w", err)
	}
	return setupFromLock(row), nil
}

func (Repo) ListSetups(ctx context.Context, tx db.DBTX, team, after uuid.UUID, carID *uuid.UUID, limit int32) ([]domain.Setup, error) {
	if carID == nil {
		rows, err := sqlcgen.New(tx).ListSetups(ctx, sqlcgen.ListSetupsParams{TeamID: team, ID: after, Limit: limit})
		if err != nil {
			return nil, fmt.Errorf("list setups: %w", err)
		}
		out := make([]domain.Setup, 0, len(rows))
		for _, row := range rows {
			out = append(out, setupFromList(row))
		}
		return out, nil
	}
	rows, err := sqlcgen.New(tx).ListSetupsByCar(ctx, sqlcgen.ListSetupsByCarParams{TeamID: team, CarID: *carID, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list setups by car: %w", err)
	}
	out := make([]domain.Setup, 0, len(rows))
	for _, row := range rows {
		out = append(out, setupFromListByCar(row))
	}
	return out, nil
}

func (Repo) UpdateCurrentVersion(ctx context.Context, tx db.DBTX, team, id uuid.UUID, current, expected int32) (domain.Setup, error) {
	row, err := sqlcgen.New(tx).UpdateCurrentVersion(ctx, sqlcgen.UpdateCurrentVersionParams{
		CurrentVersion: current, TeamID: team, ID: id, CurrentVersion_2: expected,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Setup{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Setup{}, fmt.Errorf("update setup version: %w", err)
	}
	return setupFromUpdateVersion(row), nil
}

func (Repo) CreateVersion(ctx context.Context, tx db.DBTX, team, user uuid.UUID, setupID uuid.UUID, version int32, data []byte, basedOnSetup *uuid.UUID, basedOnVersion *int32, summary *string) (domain.VersionRef, error) {
	row, err := sqlcgen.New(tx).CreateVersion(ctx, sqlcgen.CreateVersionParams{
		TeamID: team, SetupID: setupID, Version: version, Column4: data,
		SchemaVersion: domain.SchemaVersion, BasedOnSetupID: basedOnSetup,
		BasedOnVersion: int4(basedOnVersion), ChangeSummary: summary, CreatedBy: &user,
	})
	if err != nil {
		return domain.VersionRef{}, mapError("create setup version", err)
	}
	return versionRefFromCreate(row), nil
}

func (Repo) GetVersion(ctx context.Context, tx db.DBTX, team, setupID uuid.UUID, version int32) (domain.Version, error) {
	row, err := sqlcgen.New(tx).GetVersion(ctx, sqlcgen.GetVersionParams{
		TeamID: team, SetupID: setupID, Column3: version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Version{}, fmt.Errorf("get setup version: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Version{}, fmt.Errorf("get setup version: %w", err)
	}
	data, err := domain.DecodeData([]byte(row.Data))
	if err != nil {
		return domain.Version{}, err
	}
	return domain.Version{Ref: versionRefFromGet(row), Data: data}, nil
}

func (Repo) ListVersions(ctx context.Context, tx db.DBTX, team, setupID uuid.UUID, before, limit int32) ([]domain.VersionRef, error) {
	rows, err := sqlcgen.New(tx).ListVersions(ctx, sqlcgen.ListVersionsParams{
		TeamID: team, SetupID: setupID, Version: before, Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list setup versions: %w", err)
	}
	out := make([]domain.VersionRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, versionRefFromList(row))
	}
	return out, nil
}

func (Repo) CreateShareLink(ctx context.Context, tx db.DBTX, team, setupID, user uuid.UUID, pinned *int32, tokenHash []byte, ttl *string) (domain.ShareLink, error) {
	row, err := sqlcgen.New(tx).CreateShareLink(ctx, sqlcgen.CreateShareLinkParams{
		TeamID: team, SetupID: setupID, PinnedVersion: int4(pinned),
		TokenHash: tokenHash, CreatedBy: user, Ttl: ttl,
	})
	if err != nil {
		return domain.ShareLink{}, mapError("create share link", err)
	}
	return shareLinkFromCreate(row), nil
}

func (Repo) ListShareLinks(ctx context.Context, tx db.DBTX, team, setupID uuid.UUID) ([]domain.ShareLink, error) {
	rows, err := sqlcgen.New(tx).ListShareLinks(ctx, sqlcgen.ListShareLinksParams{TeamID: team, SetupID: setupID})
	if err != nil {
		return nil, fmt.Errorf("list share links: %w", err)
	}
	out := make([]domain.ShareLink, 0, len(rows))
	for _, row := range rows {
		out = append(out, shareLinkFromList(row))
	}
	return out, nil
}

func (Repo) CountActiveShareLinks(ctx context.Context, tx db.DBTX, team, setupID uuid.UUID) (int32, error) {
	n, err := sqlcgen.New(tx).CountActiveShareLinks(ctx, sqlcgen.CountActiveShareLinksParams{TeamID: team, SetupID: setupID})
	if err != nil {
		return 0, fmt.Errorf("count share links: %w", err)
	}
	return n, nil
}

func (Repo) GetShareLink(ctx context.Context, tx db.DBTX, team, setupID, id uuid.UUID) (domain.ShareLink, error) {
	row, err := sqlcgen.New(tx).GetShareLink(ctx, sqlcgen.GetShareLinkParams{TeamID: team, SetupID: setupID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ShareLink{}, fmt.Errorf("get share link: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.ShareLink{}, fmt.Errorf("get share link: %w", err)
	}
	return shareLinkFromGet(row), nil
}

func (Repo) RevokeShareLink(ctx context.Context, tx db.DBTX, team, setupID, id uuid.UUID) (bool, error) {
	_, err := sqlcgen.New(tx).RevokeShareLink(ctx, sqlcgen.RevokeShareLinkParams{TeamID: team, SetupID: setupID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("revoke share link: %w", err)
	}
	return true, nil
}

// ShareReader reads one setup through the SECURITY DEFINER function, without a
// team context. The query is parameterised and uses no dynamic SQL.
type ShareReader struct{ db Querier }

func NewShareReader(db Querier) *ShareReader { return &ShareReader{db: db} }

func (r *ShareReader) Open(ctx context.Context, tokenHash []byte) (domain.SharedSetup, error) {
	// The function is a table function with RETURNS TABLE; sqlc does not
	// introspect its output columns. This parameterised read follows the
	// identity.resolve_login precedent instead of hand-building SQL.
	var data string
	var changeSummary *string
	var createdAt time.Time
	var name string
	var version, schemaVersion int32
	err := r.db.QueryRow(ctx, `
SELECT name, version, schema_version, data::text, change_summary, version_created_at
  FROM setup.open_share_link($1)`, tokenHash).Scan(
		&name, &version, &schemaVersion, &data, &changeSummary, &createdAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SharedSetup{}, fmt.Errorf("open share link: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.SharedSetup{}, fmt.Errorf("open share link: %w", err)
	}
	parsed, err := domain.DecodeData([]byte(data))
	if err != nil {
		return domain.SharedSetup{}, err
	}
	return domain.SharedSetup{
		Name: name, Version: version, SchemaVersion: schemaVersion, Data: parsed,
		ChangeSummary: changeSummary, VersionCreatedAt: createdAt,
	}, nil
}

func setupFromCreate(r sqlcgen.CreateSetupRow) domain.Setup {
	return domain.Setup{ID: r.ID.String(), CarID: r.CarID.String(), Name: r.Name, CurrentVersion: r.CurrentVersion,
		Notes: r.Notes, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version}
}

func setupFromGet(r sqlcgen.GetSetupRow) domain.Setup {
	return domain.Setup{ID: r.ID.String(), CarID: r.CarID.String(), Name: r.Name, CurrentVersion: r.CurrentVersion,
		Notes: r.Notes, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version}
}

func setupFromLock(r sqlcgen.LockSetupRow) domain.Setup {
	return domain.Setup{ID: r.ID.String(), CarID: r.CarID.String(), Name: r.Name, CurrentVersion: r.CurrentVersion,
		Notes: r.Notes, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version}
}

func setupFromList(r sqlcgen.ListSetupsRow) domain.Setup {
	return domain.Setup{ID: r.ID.String(), CarID: r.CarID.String(), Name: r.Name, CurrentVersion: r.CurrentVersion,
		Notes: r.Notes, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version}
}

func setupFromListByCar(r sqlcgen.ListSetupsByCarRow) domain.Setup {
	return domain.Setup{ID: r.ID.String(), CarID: r.CarID.String(), Name: r.Name, CurrentVersion: r.CurrentVersion,
		Notes: r.Notes, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version}
}

func setupFromUpdateVersion(r sqlcgen.UpdateCurrentVersionRow) domain.Setup {
	return domain.Setup{ID: r.ID.String(), CarID: r.CarID.String(), Name: r.Name, CurrentVersion: r.CurrentVersion,
		Notes: r.Notes, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version}
}

func versionRefFromCreate(r sqlcgen.CreateVersionRow) domain.VersionRef {
	return versionRef(r.SetupID, r.Version, r.SchemaVersion, r.BasedOnSetupID, r.BasedOnVersion, r.ChangeSummary, r.CreatedAt)
}

func versionRefFromGet(r sqlcgen.GetVersionRow) domain.VersionRef {
	return versionRef(r.SetupID, r.Version, r.SchemaVersion, r.BasedOnSetupID, r.BasedOnVersion, r.ChangeSummary, r.CreatedAt)
}

func versionRefFromList(r sqlcgen.ListVersionsRow) domain.VersionRef {
	return versionRef(r.SetupID, r.Version, r.SchemaVersion, r.BasedOnSetupID, r.BasedOnVersion, r.ChangeSummary, r.CreatedAt)
}

func versionRef(setupID uuid.UUID, version, schemaVersion int32, basedOn *uuid.UUID, basedOnVersion pgtype.Int4, summary *string, createdAt time.Time) domain.VersionRef {
	var basedID *string
	if basedOn != nil {
		s := basedOn.String()
		basedID = &s
	}
	return domain.VersionRef{
		SetupID: setupID.String(), Version: version, SchemaVersion: schemaVersion,
		BasedOnSetupID: basedID, BasedOnVersion: int4Ptr(basedOnVersion),
		ChangeSummary: summary, CreatedAt: createdAt,
	}
}

func shareLinkFromCreate(r sqlcgen.CreateShareLinkRow) domain.ShareLink {
	return shareLink(r.ID, r.SetupID, r.PinnedVersion, r.ExpiresAt, r.RevokedAt, r.LastUsedAt, r.UseCount, r.CreatedAt)
}

func shareLinkFromList(r sqlcgen.ListShareLinksRow) domain.ShareLink {
	return shareLink(r.ID, r.SetupID, r.PinnedVersion, r.ExpiresAt, r.RevokedAt, r.LastUsedAt, r.UseCount, r.CreatedAt)
}

func shareLinkFromGet(r sqlcgen.GetShareLinkRow) domain.ShareLink {
	return shareLink(r.ID, r.SetupID, r.PinnedVersion, r.ExpiresAt, r.RevokedAt, r.LastUsedAt, r.UseCount, r.CreatedAt)
}

func shareLink(id, setupID uuid.UUID, pinned pgtype.Int4, expiresAt, revokedAt, lastUsedAt *time.Time, useCount int32, createdAt time.Time) domain.ShareLink {
	return domain.ShareLink{
		ID: id.String(), SetupID: setupID.String(), PinnedVersion: int4Ptr(pinned),
		ExpiresAt: expiresAt, RevokedAt: revokedAt, LastUsedAt: lastUsedAt,
		UseCount: useCount, CreatedAt: createdAt,
	}
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
	n := v.Int32
	return &n
}

func mapError(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return fmt.Errorf("%s: %w", op, domain.ErrNotFound)
		case "23505":
			return fmt.Errorf("%s: %w", op, domain.ErrInvalid)
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

// CreateFeedbackWithID stores the feedback with the id the device chose. A missing setup
// version is the composite foreign key (domain.ErrNotFound); the same id twice is the
// primary key (domain.ErrInvalid).
func (Repo) CreateFeedbackWithID(ctx context.Context, tx db.DBTX, team uuid.UUID, f domain.Feedback) error {
	i2 := func(v *int16) pgtype.Int2 {
		if v == nil {
			return pgtype.Int2{}
		}
		return pgtype.Int2{Int16: *v, Valid: true}
	}
	_, err := sqlcgen.New(tx).CreateFeedbackWithID(ctx, sqlcgen.CreateFeedbackWithIDParams{
		ID: f.ID, TeamID: team, SetupID: f.SetupID, Version: f.Version, AuthorID: f.AuthorID, CreatedAt: f.CreatedAt,
		SteeringEntry: i2(f.SteeringEntry), SteeringMid: i2(f.SteeringMid), SteeringExit: i2(f.SteeringExit),
		Traction: i2(f.Traction), JumpStability: i2(f.JumpStability), Braking: i2(f.Braking),
		EngineLowEnd: i2(f.EngineLowEnd), EngineTopEnd: i2(f.EngineTopEnd), TrackZone: f.TrackZone, Note: f.Note,
	})
	if err != nil {
		return mapError("create setup feedback", err)
	}
	return nil
}

func (Repo) GetFeedback(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Feedback, error) {
	row, err := sqlcgen.New(tx).GetFeedback(ctx, sqlcgen.GetFeedbackParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Feedback{}, fmt.Errorf("get setup feedback: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Feedback{}, fmt.Errorf("get setup feedback: %w", err)
	}
	p := func(v pgtype.Int2) *int16 {
		if !v.Valid {
			return nil
		}
		return &v.Int16
	}
	return domain.Feedback{
		ID: row.ID, SetupID: row.SetupID, Version: row.Version, AuthorID: row.AuthorID, CreatedAt: row.CreatedAt,
		SteeringEntry: p(row.SteeringEntry), SteeringMid: p(row.SteeringMid), SteeringExit: p(row.SteeringExit),
		Traction: p(row.Traction), JumpStability: p(row.JumpStability), Braking: p(row.Braking),
		EngineLowEnd: p(row.EngineLowEnd), EngineTopEnd: p(row.EngineTopEnd), TrackZone: row.TrackZone, Note: row.Note,
	}, nil
}
