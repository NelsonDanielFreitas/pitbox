package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
)

const (
	// maxDevicesPerUser bounds the keys one user can register in a team.
	maxDevicesPerUser = 10
	// clockSkew is how far a device clock may be ahead of the server.
	clockSkew = 5 * time.Minute
	// maxPullLimit is the largest page of a Pull (ADR 0003).
	maxPullLimit = 500
	// maxOpRetries is how many times an operation is retried after losing a race.
	maxOpRetries = 3
)

// CatalogSource gives the global catalogues and their hash.
type CatalogSource interface {
	Load(ctx context.Context) (version string, snapshot json.RawMessage, err error)
}

type Deps struct {
	DB           *db.DB
	Store        Store
	Authz        Authorizer
	Clock        clock.Clock
	Log          *slog.Logger
	Appliers     syncapply.Registry
	PassSigner   domain.PassSigner
	PassVerifier domain.PassVerifier
	Catalog      CatalogSource
}

type Service struct {
	db       *db.DB
	store    Store
	authz    Authorizer
	clock    clock.Clock
	log      *slog.Logger
	appliers syncapply.Registry
	signer   domain.PassSigner
	verifier domain.PassVerifier
	catalog  CatalogSource
}

func New(d Deps) (*Service, error) {
	if d.DB == nil || d.Store == nil || d.Authz == nil || d.Clock == nil || d.Log == nil ||
		d.Appliers == nil || d.Catalog == nil || d.PassSigner.KeyID == "" || len(d.PassVerifier) == 0 {
		return nil, errors.New("sync: missing service dependency")
	}
	return &Service{
		db: d.DB, store: d.Store, authz: d.Authz, clock: d.Clock, log: d.Log, appliers: d.Appliers,
		signer: d.PassSigner, verifier: d.PassVerifier, catalog: d.Catalog,
	}, nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC() }

func audited(ctx context.Context, tx db.DBTX, action, objectType, objectID string, after any, requestID string) error {
	return audit.RecordTeam(ctx, tx, audit.TeamEvent{
		Action: action, ObjectType: objectType, ObjectID: objectID, After: after, RequestID: requestID,
	})
}
