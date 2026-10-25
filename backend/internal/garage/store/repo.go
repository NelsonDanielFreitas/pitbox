package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store/sqlcgen"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Querier is the narrow database surface used by the public API and the store.
// The application pool satisfies it.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repo is the garage persistence layer. Every method uses the transaction opened by the
// use case and never opens its own.
type Repo struct{}

func NewRepo() *Repo { return &Repo{} }

// --- catalog -----------------------------------------------------------------

func (Repo) ActiveCatalog(ctx context.Context, tx db.DBTX, scale, powertrain string) error {
	q := sqlcgen.New(tx)
	s, err := q.GetScale(ctx, scale)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("get scale: %w", domain.ErrInvalid)
	case err != nil:
		return fmt.Errorf("get scale: %w", err)
	case !s.Active:
		return fmt.Errorf("get scale: %w", domain.ErrCatalogInactive)
	}
	p, err := q.GetPowertrain(ctx, powertrain)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("get powertrain: %w", domain.ErrInvalid)
	case err != nil:
		return fmt.Errorf("get powertrain: %w", err)
	case !p.Active:
		return fmt.Errorf("get powertrain: %w", domain.ErrCatalogInactive)
	}
	return nil
}

func (Repo) Scales(ctx context.Context, q sqlcgen.DBTX) ([]sqlcgen.ListScalesRow, error) {
	rows, err := sqlcgen.New(q).ListScales(ctx)
	if err != nil {
		return nil, fmt.Errorf("list scales: %w", err)
	}
	return rows, nil
}

func (Repo) Powertrains(ctx context.Context, q sqlcgen.DBTX) ([]sqlcgen.ListPowertrainsRow, error) {
	rows, err := sqlcgen.New(q).ListPowertrains(ctx)
	if err != nil {
		return nil, fmt.Errorf("list powertrains: %w", err)
	}
	return rows, nil
}

// --- cars --------------------------------------------------------------------

func (Repo) CreateCar(ctx context.Context, tx db.DBTX, team, user uuid.UUID, c domain.Car) (domain.Car, error) {
	row, err := sqlcgen.New(tx).CreateCar(ctx, sqlcgen.CreateCarParams{
		TeamID: team, Make: c.Make, Model: c.Model, ModelYear: int2(c.ModelYear),
		SerialNumber: c.SerialNumber, Nickname: c.Nickname, TransponderNumber: c.TransponderNumber,
		Column8: sqlcgen.GarageAssetStatus(c.Status), Scale: c.Scale, Powertrain: c.Powertrain,
		Notes: c.Notes, CreatedBy: &user,
	})
	if err != nil {
		return domain.Car{}, mapError("create car", err)
	}
	return carFromCreate(row), nil
}

func (Repo) GetCar(ctx context.Context, tx db.DBTX, team uuid.UUID, id uuid.UUID) (domain.Car, error) {
	row, err := sqlcgen.New(tx).GetCar(ctx, sqlcgen.GetCarParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Car{}, fmt.Errorf("get car: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Car{}, fmt.Errorf("get car: %w", err)
	}
	return carFromGet(row), nil
}

func (Repo) ListCars(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.Car, error) {
	rows, err := sqlcgen.New(tx).ListCars(ctx, sqlcgen.ListCarsParams{TeamID: team, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list cars: %w", err)
	}
	out := make([]domain.Car, 0, len(rows))
	for _, row := range rows {
		out = append(out, carFromList(row))
	}
	return out, nil
}

func (Repo) UpdateCar(ctx context.Context, tx db.DBTX, team uuid.UUID, c domain.Car) (domain.Car, error) {
	row, err := sqlcgen.New(tx).UpdateCar(ctx, sqlcgen.UpdateCarParams{
		Make: c.Make, Model: c.Model, ModelYear: int2(c.ModelYear), SerialNumber: c.SerialNumber,
		Nickname: c.Nickname, TransponderNumber: c.TransponderNumber,
		Column7: sqlcgen.GarageAssetStatus(c.Status), Scale: c.Scale, Powertrain: c.Powertrain,
		Notes: c.Notes, TeamID: team, ID: c.ID, Version: c.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Car{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Car{}, mapError("update car", err)
	}
	return carFromUpdate(row), nil
}

func (Repo) SoftDeleteCar(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error {
	_, err := sqlcgen.New(tx).SoftDeleteCar(ctx, sqlcgen.SoftDeleteCarParams{TeamID: team, ID: id, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("delete car: %w", err)
	}
	return nil
}

func (Repo) CarExists(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (bool, error) {
	ok, err := sqlcgen.New(tx).CarExists(ctx, sqlcgen.CarExistsParams{TeamID: team, ID: id})
	if err != nil {
		return false, fmt.Errorf("car exists: %w", err)
	}
	return ok, nil
}

func (Repo) EngineExists(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (bool, error) {
	ok, err := sqlcgen.New(tx).EngineExists(ctx, sqlcgen.EngineExistsParams{TeamID: team, ID: id})
	if err != nil {
		return false, fmt.Errorf("engine exists: %w", err)
	}
	return ok, nil
}

func (Repo) TyreSetExists(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (bool, error) {
	ok, err := sqlcgen.New(tx).TyreSetExists(ctx, sqlcgen.TyreSetExistsParams{TeamID: team, ID: id})
	if err != nil {
		return false, fmt.Errorf("tyre set exists: %w", err)
	}
	return ok, nil
}

// --- engines -----------------------------------------------------------------

func (Repo) CreateEngine(ctx context.Context, tx db.DBTX, team, user uuid.UUID, e domain.Engine) (domain.Engine, error) {
	row, err := sqlcgen.New(tx).CreateEngine(ctx, sqlcgen.CreateEngineParams{
		TeamID: team, Make: e.Make, Model: e.Model, SerialNumber: e.SerialNumber,
		Column5: date(e.PurchasedOn), Column6: sqlcgen.GarageAssetStatus(e.Status),
		Notes: e.Notes, CreatedBy: &user,
	})
	if err != nil {
		return domain.Engine{}, mapError("create engine", err)
	}
	return engineFromCreate(row), nil
}

func (Repo) GetEngine(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Engine, error) {
	row, err := sqlcgen.New(tx).GetEngine(ctx, sqlcgen.GetEngineParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Engine{}, fmt.Errorf("get engine: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Engine{}, fmt.Errorf("get engine: %w", err)
	}
	return engineFromGet(row), nil
}

func (Repo) ListEngines(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.Engine, error) {
	rows, err := sqlcgen.New(tx).ListEngines(ctx, sqlcgen.ListEnginesParams{TeamID: team, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list engines: %w", err)
	}
	out := make([]domain.Engine, 0, len(rows))
	for _, row := range rows {
		out = append(out, engineFromList(row))
	}
	return out, nil
}

func (Repo) UpdateEngine(ctx context.Context, tx db.DBTX, team uuid.UUID, e domain.Engine) (domain.Engine, error) {
	row, err := sqlcgen.New(tx).UpdateEngine(ctx, sqlcgen.UpdateEngineParams{
		Make: e.Make, Model: e.Model, SerialNumber: e.SerialNumber, Column4: date(e.PurchasedOn),
		Column5: sqlcgen.GarageAssetStatus(e.Status), Notes: e.Notes,
		TeamID: team, ID: e.ID, Version: e.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Engine{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Engine{}, mapError("update engine", err)
	}
	return engineFromUpdate(row), nil
}

func (Repo) SoftDeleteEngine(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error {
	_, err := sqlcgen.New(tx).SoftDeleteEngine(ctx, sqlcgen.SoftDeleteEngineParams{TeamID: team, ID: id, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("delete engine: %w", err)
	}
	return nil
}

// --- tyres -------------------------------------------------------------------

func (Repo) CreateTyreSet(ctx context.Context, tx db.DBTX, team, user uuid.UUID, t domain.TyreSet) (domain.TyreSet, error) {
	row, err := sqlcgen.New(tx).CreateTyreSet(ctx, sqlcgen.CreateTyreSetParams{
		TeamID: team, Label: t.Label, Make: t.Make, Model: t.Model, Compound: t.Compound,
		InsertType: t.InsertType, Column7: date(t.GluedOn), Column8: sqlcgen.GarageTyreStatus(t.Status),
		WearNotes: t.WearNotes, CreatedBy: &user,
	})
	if err != nil {
		return domain.TyreSet{}, mapError("create tyre set", err)
	}
	return tyreFromCreate(row), nil
}

func (Repo) GetTyreSet(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.TyreSet, error) {
	row, err := sqlcgen.New(tx).GetTyreSet(ctx, sqlcgen.GetTyreSetParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TyreSet{}, fmt.Errorf("get tyre set: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.TyreSet{}, fmt.Errorf("get tyre set: %w", err)
	}
	return tyreFromGet(row), nil
}

func (Repo) ListTyreSets(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.TyreSet, error) {
	rows, err := sqlcgen.New(tx).ListTyreSets(ctx, sqlcgen.ListTyreSetsParams{TeamID: team, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list tyre sets: %w", err)
	}
	out := make([]domain.TyreSet, 0, len(rows))
	for _, row := range rows {
		out = append(out, tyreFromList(row))
	}
	return out, nil
}

func (Repo) UpdateTyreSet(ctx context.Context, tx db.DBTX, team uuid.UUID, t domain.TyreSet) (domain.TyreSet, error) {
	row, err := sqlcgen.New(tx).UpdateTyreSet(ctx, sqlcgen.UpdateTyreSetParams{
		Label: t.Label, Make: t.Make, Model: t.Model, Compound: t.Compound, InsertType: t.InsertType,
		Column6: date(t.GluedOn), Column7: sqlcgen.GarageTyreStatus(t.Status), WearNotes: t.WearNotes,
		TeamID: team, ID: t.ID, Version: t.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TyreSet{}, domain.ErrConflict
	}
	if err != nil {
		return domain.TyreSet{}, mapError("update tyre set", err)
	}
	return tyreFromUpdate(row), nil
}

func (Repo) SoftDeleteTyreSet(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error {
	_, err := sqlcgen.New(tx).SoftDeleteTyreSet(ctx, sqlcgen.SoftDeleteTyreSetParams{TeamID: team, ID: id, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("delete tyre set: %w", err)
	}
	return nil
}

// --- parts and stock ---------------------------------------------------------

func (Repo) CreatePart(ctx context.Context, tx db.DBTX, team, user uuid.UUID, p domain.Part) (domain.Part, error) {
	specs, err := json.Marshal(p.Specs)
	if err != nil {
		return domain.Part{}, fmt.Errorf("create part: %w", domain.ErrInvalid)
	}
	row, err := sqlcgen.New(tx).CreatePart(ctx, sqlcgen.CreatePartParams{
		TeamID: team, Column2: sqlcgen.GaragePartKind(p.Kind), Brand: p.Brand, Reference: p.Reference,
		Description: p.Description, Unit: p.Unit, Column7: specs, Column8: p.MinStockCenti,
		UnitCostCents: int4(p.UnitCostCents), Currency: p.Currency, HasServiceLife: p.HasServiceLife,
		CreatedBy: &user,
	})
	if err != nil {
		return domain.Part{}, mapError("create part", err)
	}
	return partFromCreate(row), nil
}

func (Repo) GetPart(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.Part, error) {
	row, err := sqlcgen.New(tx).GetPart(ctx, sqlcgen.GetPartParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Part{}, fmt.Errorf("get part: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Part{}, fmt.Errorf("get part: %w", err)
	}
	return partFromGet(row)
}

func (Repo) ListParts(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.Part, error) {
	rows, err := sqlcgen.New(tx).ListParts(ctx, sqlcgen.ListPartsParams{TeamID: team, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list parts: %w", err)
	}
	out := make([]domain.Part, 0, len(rows))
	for _, row := range rows {
		p, err := partFromList(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (Repo) UpdatePart(ctx context.Context, tx db.DBTX, team uuid.UUID, p domain.Part) (domain.Part, error) {
	specs, err := json.Marshal(p.Specs)
	if err != nil {
		return domain.Part{}, fmt.Errorf("update part: %w", domain.ErrInvalid)
	}
	row, err := sqlcgen.New(tx).UpdatePart(ctx, sqlcgen.UpdatePartParams{
		Column1: sqlcgen.GaragePartKind(p.Kind), Brand: p.Brand, Reference: p.Reference,
		Description: p.Description, Unit: p.Unit, Column6: specs, Column7: p.MinStockCenti,
		UnitCostCents: int4(p.UnitCostCents), Currency: p.Currency, HasServiceLife: p.HasServiceLife,
		TeamID: team, ID: p.ID, Version: p.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Part{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Part{}, mapError("update part", err)
	}
	return partFromUpdate(row)
}

func (Repo) SoftDeletePart(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error {
	_, err := sqlcgen.New(tx).SoftDeletePart(ctx, sqlcgen.SoftDeletePartParams{TeamID: team, ID: id, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("delete part: %w", err)
	}
	return nil
}

func (Repo) LockPartForStock(ctx context.Context, tx db.DBTX, team, part uuid.UUID) error {
	_, err := sqlcgen.New(tx).LockPartForStock(ctx, sqlcgen.LockPartForStockParams{TeamID: team, ID: part})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock part: %w", domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("lock part: %w", err)
	}
	return nil
}

func (Repo) PartStockCenti(ctx context.Context, tx db.DBTX, team, part uuid.UUID) (int64, error) {
	n, err := sqlcgen.New(tx).GetPartStockCenti(ctx, sqlcgen.GetPartStockCentiParams{TeamID: team, PartID: part})
	if err != nil {
		return 0, fmt.Errorf("get part stock: %w", err)
	}
	return n, nil
}

func (Repo) CreateStockMovement(ctx context.Context, tx db.DBTX, team, user uuid.UUID, m domain.StockMovement) (domain.StockMovement, error) {
	row, err := sqlcgen.New(tx).CreateStockMovement(ctx, sqlcgen.CreateStockMovementParams{
		TeamID: team, PartID: m.PartID, Column3: m.QtyDeltaCenti,
		Column4: sqlcgen.GarageStockReason(m.Reason), Note: m.Note, CreatedBy: &user,
	})
	if err != nil {
		return domain.StockMovement{}, mapError("create stock movement", err)
	}
	return movementFromCreate(row), nil
}

func (Repo) ListStockMovements(ctx context.Context, tx db.DBTX, team, part, after uuid.UUID, limit int32) ([]domain.StockMovement, error) {
	rows, err := sqlcgen.New(tx).ListStockMovements(ctx, sqlcgen.ListStockMovementsParams{
		TeamID: team, PartID: part, ID: after, Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list stock movements: %w", err)
	}
	out := make([]domain.StockMovement, 0, len(rows))
	for _, row := range rows {
		out = append(out, movementFromList(row))
	}
	return out, nil
}

// --- engine mounts -----------------------------------------------------------

func (Repo) LockEngine(ctx context.Context, tx db.DBTX, team, id uuid.UUID) error {
	_, err := sqlcgen.New(tx).LockEngine(ctx, sqlcgen.LockEngineParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock engine: %w", domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("lock engine: %w", err)
	}
	return nil
}

func (Repo) LockCar(ctx context.Context, tx db.DBTX, team, id uuid.UUID) error {
	_, err := sqlcgen.New(tx).LockCar(ctx, sqlcgen.LockCarParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock car: %w", domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("lock car: %w", err)
	}
	return nil
}

func (Repo) OpenMountForEngine(ctx context.Context, tx db.DBTX, team, engine uuid.UUID) (domain.EngineMount, bool, error) {
	row, err := sqlcgen.New(tx).GetOpenMountForEngine(ctx, sqlcgen.GetOpenMountForEngineParams{TeamID: team, EngineID: engine})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EngineMount{}, false, nil
	}
	if err != nil {
		return domain.EngineMount{}, false, fmt.Errorf("get engine mount: %w", err)
	}
	return mountFromEngine(row), true, nil
}

func (Repo) OpenMountForCar(ctx context.Context, tx db.DBTX, team, car uuid.UUID) (domain.EngineMount, bool, error) {
	row, err := sqlcgen.New(tx).GetOpenMountForCar(ctx, sqlcgen.GetOpenMountForCarParams{TeamID: team, CarID: car})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EngineMount{}, false, nil
	}
	if err != nil {
		return domain.EngineMount{}, false, fmt.Errorf("get car mount: %w", err)
	}
	return mountFromCar(row), true, nil
}

func (Repo) CreateMount(ctx context.Context, tx db.DBTX, team, user uuid.UUID, m domain.EngineMount) (domain.EngineMount, error) {
	row, err := sqlcgen.New(tx).CreateMount(ctx, sqlcgen.CreateMountParams{
		TeamID: team, EngineID: m.EngineID, CarID: m.CarID, CreatedBy: &user,
	})
	if err != nil {
		return domain.EngineMount{}, mapError("create engine mount", err)
	}
	return mountFromCreate(row), nil
}

func (Repo) CloseMount(ctx context.Context, tx db.DBTX, team, id uuid.UUID) error {
	_, err := sqlcgen.New(tx).CloseMount(ctx, sqlcgen.CloseMountParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrMountConflict
	}
	if err != nil {
		return mapError("close engine mount", err)
	}
	return nil
}

func (Repo) ListMountsForEngine(ctx context.Context, tx db.DBTX, team, engine, after uuid.UUID, limit int32) ([]domain.EngineMount, error) {
	rows, err := sqlcgen.New(tx).ListMountsForEngine(ctx, sqlcgen.ListMountsForEngineParams{
		TeamID: team, EngineID: engine, ID: after, Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list engine mounts: %w", err)
	}
	out := make([]domain.EngineMount, 0, len(rows))
	for _, row := range rows {
		out = append(out, mountFromList(row))
	}
	return out, nil
}

// --- mapping -----------------------------------------------------------------

func int2(v *int32) pgtype.Int2 {
	if v == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: int16(*v), Valid: true} //nolint:gosec // model_year is bounded by the domain (1990..2100)
}

func ptrInt2(v pgtype.Int2) *int32 {
	if !v.Valid {
		return nil
	}
	n := int32(v.Int16)
	return &n
}

func int4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func ptrInt4(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	n := v.Int32
	return &n
}

func date(v *time.Time) pgtype.Date {
	if v == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *v, Valid: true}
}

func ptrDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

func carFromCreate(r sqlcgen.CreateCarRow) domain.Car {
	return domain.Car{ID: r.ID, Make: r.Make, Model: r.Model, ModelYear: ptrInt2(r.ModelYear),
		SerialNumber: r.SerialNumber, Nickname: r.Nickname, TransponderNumber: r.TransponderNumber,
		Status: r.Status, Scale: r.Scale, Powertrain: r.Powertrain, Notes: r.Notes, CreatedAt: r.CreatedAt, Version: r.Version}
}
func carFromGet(r sqlcgen.GetCarRow) domain.Car {
	return domain.Car{ID: r.ID, Make: r.Make, Model: r.Model, ModelYear: ptrInt2(r.ModelYear),
		SerialNumber: r.SerialNumber, Nickname: r.Nickname, TransponderNumber: r.TransponderNumber,
		Status: r.Status, Scale: r.Scale, Powertrain: r.Powertrain, Notes: r.Notes, CreatedAt: r.CreatedAt, Version: r.Version}
}
func carFromList(r sqlcgen.ListCarsRow) domain.Car {
	return domain.Car{ID: r.ID, Make: r.Make, Model: r.Model, ModelYear: ptrInt2(r.ModelYear),
		SerialNumber: r.SerialNumber, Nickname: r.Nickname, TransponderNumber: r.TransponderNumber,
		Status: r.Status, Scale: r.Scale, Powertrain: r.Powertrain, Notes: r.Notes, CreatedAt: r.CreatedAt, Version: r.Version}
}
func carFromUpdate(r sqlcgen.UpdateCarRow) domain.Car {
	return domain.Car{ID: r.ID, Make: r.Make, Model: r.Model, ModelYear: ptrInt2(r.ModelYear),
		SerialNumber: r.SerialNumber, Nickname: r.Nickname, TransponderNumber: r.TransponderNumber,
		Status: r.Status, Scale: r.Scale, Powertrain: r.Powertrain, Notes: r.Notes, CreatedAt: r.CreatedAt, Version: r.Version}
}

func engineFromCreate(r sqlcgen.CreateEngineRow) domain.Engine {
	return domain.Engine{ID: r.ID, Make: r.Make, Model: r.Model, SerialNumber: r.SerialNumber,
		PurchasedOn: ptrDate(r.PurchasedOn), Status: r.Status, TanksCenti: r.TanksCenti,
		RunSecondsTotal: r.RunSecondsTotal, SessionsTotal: r.SessionsTotal, Notes: r.Notes,
		CreatedAt: r.CreatedAt, Version: r.Version}
}
func engineFromGet(r sqlcgen.GetEngineRow) domain.Engine {
	return domain.Engine{ID: r.ID, Make: r.Make, Model: r.Model, SerialNumber: r.SerialNumber,
		PurchasedOn: ptrDate(r.PurchasedOn), Status: r.Status, TanksCenti: r.TanksCenti,
		RunSecondsTotal: r.RunSecondsTotal, SessionsTotal: r.SessionsTotal, Notes: r.Notes,
		CreatedAt: r.CreatedAt, Version: r.Version}
}
func engineFromList(r sqlcgen.ListEnginesRow) domain.Engine {
	return domain.Engine{ID: r.ID, Make: r.Make, Model: r.Model, SerialNumber: r.SerialNumber,
		PurchasedOn: ptrDate(r.PurchasedOn), Status: r.Status, TanksCenti: r.TanksCenti,
		RunSecondsTotal: r.RunSecondsTotal, SessionsTotal: r.SessionsTotal, Notes: r.Notes,
		CreatedAt: r.CreatedAt, Version: r.Version}
}
func engineFromUpdate(r sqlcgen.UpdateEngineRow) domain.Engine {
	return domain.Engine{ID: r.ID, Make: r.Make, Model: r.Model, SerialNumber: r.SerialNumber,
		PurchasedOn: ptrDate(r.PurchasedOn), Status: r.Status, TanksCenti: r.TanksCenti,
		RunSecondsTotal: r.RunSecondsTotal, SessionsTotal: r.SessionsTotal, Notes: r.Notes,
		CreatedAt: r.CreatedAt, Version: r.Version}
}

func tyreFromCreate(r sqlcgen.CreateTyreSetRow) domain.TyreSet {
	return tyreSet(r.ID, r.Label, r.Make, r.Model, r.Compound, r.InsertType, r.GluedOn, r.Status,
		r.SessionsTotal, r.RunSecondsTotal, r.WearNotes, r.CreatedAt, r.Version)
}
func tyreFromGet(r sqlcgen.GetTyreSetRow) domain.TyreSet {
	return tyreSet(r.ID, r.Label, r.Make, r.Model, r.Compound, r.InsertType, r.GluedOn, r.Status,
		r.SessionsTotal, r.RunSecondsTotal, r.WearNotes, r.CreatedAt, r.Version)
}
func tyreFromList(r sqlcgen.ListTyreSetsRow) domain.TyreSet {
	return tyreSet(r.ID, r.Label, r.Make, r.Model, r.Compound, r.InsertType, r.GluedOn, r.Status,
		r.SessionsTotal, r.RunSecondsTotal, r.WearNotes, r.CreatedAt, r.Version)
}
func tyreFromUpdate(r sqlcgen.UpdateTyreSetRow) domain.TyreSet {
	return tyreSet(r.ID, r.Label, r.Make, r.Model, r.Compound, r.InsertType, r.GluedOn, r.Status,
		r.SessionsTotal, r.RunSecondsTotal, r.WearNotes, r.CreatedAt, r.Version)
}
func tyreSet(id uuid.UUID, label, make, model string, compound, insert *string, glued string, status string,
	sessions, run int32, wear *string, created time.Time, version int32) domain.TyreSet {
	return domain.TyreSet{ID: id, Label: label, Make: make, Model: model, Compound: compound, InsertType: insert,
		GluedOn: ptrDate(glued), Status: status, SessionsTotal: sessions, RunSecondsTotal: run,
		WearNotes: wear, CreatedAt: created, Version: version}
}

func partFromCreate(r sqlcgen.CreatePartRow) domain.Part {
	return part(r.ID, r.Kind, r.Brand, r.Reference, r.Description, r.Unit, r.Specs, r.MinStockCenti,
		r.UnitCostCents, r.Currency, r.HasServiceLife, 0, false, r.CreatedAt, r.Version)
}
func partFromGet(r sqlcgen.GetPartRow) (domain.Part, error) {
	return partWithError(r.ID, r.Kind, r.Brand, r.Reference, r.Description, r.Unit, r.Specs, r.MinStockCenti,
		r.UnitCostCents, r.Currency, r.HasServiceLife, r.QtyCenti, r.BelowMin, r.CreatedAt, r.Version)
}
func partFromList(r sqlcgen.ListPartsRow) (domain.Part, error) {
	return partWithError(r.ID, r.Kind, r.Brand, r.Reference, r.Description, r.Unit, r.Specs, r.MinStockCenti,
		r.UnitCostCents, r.Currency, r.HasServiceLife, r.QtyCenti, r.BelowMin, r.CreatedAt, r.Version)
}
func partFromUpdate(r sqlcgen.UpdatePartRow) (domain.Part, error) {
	return partWithError(r.ID, r.Kind, r.Brand, r.Reference, r.Description, r.Unit, r.Specs, r.MinStockCenti,
		r.UnitCostCents, r.Currency, r.HasServiceLife, 0, false, r.CreatedAt, r.Version)
}
func part(id uuid.UUID, kind string, brand, ref *string, description, unit, specs string, min int64,
	cost pgtype.Int4, currency string, service bool, qty int64, below bool, created time.Time, version int32) domain.Part {
	p, _ := partWithError(id, kind, brand, ref, description, unit, specs, min, cost, currency, service, qty, below, created, version)
	return p
}
func partWithError(id uuid.UUID, kind string, brand, ref *string, description, unit, specs string, min int64,
	cost pgtype.Int4, currency string, service bool, qty int64, below bool, created time.Time, version int32) (domain.Part, error) {
	var values map[string]any
	if err := json.Unmarshal([]byte(specs), &values); err != nil {
		return domain.Part{}, fmt.Errorf("decode part specs: %w", err)
	}
	return domain.Part{ID: id, Kind: kind, Brand: brand, Reference: ref, Description: description, Unit: unit,
		Specs: values, MinStockCenti: min, UnitCostCents: ptrInt4(cost), Currency: currency,
		HasServiceLife: service, QtyCenti: qty, BelowMin: below, CreatedAt: created, Version: version}, nil
}

func movementFromCreate(r sqlcgen.CreateStockMovementRow) domain.StockMovement {
	return domain.StockMovement{ID: r.ID, PartID: r.PartID, QtyDeltaCenti: r.QtyDeltaCenti, Reason: r.Reason, Note: r.Note, OccurredAt: r.OccurredAt}
}
func movementFromList(r sqlcgen.ListStockMovementsRow) domain.StockMovement {
	return domain.StockMovement{ID: r.ID, PartID: r.PartID, QtyDeltaCenti: r.QtyDeltaCenti, Reason: r.Reason, Note: r.Note, OccurredAt: r.OccurredAt}
}

func mountFromCreate(r sqlcgen.CreateMountRow) domain.EngineMount {
	return domain.EngineMount{ID: r.ID, EngineID: r.EngineID, CarID: r.CarID, MountedAt: r.MountedAt, UnmountedAt: r.UnmountedAt}
}
func mountFromEngine(r sqlcgen.GetOpenMountForEngineRow) domain.EngineMount {
	return domain.EngineMount{ID: r.ID, EngineID: r.EngineID, CarID: r.CarID, MountedAt: r.MountedAt, UnmountedAt: r.UnmountedAt}
}
func mountFromCar(r sqlcgen.GetOpenMountForCarRow) domain.EngineMount {
	return domain.EngineMount{ID: r.ID, EngineID: r.EngineID, CarID: r.CarID, MountedAt: r.MountedAt, UnmountedAt: r.UnmountedAt}
}
func mountFromList(r sqlcgen.ListMountsForEngineRow) domain.EngineMount {
	return domain.EngineMount{ID: r.ID, EngineID: r.EngineID, CarID: r.CarID, MountedAt: r.MountedAt, UnmountedAt: r.UnmountedAt}
}

func mapError(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			switch pgErr.ConstraintName {
			case "car_transponder_uq", "tyre_set_label_uq":
				return fmt.Errorf("%s: %w", op, domain.ErrAlreadyExists)
			}
		case "23503":
			return fmt.Errorf("%s: %w", op, domain.ErrInvalid)
		case "23P01":
			return fmt.Errorf("%s: %w", op, domain.ErrMountConflict)
		case "23514":
			if pgErr.ConstraintName == "engine_mount_check" {
				return fmt.Errorf("%s: %w", op, domain.ErrMountConflict)
			}
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}
