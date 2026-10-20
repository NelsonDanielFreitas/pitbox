package bootstrap

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/grpcreflect/v2"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/httpserver"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
)

// requestMessages lists the request type of every RPC. The validator is built from
// this list and refuses any type that is not in it, so a service added without its
// types here fails the test (TestRequestMessagesCoverEveryService) and, at runtime,
// every call to it.
func requestMessages() []proto.Message {
	return []proto.Message{
		&identityv1.CreateTeamRequest{},
		&identityv1.ListMyTeamsRequest{},
		&identityv1.GetTeamRequest{},
		&identityv1.UpdateTeamRequest{},
		&identityv1.ListMembersRequest{},
		&identityv1.ListInvitationsRequest{},
		&identityv1.InviteMemberRequest{},
		&identityv1.AcceptInvitationRequest{},
		&identityv1.ChangeMemberRoleRequest{},
		&identityv1.RemoveMemberRequest{},
		&identityv1.LeaveTeamRequest{},
		&identityv1.RevokeInvitationRequest{},
		&identityv1.InitiateTransferRequest{},
		&identityv1.AcceptTransferRequest{},
		&identityv1.CancelTransferRequest{},
	}
}

// newRPCServer builds the Connect server with the interceptor chain and lets the caller
// register the services. No service is registered in production yet: the modules bring
// theirs (F1-06 onwards).
func newRPCServer(cfg config.Config, logger *slog.Logger, tp trace.TracerProvider, authn, rateLimit, stepUp connect.ServerInterceptor, authzClient *authz.Client, register ...func(*connect.Server)) (*connect.Server, error) {
	// authzClient is reserved for service constructors from F1-06 onwards.
	_ = authzClient
	validator, err := interceptors.NewValidator(requestMessages()...)
	if err != nil {
		return nil, err
	}
	chain, err := interceptors.Server(interceptors.Options{
		Logger:            logger,
		TracerProvider:    tp,
		Validator:         validator,
		MaxUnaryTimeout:   cfg.RPCMaxTimeout,
		MaxStreamDuration: cfg.RPCStreamMaxDuration,
		ClientIPHeader:    cfg.ClientIPHeader(),
		Auth:              authn,
		RateLimit:         rateLimit,
		StepUp:            stepUp,
	})
	if err != nil {
		return nil, fmt.Errorf("build interceptor chain: %w", err)
	}
	server := connect.NewServer(chain...)
	for _, r := range register {
		r(server)
	}
	return server, nil
}

// newReflectionServer returns the gRPC reflection server, or nil outside local
// development: reflection serves every protobuf type compiled into the binary, so it
// is never mounted in preview, staging or production (architecture §12.1). It has its
// own minimal chain: it carries no PitBox requests, so there is nothing to validate or
// audit, and the PitBox validator would refuse its message types.
func newReflectionServer(cfg config.Config, logger *slog.Logger, api *connect.Server) *connect.Server {
	if cfg.Env != config.EnvLocal {
		return nil
	}
	server := connect.NewServer(interceptors.Recovery(logger), interceptors.Errors(logger))
	grpcreflect.Register(server, grpcreflect.WithNamer(grpcreflect.NamerFunc(func() []string {
		return serviceNames(api)
	})))
	return server
}

// serviceNames returns the fully-qualified service names ("pkg.Service") of a server.
func serviceNames(server *connect.Server) []string {
	var names []string
	for _, prefix := range httpserver.ServicePrefixes(server) {
		names = append(names, strings.Trim(prefix, "/"))
	}
	return names
}

// mountRPC mounts the API server (and reflection, when present) and returns the filter
// that keeps their routes out of the HTTP trace middleware.
func mountRPC(mux *http.ServeMux, cfg config.Config, api, reflection *connect.Server) error {
	o := httpserver.RPCOptions{MaxUnaryTimeout: cfg.RPCMaxTimeout, MaxStreamDuration: cfg.RPCStreamMaxDuration}
	if err := httpserver.MountRPC(mux, api, o); err != nil {
		return err
	}
	if reflection != nil {
		return httpserver.MountRPC(mux, reflection, o)
	}
	return nil
}

// rpcPaths reports whether a request is for a Connect procedure.
func rpcPaths(servers ...*connect.Server) func(*http.Request) bool {
	var prefixes []string
	for _, s := range servers {
		if s != nil {
			prefixes = append(prefixes, httpserver.ServicePrefixes(s)...)
		}
	}
	return func(r *http.Request) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(r.URL.Path, p) {
				return true
			}
		}
		return false
	}
}
