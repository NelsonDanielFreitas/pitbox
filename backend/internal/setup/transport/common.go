package transport

import (
	"context"
	"errors"
	"fmt"
	"math"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	setupv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/setup/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

const msgInvalidSetupData = "invalid setup data"

func caller(ctx context.Context) (db.UserID, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return db.UserID{}, connect.NewError(connect.CodeUnauthenticated, "unauthenticated")
	}
	return db.ParseUserID(p.UserID)
}

func parseUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("id: %w", domain.ErrInvalid)
	}
	return id, nil
}

func callerTeamID(ctx context.Context, teamID, id string) (db.UserID, db.TeamID, uuid.UUID, error) {
	user, err := caller(ctx)
	if err != nil {
		return db.UserID{}, db.TeamID{}, uuid.Nil, err
	}
	team, err := db.ParseTeamID(teamID)
	if err != nil {
		return db.UserID{}, db.TeamID{}, uuid.Nil, err
	}
	entity, err := parseUUID(id)
	if err != nil {
		return db.UserID{}, db.TeamID{}, uuid.Nil, err
	}
	return user, team, entity, nil
}

func callerTeamRequest(ctx context.Context, teamID, requestID string) (db.UserID, db.TeamID, uuid.UUID, error) {
	user, err := caller(ctx)
	if err != nil {
		return db.UserID{}, db.TeamID{}, uuid.Nil, err
	}
	team, err := db.ParseTeamID(teamID)
	if err != nil {
		return db.UserID{}, db.TeamID{}, uuid.Nil, err
	}
	id, err := parseUUID(requestID)
	if err != nil {
		return db.UserID{}, db.TeamID{}, uuid.Nil, err
	}
	return user, team, id, nil
}

func setupError(err error) error {
	if err == nil {
		return nil
	}
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		detail, derr := connectproto.NewErrorDetail(&setupv1.SetupDataViolation{
			Path: ve.Path, RuleId: ve.RuleID,
		})
		if derr == nil {
			return connect.NewError(connect.CodeInvalidArgument, msgInvalidSetupData).
				WithDetail(detail).WithCause(err)
		}
		return connect.NewError(connect.CodeInvalidArgument, msgInvalidSetupData).WithCause(err)
	}
	return err
}

func structToMap(s *structpb.Struct) (map[string]any, error) {
	if s == nil {
		return map[string]any{}, nil
	}
	v, err := valueToAny(&structpb.Value{Kind: &structpb.Value_StructValue{StructValue: s}}, "", 1)
	if err != nil {
		return nil, err
	}
	return v.(map[string]any), nil
}

func valueToAny(v *structpb.Value, path string, depth int) (any, error) {
	if depth > domain.MaxDataDepth {
		return nil, &domain.ValidationError{Path: path, RuleID: domain.RuleDepth}
	}
	if v == nil || v.GetKind() == nil {
		return nil, &domain.ValidationError{Path: path, RuleID: domain.RuleType}
	}
	switch kind := v.GetKind().(type) {
	case *structpb.Value_NullValue:
		return nil, nil
	case *structpb.Value_NumberValue:
		if math.IsNaN(kind.NumberValue) || math.IsInf(kind.NumberValue, 0) {
			return nil, &domain.ValidationError{Path: path, RuleID: domain.RuleType}
		}
		return kind.NumberValue, nil
	case *structpb.Value_StringValue:
		return kind.StringValue, nil
	case *structpb.Value_BoolValue:
		return kind.BoolValue, nil
	case *structpb.Value_StructValue:
		out := make(map[string]any, len(kind.StructValue.GetFields()))
		for key, child := range kind.StructValue.GetFields() {
			p := join(path, key)
			value, err := valueToAny(child, p, depth+1)
			if err != nil {
				return nil, err
			}
			out[key] = value
		}
		return out, nil
	case *structpb.Value_ListValue:
		out := make([]any, 0, len(kind.ListValue.GetValues()))
		for i, child := range kind.ListValue.GetValues() {
			p := fmt.Sprintf("%s[%d]", path, i)
			value, err := valueToAny(child, p, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	default:
		return nil, &domain.ValidationError{Path: path, RuleID: domain.RuleType}
	}
}

func dataToStruct(data map[string]any) (*structpb.Struct, error) {
	s, err := structpb.NewStruct(data)
	if err != nil {
		return nil, fmt.Errorf("setup data: %w", domain.ErrInvalid)
	}
	return s, nil
}

func valueToProto(v any) (*structpb.Value, error) {
	if v == nil {
		return nil, nil
	}
	return structpb.NewValue(v)
}

func join(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func setupToProto(s domain.Setup) *setupv1.Setup {
	return &setupv1.Setup{
		Id: s.ID, CarId: s.CarID, Name: s.Name, CurrentVersion: s.CurrentVersion,
		Notes: s.Notes, CreatedAt: timestamppb.New(s.CreatedAt),
		UpdatedAt: timestamppb.New(s.UpdatedAt), Version: s.Version,
	}
}

func versionRefToProto(v domain.VersionRef) *setupv1.SetupVersionRef {
	return &setupv1.SetupVersionRef{
		SetupId: v.SetupID, Version: v.Version, SchemaVersion: v.SchemaVersion,
		BasedOnSetupId: v.BasedOnSetupID, BasedOnVersion: v.BasedOnVersion,
		ChangeSummary: v.ChangeSummary, CreatedAt: timestamppb.New(v.CreatedAt),
	}
}

func versionToProto(v domain.Version) (*setupv1.SetupVersion, error) {
	data, err := dataToStruct(v.Data)
	if err != nil {
		return nil, err
	}
	return &setupv1.SetupVersion{Ref: versionRefToProto(v.Ref), Data: data}, nil
}

func differenceToProto(d domain.Difference) (*setupv1.SetupDifference, error) {
	before, err := valueToProto(d.Before)
	if err != nil {
		return nil, err
	}
	after, err := valueToProto(d.After)
	if err != nil {
		return nil, err
	}
	kind := setupv1.DifferenceKind_DIFFERENCE_KIND_UNSPECIFIED
	switch d.Kind {
	case domain.DifferenceAdded:
		kind = setupv1.DifferenceKind_DIFFERENCE_KIND_ADDED
	case domain.DifferenceRemoved:
		kind = setupv1.DifferenceKind_DIFFERENCE_KIND_REMOVED
	case domain.DifferenceChanged:
		kind = setupv1.DifferenceKind_DIFFERENCE_KIND_CHANGED
	}
	return &setupv1.SetupDifference{Path: d.Path, Before: before, After: after, Kind: kind}, nil
}

func shareLinkToProto(l domain.ShareLink) *setupv1.ShareLink {
	out := &setupv1.ShareLink{
		Id: l.ID, SetupId: l.SetupID, UseCount: l.UseCount,
		CreatedAt: timestamppb.New(l.CreatedAt),
	}
	if l.PinnedVersion != nil {
		out.PinnedVersion = l.PinnedVersion
	}
	if l.ExpiresAt != nil {
		out.ExpiresAt = timestamppb.New(*l.ExpiresAt)
	}
	if l.RevokedAt != nil {
		out.RevokedAt = timestamppb.New(*l.RevokedAt)
	}
	if l.LastUsedAt != nil {
		out.LastUsedAt = timestamppb.New(*l.LastUsedAt)
	}
	return out
}

func sharedSetupToProto(s domain.SharedSetup) (*setupv1.SharedSetup, error) {
	data, err := dataToStruct(s.Data)
	if err != nil {
		return nil, err
	}
	return &setupv1.SharedSetup{
		Name: s.Name, Version: s.Version, SchemaVersion: s.SchemaVersion,
		Data: data, ChangeSummary: s.ChangeSummary,
		VersionCreatedAt: timestamppb.New(s.VersionCreatedAt),
	}, nil
}
