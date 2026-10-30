// Package transport maps the Connect handlers of SyncService to the use cases.
package transport

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	syncv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/sync/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/sync/v1/syncv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
)

type Handler struct {
	syncv1connect.UnimplementedSyncServiceHandler
	svc *app.Service
}

func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

func caller(ctx context.Context, teamID string) (db.UserID, db.TeamID, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return db.UserID{}, db.TeamID{}, connect.NewError(connect.CodeUnauthenticated, "unauthenticated")
	}
	user, err := db.ParseUserID(p.UserID)
	if err != nil {
		return db.UserID{}, db.TeamID{}, connect.NewError(connect.CodeUnauthenticated, "unauthenticated")
	}
	team, err := db.ParseTeamID(teamID)
	if err != nil {
		return db.UserID{}, db.TeamID{}, fmt.Errorf("team id: %w", domain.ErrInvalid)
	}
	return user, team, nil
}

func parseID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("id: %w", domain.ErrInvalid)
	}
	return id, nil
}

func (h *Handler) RegisterDevice(ctx context.Context, req *syncv1.RegisterDeviceRequest) (*syncv1.RegisterDeviceResponse, error) {
	user, team, err := caller(ctx, req.GetTeamId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	id, err := h.svc.RegisterDevice(ctx, user, team, app.RegisterDeviceInput{
		Name: req.GetDeviceName(), Platform: req.GetPlatform(), PublicKey: req.GetPublicKey(),
		Proof: req.GetProof(), RequestID: requestID,
	})
	if err != nil {
		return nil, err
	}
	return &syncv1.RegisterDeviceResponse{DeviceId: id.String()}, nil
}

func (h *Handler) IssueOfflinePass(ctx context.Context, req *syncv1.IssueOfflinePassRequest) (*syncv1.IssueOfflinePassResponse, error) {
	user, team, err := caller(ctx, req.GetTeamId())
	if err != nil {
		return nil, err
	}
	device, err := parseID(req.GetDeviceId())
	if err != nil {
		return nil, err
	}
	token, expires, err := h.svc.IssueOfflinePass(ctx, user, team, device)
	if err != nil {
		return nil, err
	}
	return &syncv1.IssueOfflinePassResponse{Token: token, ExpiresAt: timestamppb.New(expires)}, nil
}

func (h *Handler) RevokeDevice(ctx context.Context, req *syncv1.RevokeDeviceRequest) (*syncv1.RevokeDeviceResponse, error) {
	user, team, err := caller(ctx, req.GetTeamId())
	if err != nil {
		return nil, err
	}
	device, err := parseID(req.GetDeviceId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.RevokeDevice(ctx, user, team, device, requestID); err != nil {
		return nil, err
	}
	return &syncv1.RevokeDeviceResponse{}, nil
}

var kindFromProto = map[syncv1.OpKind]domain.Kind{
	syncv1.OpKind_OP_KIND_CREATE: domain.KindCreate,
	syncv1.OpKind_OP_KIND_PATCH:  domain.KindPatch,
	syncv1.OpKind_OP_KIND_APPEND: domain.KindAppend,
	syncv1.OpKind_OP_KIND_DELETE: domain.KindDelete,
}

var statusToProto = map[domain.Status]syncv1.OpStatus{
	domain.StatusApplied:  syncv1.OpStatus_OP_STATUS_APPLIED,
	domain.StatusMerged:   syncv1.OpStatus_OP_STATUS_MERGED,
	domain.StatusConflict: syncv1.OpStatus_OP_STATUS_CONFLICT,
	domain.StatusRejected: syncv1.OpStatus_OP_STATUS_REJECTED,
}

func (h *Handler) Push(ctx context.Context, req *syncv1.PushRequest) (*syncv1.PushResponse, error) {
	user, team, err := caller(ctx, req.GetTeamId())
	if err != nil {
		return nil, err
	}
	device, err := parseID(req.GetDeviceId())
	if err != nil {
		return nil, err
	}
	ops := make([]domain.Operation, 0, len(req.GetOperations()))
	for _, o := range req.GetOperations() {
		opID, err := parseID(o.GetOpId())
		if err != nil {
			return nil, err
		}
		entityID, err := parseID(o.GetEntityId())
		if err != nil {
			return nil, err
		}
		kind, ok := kindFromProto[o.GetKind()]
		if !ok {
			return nil, fmt.Errorf("kind: %w", domain.ErrInvalid)
		}
		ops = append(ops, domain.Operation{
			OpID: opID, Entity: o.GetEntity(), EntityID: entityID, Kind: kind, BaseVersion: o.BaseVersion,
			FieldsJSON: o.GetFieldsJson(), ClientTS: o.GetClientTs().AsTime(), Signature: o.GetDeviceSignature(),
		})
	}
	results, err := h.svc.Push(ctx, user, team, app.PushInput{DeviceID: device, Pass: req.GetOfflinePass(), Ops: ops})
	if err != nil {
		return nil, err
	}
	resp := &syncv1.PushResponse{Results: make([]*syncv1.OperationResult, 0, len(results))}
	for _, r := range results {
		out := &syncv1.OperationResult{OpId: r.OpID.String(), Status: statusToProto[r.Status], Version: r.Version}
		if r.Reason != "" {
			reason := r.Reason
			out.Reason = &reason
		}
		resp.Results = append(resp.Results, out)
	}
	return resp, nil
}

func (h *Handler) Pull(ctx context.Context, req *syncv1.PullRequest) (*syncv1.PullResponse, error) {
	user, team, err := caller(ctx, req.GetTeamId())
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Pull(ctx, user, team, app.PullInput{
		Cursor: req.GetCursor(), Limit: req.GetLimit(), CatalogVersion: req.GetCatalogVersion(),
	})
	if err != nil {
		return nil, err
	}
	resp := &syncv1.PullResponse{
		Changes: make([]*syncv1.Change, 0, len(out.Changes)), NextCursor: out.NextCursor,
		HasMore: out.HasMore, CatalogVersion: out.CatalogVersion,
	}
	for _, c := range out.Changes {
		ch := &syncv1.Change{Entity: c.Entity, EntityId: c.EntityID.String(), Kind: syncv1.ChangeKind_CHANGE_KIND_UPSERT}
		if c.Deleted {
			ch.Kind = syncv1.ChangeKind_CHANGE_KIND_DELETE
		} else if ch.Data, err = toStruct(c.Data); err != nil {
			return nil, err
		}
		resp.Changes = append(resp.Changes, ch)
	}
	if len(out.CatalogSnapshot) > 0 {
		snap, err := toStruct(out.CatalogSnapshot)
		if err != nil {
			return nil, err
		}
		resp.CatalogSnapshot = snap
	}
	return resp, nil
}

func toStruct(raw json.RawMessage) (*structpb.Struct, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("entity data: %w", err)
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil, fmt.Errorf("entity data: %w", err)
	}
	return s, nil
}

func conflictToProto(c domain.Conflict) (*syncv1.Conflict, error) {
	sv, err := toValue(c.ServerValue)
	if err != nil {
		return nil, err
	}
	cv, err := toValue(c.ClientValue)
	if err != nil {
		return nil, err
	}
	return &syncv1.Conflict{
		Id: c.ID.String(), OpId: c.OpID.String(), Entity: c.Entity, EntityId: c.EntityID.String(),
		Field: c.Field, ServerValue: sv, ClientValue: cv, CreatedAt: timestamppb.New(c.CreatedAt),
	}, nil
}

func toValue(raw []byte) (*structpb.Value, error) {
	if len(raw) == 0 {
		return structpb.NewNullValue(), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("conflict value: %w", err)
	}
	return structpb.NewValue(v)
}

func (h *Handler) ListConflicts(ctx context.Context, req *syncv1.ListConflictsRequest) (*syncv1.ListConflictsResponse, error) {
	user, team, err := caller(ctx, req.GetTeamId())
	if err != nil {
		return nil, err
	}
	list, err := h.svc.ListConflicts(ctx, user, team)
	if err != nil {
		return nil, err
	}
	resp := &syncv1.ListConflictsResponse{Conflicts: make([]*syncv1.Conflict, 0, len(list))}
	for _, c := range list {
		pc, err := conflictToProto(c)
		if err != nil {
			return nil, err
		}
		resp.Conflicts = append(resp.Conflicts, pc)
	}
	return resp, nil
}

var resolutionFromProto = map[syncv1.ConflictResolution]string{
	syncv1.ConflictResolution_CONFLICT_RESOLUTION_SERVER: app.ResolveServer,
	syncv1.ConflictResolution_CONFLICT_RESOLUTION_CLIENT: app.ResolveClient,
	syncv1.ConflictResolution_CONFLICT_RESOLUTION_MANUAL: app.ResolveManual,
}

func (h *Handler) ResolveConflict(ctx context.Context, req *syncv1.ResolveConflictRequest) (*syncv1.ResolveConflictResponse, error) {
	user, team, err := caller(ctx, req.GetTeamId())
	if err != nil {
		return nil, err
	}
	id, err := parseID(req.GetConflictId())
	if err != nil {
		return nil, err
	}
	resolution, ok := resolutionFromProto[req.GetResolution()]
	if !ok {
		return nil, fmt.Errorf("resolution: %w", domain.ErrInvalid)
	}
	var value json.RawMessage
	if req.GetValue() != nil {
		if value, err = req.GetValue().MarshalJSON(); err != nil {
			return nil, fmt.Errorf("value: %w", domain.ErrInvalid)
		}
	}
	c, err := h.svc.ResolveConflict(ctx, user, team, id, resolution, value)
	if err != nil {
		return nil, err
	}
	pc, err := conflictToProto(c)
	if err != nil {
		return nil, err
	}
	return &syncv1.ResolveConflictResponse{Conflict: pc}, nil
}
