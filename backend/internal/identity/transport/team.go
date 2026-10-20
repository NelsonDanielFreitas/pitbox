package transport

import (
	"context"
	"fmt"

	"connectrpc.com/connect/v2"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1/identityv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Teams implements the Connect TeamService.
type Teams struct {
	identityv1connect.UnimplementedTeamServiceHandler
	svc *app.Service
}

// NewTeams returns the TeamService handler.
func NewTeams(svc *app.Service) *Teams { return &Teams{svc: svc} }

func caller(ctx context.Context) (db.UserID, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return db.UserID{}, connect.NewError(connect.CodeUnauthenticated, "unauthenticated")
	}
	user, err := db.ParseUserID(p.UserID)
	if err != nil {
		return db.UserID{}, fmt.Errorf("session user: %w", err)
	}
	return user, nil
}

func (h *Teams) CreateTeam(ctx context.Context, req *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID("request_id", req.GetRequestId())
	if err != nil {
		return nil, err
	}
	team, err := h.svc.CreateTeam(ctx, user, app.CreateTeamInput{
		RequestID: requestID, Name: req.GetName(), Category: req.GetCategory(),
		Club: req.Club, CountryCode: req.GetCountryCode(),
	})
	if err != nil {
		return nil, err
	}
	return &identityv1.CreateTeamResponse{Team: teamToProto(team)}, nil
}

func (h *Teams) ListMyTeams(ctx context.Context, req *identityv1.ListMyTeamsRequest) (*identityv1.ListMyTeamsResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	teams, next, err := h.svc.ListMyTeams(ctx, user, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &identityv1.ListMyTeamsResponse{NextPageToken: next, Teams: make([]*identityv1.TeamMembership, 0, len(teams))}
	for _, t := range teams {
		resp.Teams = append(resp.Teams, membershipToProto(t))
	}
	return resp, nil
}

func (h *Teams) GetTeam(ctx context.Context, req *identityv1.GetTeamRequest) (*identityv1.GetTeamResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	teamID, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	team, err := h.svc.GetTeam(ctx, user, teamID)
	if err != nil {
		return nil, err
	}
	return &identityv1.GetTeamResponse{Team: teamToProto(team)}, nil
}

func (h *Teams) UpdateTeam(ctx context.Context, req *identityv1.UpdateTeamRequest) (*identityv1.UpdateTeamResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	teamID, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID("request_id", req.GetRequestId())
	if err != nil {
		return nil, err
	}
	update, err := teamUpdateFromProto(req)
	if err != nil {
		return nil, err
	}
	team, err := h.svc.UpdateTeam(ctx, user, teamID, app.UpdateTeamInput{
		RequestID: requestID, Version: req.GetVersion(), Update: update,
	})
	if err != nil {
		return nil, err
	}
	return &identityv1.UpdateTeamResponse{Team: teamToProto(team)}, nil
}
