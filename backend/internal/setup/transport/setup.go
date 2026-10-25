package transport

import (
	"context"
	"errors"

	"github.com/google/uuid"

	setupv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/setup/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/setup/v1/setupv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// Setup implements the team-facing SetupService.
type Setup struct {
	setupv1connect.UnimplementedSetupServiceHandler
	svc *app.Service
}

func NewSetup(svc *app.Service) *Setup { return &Setup{svc: svc} }

func (h *Setup) CreateSetup(ctx context.Context, req *setupv1.CreateSetupRequest) (*setupv1.CreateSetupResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	carID, err := parseUUID(req.GetCarId())
	if err != nil {
		return nil, err
	}
	in := app.CreateSetupInput{RequestID: requestID, CarID: carID, Name: req.GetName(), Notes: req.Notes}
	if req.SourceSetupId != nil {
		id, err := parseUUID(req.GetSourceSetupId())
		if err != nil {
			return nil, err
		}
		in.SourceSetupID = &id
	}
	in.SourceVersion = req.SourceVersion
	created, err := h.svc.CreateSetup(ctx, user, team, in)
	if err != nil {
		return nil, setupError(err)
	}
	return &setupv1.CreateSetupResponse{Setup: setupToProto(created)}, nil
}

func (h *Setup) CreateVersion(ctx context.Context, req *setupv1.CreateVersionRequest) (*setupv1.CreateVersionResponse, error) {
	user, team, setupID, err := callerTeamID(ctx, req.GetTeamId(), req.GetSetupId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	data, err := structToMap(req.GetData())
	if err != nil {
		return nil, setupError(err)
	}
	ref, err := h.svc.CreateVersion(ctx, user, team, app.CreateVersionInput{
		RequestID: requestID, SetupID: setupID, BaseVersion: req.GetBaseVersion(),
		Data: data, ChangeSummary: req.ChangeSummary,
	})
	if err != nil {
		return nil, setupError(err)
	}
	return &setupv1.CreateVersionResponse{Version: versionRefToProto(ref)}, nil
}

func (h *Setup) GetVersion(ctx context.Context, req *setupv1.GetVersionRequest) (*setupv1.GetVersionResponse, error) {
	user, team, setupID, err := callerTeamID(ctx, req.GetTeamId(), req.GetSetupId())
	if err != nil {
		return nil, err
	}
	version, err := h.svc.GetVersion(ctx, user, team, setupID, req.Version)
	if err != nil {
		return nil, setupError(err)
	}
	out, err := versionToProto(version)
	if err != nil {
		return nil, err
	}
	return &setupv1.GetVersionResponse{Version: out}, nil
}

func (h *Setup) ListSetups(ctx context.Context, req *setupv1.ListSetupsRequest) (*setupv1.ListSetupsResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	team, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	var carID *uuid.UUID
	if req.CarId != nil {
		id, err := parseUUID(req.GetCarId())
		if err != nil {
			return nil, err
		}
		carID = &id
	}
	setups, next, err := h.svc.ListSetups(ctx, user, team, carID, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, setupError(err)
	}
	resp := &setupv1.ListSetupsResponse{NextPageToken: next, Setups: make([]*setupv1.Setup, 0, len(setups))}
	for _, s := range setups {
		resp.Setups = append(resp.Setups, setupToProto(s))
	}
	return resp, nil
}

func (h *Setup) ListVersions(ctx context.Context, req *setupv1.ListVersionsRequest) (*setupv1.ListVersionsResponse, error) {
	user, team, setupID, err := callerTeamID(ctx, req.GetTeamId(), req.GetSetupId())
	if err != nil {
		return nil, err
	}
	versions, next, err := h.svc.ListVersions(ctx, user, team, setupID, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, setupError(err)
	}
	resp := &setupv1.ListVersionsResponse{NextPageToken: next, Versions: make([]*setupv1.SetupVersionRef, 0, len(versions))}
	for _, v := range versions {
		resp.Versions = append(resp.Versions, versionRefToProto(v))
	}
	return resp, nil
}

func (h *Setup) CompareVersions(ctx context.Context, req *setupv1.CompareVersionsRequest) (*setupv1.CompareVersionsResponse, error) {
	user, team, setupID, err := callerTeamID(ctx, req.GetTeamId(), req.GetSetupId())
	if err != nil {
		return nil, err
	}
	diffs, err := h.svc.CompareVersions(ctx, user, team, setupID, req.GetFromVersion(), req.GetToVersion())
	if err != nil {
		return nil, setupError(err)
	}
	resp := &setupv1.CompareVersionsResponse{
		FromVersion: req.GetFromVersion(), ToVersion: req.GetToVersion(),
		Differences: make([]*setupv1.SetupDifference, 0, len(diffs)),
	}
	for _, d := range diffs {
		out, err := differenceToProto(d)
		if err != nil {
			return nil, err
		}
		resp.Differences = append(resp.Differences, out)
	}
	return resp, nil
}

func (h *Setup) CreateShareLink(ctx context.Context, req *setupv1.CreateShareLinkRequest) (*setupv1.CreateShareLinkResponse, error) {
	user, team, setupID, err := callerTeamID(ctx, req.GetTeamId(), req.GetSetupId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	expiry, err := expiryFromProto(req.GetExpiresIn())
	if err != nil {
		return nil, err
	}
	link, token, err := h.svc.CreateShareLink(ctx, user, team, app.CreateShareLinkInput{
		RequestID: requestID, SetupID: setupID, Expiry: expiry, PinnedVersion: req.PinnedVersion,
	})
	if err != nil {
		return nil, setupError(err)
	}
	return &setupv1.CreateShareLinkResponse{
		ShareLink: &setupv1.ShareLinkWithToken{ShareLink: shareLinkToProto(link), Token: token},
	}, nil
}

func (h *Setup) ListShareLinks(ctx context.Context, req *setupv1.ListShareLinksRequest) (*setupv1.ListShareLinksResponse, error) {
	user, team, setupID, err := callerTeamID(ctx, req.GetTeamId(), req.GetSetupId())
	if err != nil {
		return nil, err
	}
	links, err := h.svc.ListShareLinks(ctx, user, team, setupID)
	if err != nil {
		return nil, setupError(err)
	}
	resp := &setupv1.ListShareLinksResponse{ShareLinks: make([]*setupv1.ShareLink, 0, len(links))}
	for _, l := range links {
		resp.ShareLinks = append(resp.ShareLinks, shareLinkToProto(l))
	}
	return resp, nil
}

func (h *Setup) RevokeShareLink(ctx context.Context, req *setupv1.RevokeShareLinkRequest) (*setupv1.RevokeShareLinkResponse, error) {
	user, team, setupID, err := callerTeamID(ctx, req.GetTeamId(), req.GetSetupId())
	if err != nil {
		return nil, err
	}
	linkID, err := parseUUID(req.GetShareLinkId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.RevokeShareLink(ctx, user, team, setupID, linkID, requestID); err != nil {
		return nil, setupError(err)
	}
	return &setupv1.RevokeShareLinkResponse{}, nil
}

func expiryFromProto(v setupv1.ShareLinkExpiry) (domain.ShareExpiry, error) {
	switch v {
	case setupv1.ShareLinkExpiry_SHARE_LINK_EXPIRY_24H:
		return domain.ShareExpiry24h, nil
	case setupv1.ShareLinkExpiry_SHARE_LINK_EXPIRY_7D:
		return domain.ShareExpiry7d, nil
	case setupv1.ShareLinkExpiry_SHARE_LINK_EXPIRY_NEVER:
		return domain.ShareExpiryNever, nil
	default:
		return "", errors.New("invalid share link expiry")
	}
}
