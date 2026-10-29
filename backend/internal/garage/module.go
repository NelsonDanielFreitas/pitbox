package garage

import (
	"log/slog"

	"connectrpc.com/connect/v2"

	garagev1connect "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1/garagev1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/transport"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/idempotency"
)

type Deps struct {
	DB    *db.DB
	Authz *authz.Client
	Clock clock.Clock
	Log   *slog.Logger
}

type Module struct {
	cars    *transport.Cars
	engines *transport.Engines
	tyres   *transport.Tyres
	parts   *transport.Parts
	shop    *transport.Shopping
}

func New(d Deps) (*Module, error) {
	svc, err := app.New(app.Deps{
		DB: d.DB, Store: store.NewRepo(), Authz: d.Authz, Clock: d.Clock, Log: d.Log,
		Idempotency: idempotency.NewStore(),
	})
	if err != nil {
		return nil, err
	}
	return &Module{
		cars: transport.NewCars(svc), engines: transport.NewEngines(svc),
		tyres: transport.NewTyres(svc), parts: transport.NewParts(svc), shop: transport.NewShopping(svc),
	}, nil
}

func (m *Module) Register(server *connect.Server) {
	garagev1connect.RegisterCarServiceHandler(server, m.cars)
	garagev1connect.RegisterEngineServiceHandler(server, m.engines)
	garagev1connect.RegisterTyreServiceHandler(server, m.tyres)
	garagev1connect.RegisterPartServiceHandler(server, m.parts)
	garagev1connect.RegisterShoppingServiceHandler(server, m.shop)
}
