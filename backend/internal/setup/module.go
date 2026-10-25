package setup

import (
	"log/slog"

	"connectrpc.com/connect/v2"

	setupv1connect "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/setup/v1/setupv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/idempotency"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/transport"
)

type Deps struct {
	DB     *db.DB
	Authz  *authz.Client
	Clock  clock.Clock
	Log    *slog.Logger
	Shares store.Querier
	Garage app.Garage
}

type Module struct {
	setup  *transport.Setup
	shared *transport.Shared
}

func New(d Deps) (*Module, error) {
	svc, err := app.New(app.Deps{
		DB: d.DB, Store: store.NewRepo(), Authz: d.Authz, Clock: d.Clock, Log: d.Log,
		Idempotency: idempotency.NewStore(), Garage: d.Garage,
		Shares: store.NewShareReader(d.Shares),
	})
	if err != nil {
		return nil, err
	}
	return &Module{setup: transport.NewSetup(svc), shared: transport.NewShared(svc)}, nil
}

func (m *Module) Register(server *connect.Server) {
	setupv1connect.RegisterSetupServiceHandler(server, m.setup)
	setupv1connect.RegisterSharedSetupServiceHandler(server, m.shared)
}
