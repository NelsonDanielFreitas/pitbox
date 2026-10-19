package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Deps is what the identity service needs. They are interfaces (or the db
// transaction helper) so tests can use fakes.
type Deps struct {
	DB     *db.DB
	Store  Store
	Authz  Authorizer
	Clock  clock.Clock
	Log    *slog.Logger
	Mailer Mailer
}

// Service implements the F1-06 use cases.
type Service struct {
	db     *db.DB
	store  Store
	authz  Authorizer
	clock  clock.Clock
	log    *slog.Logger
	mailer Mailer
}

// New validates the dependencies and returns the service.
func New(d Deps) (*Service, error) {
	if d.DB == nil || d.Store == nil || d.Authz == nil || d.Clock == nil || d.Log == nil || d.Mailer == nil {
		return nil, errors.New("identity: missing service dependency")
	}
	return &Service{db: d.DB, store: d.Store, authz: d.Authz, clock: d.Clock, log: d.Log, mailer: d.Mailer}, nil
}

// syncRole sets the tuple after the transaction commits. The outbox is the
// guarantee; this only removes the window in which the owner would get
// NOT_FOUND right after creating a team or accepting an invitation. It never
// fails the request, and the log carries no ids.
func (s *Service) syncRole(ctx context.Context, user, teamID string, role *domain.Role) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	var want *string
	if role != nil {
		v := string(*role)
		want = &v
	}
	if err := s.authz.SetTeamRole(ctx, "user:"+user, "team:"+teamID, want); err != nil {
		s.log.WarnContext(ctx, "outbox tuple sync failed", slog.String("reason", "authorization unavailable"))
	}
}

// Mailer is called after the invitation transaction commits; a failure leaves
// the invitation valid and the owner still has the token.
func (s *Service) sendInvitation(ctx context.Context, to, token string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.mailer.Invitation(ctx, to, token); err != nil {
		s.log.WarnContext(ctx, "invitation email failed", slog.String("reason", "mailer unavailable"))
	}
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
