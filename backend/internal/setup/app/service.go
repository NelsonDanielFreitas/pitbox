package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/idempotency"
)

type Deps struct {
	DB          *db.DB
	Store       Store
	Authz       Authorizer
	Clock       clock.Clock
	Log         *slog.Logger
	Idempotency *idempotency.Store
	Garage      Garage
	Shares      ShareReader
}

type Service struct {
	db          *db.DB
	store       Store
	authz       Authorizer
	clock       clock.Clock
	log         *slog.Logger
	idempotency *idempotency.Store
	garage      Garage
	shares      ShareReader
}

func New(d Deps) (*Service, error) {
	if d.DB == nil || d.Store == nil || d.Authz == nil || d.Clock == nil || d.Log == nil ||
		d.Idempotency == nil || d.Garage == nil || d.Shares == nil {
		return nil, errors.New("setup: missing service dependency")
	}
	return &Service{
		db: d.DB, store: d.Store, authz: d.Authz, clock: d.Clock, log: d.Log,
		idempotency: d.Idempotency, garage: d.Garage, shares: d.Shares,
	}, nil
}

func pageSize(n int32) int32 {
	if n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}

func (s *Service) audit(ctx context.Context, tx db.DBTX, action, objectType, objectID string, before, after any, requestID string) error {
	return audit.RecordTeam(ctx, tx, audit.TeamEvent{
		Action: action, ObjectType: objectType, ObjectID: objectID,
		Before: before, After: after, RequestID: requestID,
	})
}
