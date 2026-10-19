package identity

import (
	"log/slog"

	"connectrpc.com/connect/v2"

	identityv1connect "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1/identityv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/transport"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Deps is what the identity module needs from the composition root.
type Deps struct {
	DB    *db.DB
	Authz app.Authorizer
	Clock clock.Clock
	Log   *slog.Logger
}

// Module registers the identity services.
type Module struct {
	teams   *transport.Teams
	members *transport.Members
}

// New builds the identity module. The fake mailer is replaced by the real
// provider in F5.
func New(d Deps) (*Module, error) {
	svc, err := app.New(app.Deps{
		DB: d.DB, Store: store.NewRepo(), Authz: d.Authz, Clock: d.Clock, Log: d.Log,
		Mailer: app.NoopMailer{},
	})
	if err != nil {
		return nil, err
	}
	return &Module{teams: transport.NewTeams(svc), members: transport.NewMembers(svc)}, nil
}

// Register mounts TeamService and MemberService on the Connect server.
func (m *Module) Register(server *connect.Server) {
	identityv1connect.RegisterTeamServiceHandler(server, m.teams)
	identityv1connect.RegisterMemberServiceHandler(server, m.members)
}
