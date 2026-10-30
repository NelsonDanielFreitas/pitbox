package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
)

type PullInput struct {
	Cursor         string
	Limit          int32
	CatalogVersion string
}

// PullChange is one entity to upsert on the device, or a tombstone.
type PullChange struct {
	Entity   string
	EntityID uuid.UUID
	Deleted  bool
	Data     json.RawMessage
}

type PullOutput struct {
	Changes         []PullChange
	NextCursor      string
	HasMore         bool
	CatalogVersion  string
	CatalogSnapshot json.RawMessage // empty unless the device's catalogue is out of date
}

// Pull returns what changed after the cursor, read again from the current rows and filtered
// for the caller's role (the change log only names the entity). Deletions travel as
// tombstones. Entities without an Applier are not syncable and are skipped, but the cursor
// still moves past them.
func (s *Service) Pull(ctx context.Context, user db.UserID, team db.TeamID, in PullInput) (PullOutput, error) {
	after, err := domain.ParseCursor(in.Cursor)
	if err != nil {
		return PullOutput{}, fmt.Errorf("cursor: %w", err)
	}
	limit := in.Limit
	if limit <= 0 || limit > maxPullLimit {
		limit = maxPullLimit
	}

	out := PullOutput{NextCursor: after.String()}
	if in.Cursor == "" {
		out.NextCursor = ""
	}
	err = s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, role string) error {
		rows, err := s.store.Changes(ctx, tx, uuid.UUID(team), after, limit)
		if err != nil {
			return err
		}
		type key struct {
			entity string
			id     uuid.UUID
		}
		// One entry per entity, in the order of its last change: the device only needs the
		// current state, not every step.
		last := map[key]int{}
		for i, r := range rows {
			last[key{r.Entity, r.EntityID}] = i
		}
		for i, r := range rows {
			out.NextCursor = r.Cursor.String()
			if last[key{r.Entity, r.EntityID}] != i {
				continue
			}
			a, ok := s.appliers[r.Entity]
			if !ok {
				continue
			}
			ch := PullChange{Entity: r.Entity, EntityID: r.EntityID, Deleted: r.Deleted}
			if !r.Deleted {
				data, found, err := a.Read(ctx, tx, team, role, r.EntityID)
				if err != nil {
					return fmt.Errorf("read %s: %w", r.Entity, err)
				}
				ch.Data, ch.Deleted = data, !found
			}
			out.Changes = append(out.Changes, ch)
		}
		out.HasMore = int32(len(rows)) == limit //nolint:gosec // len(rows) <= limit <= 500
		return nil
	})
	if err != nil {
		return PullOutput{}, err
	}

	version, snapshot, err := s.catalog.Load(ctx)
	if err != nil {
		return PullOutput{}, fmt.Errorf("load catalogue: %w", err)
	}
	out.CatalogVersion = version
	if in.CatalogVersion != version {
		out.CatalogSnapshot = snapshot
	}
	return out, nil
}
