package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Store is the garage persistence surface. Every method runs in the transaction the use
// case opened.
type Store interface {
	ActiveCatalog(ctx context.Context, tx db.DBTX, scale, powertrain string) error

	CreateCar(ctx context.Context, tx db.DBTX, team, user uuid.UUID, c domain.Car) (domain.Car, error)
	GetCar(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Car, error)
	ListCars(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.Car, error)
	UpdateCar(ctx context.Context, tx db.DBTX, team uuid.UUID, c domain.Car) (domain.Car, error)
	SoftDeleteCar(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error

	CreateEngine(ctx context.Context, tx db.DBTX, team, user uuid.UUID, e domain.Engine) (domain.Engine, error)
	GetEngine(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Engine, error)
	ListEngines(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.Engine, error)
	UpdateEngine(ctx context.Context, tx db.DBTX, team uuid.UUID, e domain.Engine) (domain.Engine, error)
	SoftDeleteEngine(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error

	CreateTyreSet(ctx context.Context, tx db.DBTX, team, user uuid.UUID, t domain.TyreSet) (domain.TyreSet, error)
	GetTyreSet(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.TyreSet, error)
	ListTyreSets(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.TyreSet, error)
	UpdateTyreSet(ctx context.Context, tx db.DBTX, team uuid.UUID, t domain.TyreSet) (domain.TyreSet, error)
	SoftDeleteTyreSet(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error

	CreatePart(ctx context.Context, tx db.DBTX, team, user uuid.UUID, p domain.Part) (domain.Part, error)
	GetPart(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Part, error)
	ListParts(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.Part, error)
	UpdatePart(ctx context.Context, tx db.DBTX, team uuid.UUID, p domain.Part) (domain.Part, error)
	SoftDeletePart(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error
	LockPartForStock(ctx context.Context, tx db.DBTX, team, part uuid.UUID) error
	PartStockCenti(ctx context.Context, tx db.DBTX, team, part uuid.UUID) (int64, error)
	CreateStockMovement(ctx context.Context, tx db.DBTX, team, user uuid.UUID, m domain.StockMovement) (domain.StockMovement, error)
	ListStockMovements(ctx context.Context, tx db.DBTX, team, part, after uuid.UUID, limit int32) ([]domain.StockMovement, error)

	LockEngine(ctx context.Context, tx db.DBTX, team, id uuid.UUID) error
	LockCar(ctx context.Context, tx db.DBTX, team, id uuid.UUID) error
	OpenMountForEngine(ctx context.Context, tx db.DBTX, team, engine uuid.UUID) (domain.EngineMount, bool, error)
	OpenMountForCar(ctx context.Context, tx db.DBTX, team, car uuid.UUID) (domain.EngineMount, bool, error)
	CreateMount(ctx context.Context, tx db.DBTX, team, user uuid.UUID, m domain.EngineMount) (domain.EngineMount, error)
	CloseMount(ctx context.Context, tx db.DBTX, team, id uuid.UUID) error
	CreatePartInstall(ctx context.Context, tx db.DBTX, team, user uuid.UUID, in domain.NewInstall) (domain.PartInstall, error)
	GetPartInstallForUpdate(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.PartInstall, error)
	ClosePartInstall(ctx context.Context, tx db.DBTX, team, id uuid.UUID, removal domain.Removal) (domain.PartInstall, error)
	ListPartInstalls(ctx context.Context, tx db.DBTX, team uuid.UUID, filter domain.InstallFilter, history bool, after uuid.UUID, limit int32) ([]domain.PartInstall, error)
	ListMountsForEngine(ctx context.Context, tx db.DBTX, team, engine, after uuid.UUID, limit int32) ([]domain.EngineMount, error)
}
