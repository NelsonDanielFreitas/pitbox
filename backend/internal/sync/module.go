// Package sync is the synchronisation module: it takes the signed operations a device queued
// offline and applies them with the owners of the data, and hands changes back to the device.
// Other modules use only api.go.
package sync

import (
	"crypto/ed25519"
	"errors"
	"log/slog"

	"connectrpc.com/connect/v2"

	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/sync/v1/syncv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/transport"
)

type Deps struct {
	DB    *db.DB
	Authz *authz.Client
	Clock clock.Clock
	Log   *slog.Logger
	// Appliers are the entities that can be synchronised: each owning module provides its own.
	Appliers syncapply.Registry
	// PassKeys are Ed25519 seeds (32 bytes). The first signs offline passes; all of them verify.
	PassKeys []PassKey
	Catalog  CatalogSource
}

// PassKey is one offline pass key: an id (the "kid" of the token) and an Ed25519 seed.
type PassKey struct {
	ID   string
	Seed []byte
}

// CatalogSource gives the global catalogues, their hash and a snapshot for the Pull.
type CatalogSource = app.CatalogSource

type Module struct{ handler *transport.Handler }

func New(d Deps) (*Module, error) {
	if len(d.PassKeys) == 0 {
		return nil, errors.New("sync: at least one offline pass key is required")
	}
	verifier := domain.PassVerifier{}
	for _, k := range d.PassKeys {
		if len(k.Seed) != ed25519.SeedSize {
			return nil, errors.New("sync: an offline pass key is not a 32-byte seed")
		}
		verifier[k.ID] = ed25519.NewKeyFromSeed(k.Seed).Public().(ed25519.PublicKey) //nolint:forcetypeassert // always ed25519.PublicKey
	}
	signer := domain.PassSigner{KeyID: d.PassKeys[0].ID, Key: ed25519.NewKeyFromSeed(d.PassKeys[0].Seed)}
	svc, err := app.New(app.Deps{
		DB: d.DB, Store: store.NewRepo(), Authz: d.Authz, Clock: d.Clock, Log: d.Log,
		Appliers: d.Appliers, PassSigner: signer, PassVerifier: verifier, Catalog: d.Catalog,
	})
	if err != nil {
		return nil, err
	}
	return &Module{handler: transport.NewHandler(svc)}, nil
}

func (m *Module) Register(server *connect.Server) {
	syncv1connect.RegisterSyncServiceHandler(server, m.handler)
}
