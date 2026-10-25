package transport

import (
	"context"

	"connectrpc.com/connect/v2"

	setupv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/setup/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/setup/v1/setupv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// SharedService is the public use case used by the transport.
type SharedService interface {
	OpenShareLink(ctx context.Context, token string) (domain.SharedSetup, error)
}

// Shared implements the public SharedSetupService.
type Shared struct {
	setupv1connect.UnimplementedSharedSetupServiceHandler
	svc SharedService
}

func NewShared(svc SharedService) *Shared { return &Shared{svc: svc} }

func (h *Shared) GetSharedSetup(ctx context.Context, req *setupv1.GetSharedSetupRequest) (*setupv1.GetSharedSetupResponse, error) {
	if info, ok := connect.CallInfoForServerContext(ctx); ok {
		info.ResponseHeader().Set("Cache-Control", "no-store")
	}
	setup, err := h.svc.OpenShareLink(ctx, req.GetToken())
	if err != nil {
		return nil, setupError(err)
	}
	out, err := sharedSetupToProto(setup)
	if err != nil {
		return nil, err
	}
	return &setupv1.GetSharedSetupResponse{Setup: out}, nil
}
