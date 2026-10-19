package transport

import (
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
)

func roleToProto(r domain.Role) identityv1.TeamRole {
	switch r {
	case domain.RoleOwner:
		return identityv1.TeamRole_TEAM_ROLE_OWNER
	case domain.RoleMechanic:
		return identityv1.TeamRole_TEAM_ROLE_MECHANIC
	case domain.RolePilot:
		return identityv1.TeamRole_TEAM_ROLE_PILOT
	case domain.RoleGuest:
		return identityv1.TeamRole_TEAM_ROLE_GUEST
	default:
		return identityv1.TeamRole_TEAM_ROLE_UNSPECIFIED
	}
}

func roleFromProto(r identityv1.TeamRole) (domain.Role, error) {
	switch r {
	case identityv1.TeamRole_TEAM_ROLE_OWNER:
		return domain.RoleOwner, nil
	case identityv1.TeamRole_TEAM_ROLE_MECHANIC:
		return domain.RoleMechanic, nil
	case identityv1.TeamRole_TEAM_ROLE_PILOT:
		return domain.RolePilot, nil
	case identityv1.TeamRole_TEAM_ROLE_GUEST:
		return domain.RoleGuest, nil
	default:
		return "", fmt.Errorf("role: %w", domain.ErrInvalid)
	}
}

func teamToProto(t domain.Team) *identityv1.Team {
	p := &identityv1.Team{
		Id: t.ID.String(), Name: t.Name, Category: t.Category, Club: t.Club,
		CountryCode: t.CountryCode, CreatedAt: timestamppb.New(t.CreatedAt), Version: t.Version,
	}
	if t.PendingTransfer != nil {
		p.PendingTransfer = transferToProto(*t.PendingTransfer)
	}
	return p
}

func transferToProto(t domain.OwnershipTransfer) *identityv1.OwnershipTransfer {
	return &identityv1.OwnershipTransfer{
		Id: t.ID.String(), FromUserId: t.FromUserID.String(), ToUserId: t.ToUserID.String(),
		CreatedAt: timestamppb.New(t.CreatedAt), ExpiresAt: timestamppb.New(t.ExpiresAt),
	}
}

func membershipToProto(m domain.TeamMembership) *identityv1.TeamMembership {
	return &identityv1.TeamMembership{Team: teamToProto(m.Team), Role: roleToProto(m.Role)}
}

func memberToProto(m domain.Member) *identityv1.Member {
	return &identityv1.Member{
		UserId: m.UserID.String(), DisplayName: m.DisplayName, Role: roleToProto(m.Role),
		JoinedAt: timestamppb.New(m.JoinedAt), Version: m.Version,
	}
}

func invitationToProto(i domain.Invitation) *identityv1.Invitation {
	p := &identityv1.Invitation{
		Id: i.ID.String(), Email: i.Email, Role: roleToProto(i.Role),
		ExpiresAt: timestamppb.New(i.ExpiresAt), CreatedAt: timestamppb.New(i.CreatedAt),
	}
	if i.AcceptedAt != nil {
		p.AcceptedAt = timestamppb.New(*i.AcceptedAt)
	}
	if i.RevokedAt != nil {
		p.RevokedAt = timestamppb.New(*i.RevokedAt)
	}
	return p
}

func parseUUID(field, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", field, domain.ErrInvalid)
	}
	return id, nil
}

func teamUpdateFromProto(req *identityv1.UpdateTeamRequest) (domain.TeamUpdate, error) {
	var update domain.TeamUpdate
	for _, path := range req.GetUpdateMask().GetPaths() {
		switch path {
		case "name":
			v := req.GetName()
			update.Name = &v
		case "category":
			v := req.GetCategory()
			update.Category = &v
		case "country_code":
			v := req.GetCountryCode()
			update.CountryCode = &v
		case "club":
			update.ClubSet = true
			update.Club = req.Club
		default:
			return domain.TeamUpdate{}, fmt.Errorf("update_mask %q: %w", path, domain.ErrInvalid)
		}
	}
	return update, nil
}
