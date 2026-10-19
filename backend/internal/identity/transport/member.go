package transport

import (
	"context"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1/identityv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Members implements the Connect MemberService.
type Members struct {
	identityv1connect.UnimplementedMemberServiceHandler
	svc *app.Service
}

// NewMembers returns the MemberService handler.
func NewMembers(svc *app.Service) *Members { return &Members{svc: svc} }

func (h *Members) ListMembers(ctx context.Context, req *identityv1.ListMembersRequest) (*identityv1.ListMembersResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	teamID, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	members, next, err := h.svc.ListMembers(ctx, user, teamID, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &identityv1.ListMembersResponse{NextPageToken: next, Members: make([]*identityv1.Member, 0, len(members))}
	for _, m := range members {
		resp.Members = append(resp.Members, memberToProto(m))
	}
	return resp, nil
}

func (h *Members) ListInvitations(ctx context.Context, req *identityv1.ListInvitationsRequest) (*identityv1.ListInvitationsResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	teamID, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	invitations, next, err := h.svc.ListInvitations(ctx, user, teamID, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &identityv1.ListInvitationsResponse{NextPageToken: next, Invitations: make([]*identityv1.Invitation, 0, len(invitations))}
	for _, i := range invitations {
		resp.Invitations = append(resp.Invitations, invitationToProto(i))
	}
	return resp, nil
}

func (h *Members) InviteMember(ctx context.Context, req *identityv1.InviteMemberRequest) (*identityv1.InviteMemberResponse, error) {
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
	role, err := roleFromProto(req.GetRole())
	if err != nil {
		return nil, err
	}
	invitation, token, err := h.svc.InviteMember(ctx, user, teamID, app.InviteMemberInput{
		RequestID: requestID, Role: role, Email: req.Email,
	})
	if err != nil {
		return nil, err
	}
	return &identityv1.InviteMemberResponse{Invitation: invitationToProto(invitation), Token: token}, nil
}

func (h *Members) AcceptInvitation(ctx context.Context, req *identityv1.AcceptInvitationRequest) (*identityv1.AcceptInvitationResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID("request_id", req.GetRequestId())
	if err != nil {
		return nil, err
	}
	membership, err := h.svc.AcceptInvitation(ctx, user, requestID, req.GetToken())
	if err != nil {
		return nil, err
	}
	return &identityv1.AcceptInvitationResponse{Membership: membershipToProto(membership)}, nil
}

func (h *Members) ChangeMemberRole(ctx context.Context, req *identityv1.ChangeMemberRoleRequest) (*identityv1.ChangeMemberRoleResponse, error) {
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
	target, err := parseUUID("user_id", req.GetUserId())
	if err != nil {
		return nil, err
	}
	role, err := roleFromProto(req.GetRole())
	if err != nil {
		return nil, err
	}
	member, err := h.svc.ChangeMemberRole(ctx, user, teamID, app.ChangeMemberRoleInput{
		RequestID: requestID, UserID: target, Role: role, Version: req.GetVersion(),
	})
	if err != nil {
		return nil, err
	}
	return &identityv1.ChangeMemberRoleResponse{Member: memberToProto(member)}, nil
}

func (h *Members) RemoveMember(ctx context.Context, req *identityv1.RemoveMemberRequest) (*identityv1.RemoveMemberResponse, error) {
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
	target, err := parseUUID("user_id", req.GetUserId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.RemoveMember(ctx, user, teamID, app.RemoveMemberInput{
		RequestID: requestID, UserID: target, Version: req.GetVersion(),
	}); err != nil {
		return nil, err
	}
	return &identityv1.RemoveMemberResponse{}, nil
}

func (h *Members) LeaveTeam(ctx context.Context, req *identityv1.LeaveTeamRequest) (*identityv1.LeaveTeamResponse, error) {
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
	if err := h.svc.LeaveTeam(ctx, user, teamID, requestID); err != nil {
		return nil, err
	}
	return &identityv1.LeaveTeamResponse{}, nil
}

func (h *Members) RevokeInvitation(ctx context.Context, req *identityv1.RevokeInvitationRequest) (*identityv1.RevokeInvitationResponse, error) {
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
	invitationID, err := parseUUID("invitation_id", req.GetInvitationId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.RevokeInvitation(ctx, user, teamID, invitationID, requestID); err != nil {
		return nil, err
	}
	return &identityv1.RevokeInvitationResponse{}, nil
}

func (h *Members) InitiateTransfer(ctx context.Context, req *identityv1.InitiateTransferRequest) (*identityv1.InitiateTransferResponse, error) {
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
	toUserID, err := parseUUID("to_user_id", req.GetToUserId())
	if err != nil {
		return nil, err
	}
	transfer, err := h.svc.InitiateTransfer(ctx, user, teamID, app.InitiateTransferInput{
		RequestID: requestID, ToUserID: toUserID,
	})
	if err != nil {
		return nil, err
	}
	return &identityv1.InitiateTransferResponse{Transfer: transferToProto(transfer)}, nil
}

func (h *Members) AcceptTransfer(ctx context.Context, req *identityv1.AcceptTransferRequest) (*identityv1.AcceptTransferResponse, error) {
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
	transferID, err := parseUUID("transfer_id", req.GetTransferId())
	if err != nil {
		return nil, err
	}
	membership, err := h.svc.AcceptTransfer(ctx, user, teamID, requestID, transferID)
	if err != nil {
		return nil, err
	}
	return &identityv1.AcceptTransferResponse{Membership: membershipToProto(membership)}, nil
}

func (h *Members) CancelTransfer(ctx context.Context, req *identityv1.CancelTransferRequest) (*identityv1.CancelTransferResponse, error) {
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
	transferID, err := parseUUID("transfer_id", req.GetTransferId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.CancelTransfer(ctx, user, teamID, requestID, transferID); err != nil {
		return nil, err
	}
	return &identityv1.CancelTransferResponse{}, nil
}
