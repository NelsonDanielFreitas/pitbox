package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store/sqlcgen"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// --- shopping list -----------------------------------------------------------

func (Repo) ListShoppingList(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.ShoppingLine, error) {
	rows, err := sqlcgen.New(tx).ListShoppingList(ctx, sqlcgen.ListShoppingListParams{TeamID: team, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list shopping list: %w", err)
	}
	out := make([]domain.ShoppingLine, 0, len(rows))
	for _, r := range rows {
		suggested := domain.SuggestedQty(r.QtyCenti, r.MinStockCenti, optionalCenti(r.ReorderToCenti))
		line := domain.ShoppingLine{
			PartID: r.PartID, SuggestedQtyCenti: suggested, BelowMin: r.BelowMin, Note: r.ItemNote,
			ItemVersion: r.ItemVersion, UnitCostCents: ptrInt4(r.UnitCostCents), QtyCenti: suggested,
		}
		if q := optionalCenti(r.ItemQtyCenti); q != nil {
			line.QtyCenti = *q
		}
		out = append(out, line)
	}
	return out, nil
}

func (Repo) GetShoppingItemForUpdate(ctx context.Context, tx db.DBTX, team, part uuid.UUID) (domain.ShoppingItem, bool, error) {
	row, err := sqlcgen.New(tx).GetShoppingItemForUpdate(ctx, sqlcgen.GetShoppingItemForUpdateParams{TeamID: team, PartID: part})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ShoppingItem{}, false, nil
	}
	if err != nil {
		return domain.ShoppingItem{}, false, fmt.Errorf("get shopping item: %w", err)
	}
	return domain.ShoppingItem{ID: row.ID, PartID: row.PartID, QtyCenti: optionalCenti(row.QtyCenti),
		Dismissed: row.Dismissed, Note: row.Note, Version: row.Version}, true, nil
}

func (Repo) CreateShoppingItem(ctx context.Context, tx db.DBTX, team, user uuid.UUID, item domain.ShoppingItem) (domain.ShoppingItem, error) {
	row, err := sqlcgen.New(tx).CreateShoppingItem(ctx, sqlcgen.CreateShoppingItemParams{
		TeamID: team, PartID: item.PartID, Dismissed: item.Dismissed, CreatedBy: &user,
		QtyCenti: int8p(item.QtyCenti), Note: item.Note,
	})
	if err != nil {
		return domain.ShoppingItem{}, mapError("create shopping item", err)
	}
	return domain.ShoppingItem{ID: row.ID, PartID: row.PartID, QtyCenti: optionalCenti(row.QtyCenti),
		Dismissed: row.Dismissed, Note: row.Note, Version: row.Version}, nil
}

func (Repo) UpdateShoppingItem(ctx context.Context, tx db.DBTX, team uuid.UUID, item domain.ShoppingItem) (domain.ShoppingItem, error) {
	row, err := sqlcgen.New(tx).UpdateShoppingItem(ctx, sqlcgen.UpdateShoppingItemParams{
		TeamID: team, PartID: item.PartID, Dismissed: item.Dismissed, Version: item.Version,
		QtyCenti: int8p(item.QtyCenti), Note: item.Note,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ShoppingItem{}, domain.ErrConflict
	}
	if err != nil {
		return domain.ShoppingItem{}, mapError("update shopping item", err)
	}
	return domain.ShoppingItem{ID: row.ID, PartID: row.PartID, QtyCenti: optionalCenti(row.QtyCenti),
		Dismissed: row.Dismissed, Note: row.Note, Version: row.Version}, nil
}

func (Repo) DeleteShoppingItemForPart(ctx context.Context, tx db.DBTX, team, part uuid.UUID) (bool, error) {
	n, err := sqlcgen.New(tx).DeleteShoppingItemForPart(ctx, sqlcgen.DeleteShoppingItemForPartParams{TeamID: team, PartID: part})
	if err != nil {
		return false, fmt.Errorf("delete shopping item: %w", err)
	}
	return n > 0, nil
}

func (Repo) ClearDismissedShoppingItem(ctx context.Context, tx db.DBTX, team, part uuid.UUID) error {
	if _, err := sqlcgen.New(tx).ClearDismissedShoppingItem(ctx, sqlcgen.ClearDismissedShoppingItemParams{TeamID: team, PartID: part}); err != nil {
		return fmt.Errorf("clear dismissed shopping item: %w", err)
	}
	return nil
}

// --- synced shopping lines (addressed by their own id, F1-13) ----------------------------

func shoppingItemFrom(id, part uuid.UUID, qty int64, dismissed bool, note *string, version int32) domain.ShoppingItem {
	return domain.ShoppingItem{ID: id, PartID: part, QtyCenti: optionalCenti(qty), Dismissed: dismissed, Note: note, Version: version}
}

func (Repo) GetShoppingItemByID(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.ShoppingItem, error) {
	row, err := sqlcgen.New(tx).GetShoppingItemByID(ctx, sqlcgen.GetShoppingItemByIDParams{TeamID: team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ShoppingItem{}, fmt.Errorf("get shopping item: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.ShoppingItem{}, fmt.Errorf("get shopping item: %w", err)
	}
	return shoppingItemFrom(row.ID, row.PartID, row.QtyCenti, row.Dismissed, row.Note, row.Version), nil
}

// CreateShoppingItemWithID: the device chose the id. A second line for the same part hits
// shopping_item_team_id_part_id_key and is reported as ErrAlreadyExists.
func (Repo) CreateShoppingItemWithID(ctx context.Context, tx db.DBTX, team, user, id uuid.UUID, item domain.ShoppingItem) (domain.ShoppingItem, error) {
	row, err := sqlcgen.New(tx).CreateShoppingItemWithID(ctx, sqlcgen.CreateShoppingItemWithIDParams{
		ID: id, TeamID: team, PartID: item.PartID, Dismissed: item.Dismissed, CreatedBy: &user,
		QtyCenti: int8p(item.QtyCenti), Note: item.Note,
	})
	if err != nil {
		return domain.ShoppingItem{}, mapError("create shopping item", err)
	}
	return shoppingItemFrom(row.ID, row.PartID, row.QtyCenti, row.Dismissed, row.Note, row.Version), nil
}

func (Repo) UpdateShoppingItemByID(ctx context.Context, tx db.DBTX, team uuid.UUID, item domain.ShoppingItem) (domain.ShoppingItem, error) {
	row, err := sqlcgen.New(tx).UpdateShoppingItemByID(ctx, sqlcgen.UpdateShoppingItemByIDParams{
		TeamID: team, ID: item.ID, Dismissed: item.Dismissed, Version: item.Version,
		QtyCenti: int8p(item.QtyCenti), Note: item.Note,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ShoppingItem{}, domain.ErrConflict
	}
	if err != nil {
		return domain.ShoppingItem{}, mapError("update shopping item", err)
	}
	return shoppingItemFrom(row.ID, row.PartID, row.QtyCenti, row.Dismissed, row.Note, row.Version), nil
}

func (Repo) DeleteShoppingItemByID(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) error {
	n, err := sqlcgen.New(tx).DeleteShoppingItemByID(ctx, sqlcgen.DeleteShoppingItemByIDParams{TeamID: team, ID: id, Version: version})
	if err != nil {
		return fmt.Errorf("delete shopping item: %w", err)
	}
	if n == 0 {
		return domain.ErrConflict
	}
	return nil
}
