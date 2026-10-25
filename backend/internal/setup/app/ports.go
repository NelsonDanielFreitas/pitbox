package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// Store is the setup persistence surface. Every method runs inside the
// transaction opened by the use case.
type Store interface {
	CreateSetup(ctx context.Context, tx db.DBTX, team, user uuid.UUID, carID uuid.UUID, name string, notes *string) (domain.Setup, error)
	GetSetup(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Setup, error)
	LockSetup(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Setup, error)
	ListSetups(ctx context.Context, tx db.DBTX, team, after uuid.UUID, carID *uuid.UUID, limit int32) ([]domain.Setup, error)
	UpdateCurrentVersion(ctx context.Context, tx db.DBTX, team, id uuid.UUID, current, expected int32) (domain.Setup, error)

	CreateVersion(ctx context.Context, tx db.DBTX, team, user uuid.UUID, setupID uuid.UUID, version int32, data []byte, basedOnSetup *uuid.UUID, basedOnVersion *int32, summary *string) (domain.VersionRef, error)
	GetVersion(ctx context.Context, tx db.DBTX, team, setupID uuid.UUID, version int32) (domain.Version, error)
	ListVersions(ctx context.Context, tx db.DBTX, team, setupID uuid.UUID, before, limit int32) ([]domain.VersionRef, error)

	CreateShareLink(ctx context.Context, tx db.DBTX, team, setupID, user uuid.UUID, pinned *int32, tokenHash []byte, expiry string) (domain.ShareLink, error)
	ListShareLinks(ctx context.Context, tx db.DBTX, team, setupID uuid.UUID) ([]domain.ShareLink, error)
	CountActiveShareLinks(ctx context.Context, tx db.DBTX, team, setupID uuid.UUID) (int32, error)
	GetShareLink(ctx context.Context, tx db.DBTX, team, setupID, id uuid.UUID) (domain.ShareLink, error)
	RevokeShareLink(ctx context.Context, tx db.DBTX, team, setupID, id uuid.UUID) error
}

// Authorizer is the subset of OpenFGA used by the setup use cases.
type Authorizer interface {
	Check(ctx context.Context, user, relation, object string) error
}

// Garage provides the team-scoped existence checks for references in setup data.
type Garage interface {
	CarExists(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (bool, error)
	EngineExists(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (bool, error)
	TyreSetExists(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (bool, error)
}

// ShareReader reads one setup through the narrow SECURITY DEFINER function.
type ShareReader interface {
	Open(ctx context.Context, tokenHash []byte) (domain.SharedSetup, error)
}
