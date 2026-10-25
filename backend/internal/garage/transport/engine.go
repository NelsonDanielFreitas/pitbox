package transport

import (
	"context"

	garagev1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1/garagev1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

type Engines struct {
	garagev1connect.UnimplementedEngineServiceHandler
	svc *app.Service
}

func NewEngines(svc *app.Service) *Engines { return &Engines{svc: svc} }

func (h *Engines) CreateEngine(ctx context.Context, req *garagev1.CreateEngineRequest) (*garagev1.CreateEngineResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	status, err := assetStatusFromProto(req.GetStatus(), domain.AssetActive)
	if err != nil {
		return nil, err
	}
	e, err := h.svc.CreateEngine(ctx, user, team, app.CreateEngineInput{
		RequestID: requestID, Make: req.GetMake(), Model: req.GetModel(), SerialNumber: req.SerialNumber,
		PurchasedOn: req.PurchasedOn, Status: status, Notes: req.Notes,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.CreateEngineResponse{Engine: engineToProto(e)}, nil
}

func (h *Engines) GetEngine(ctx context.Context, req *garagev1.GetEngineRequest) (*garagev1.GetEngineResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetEngineId())
	if err != nil {
		return nil, err
	}
	e, err := h.svc.GetEngine(ctx, user, team, id)
	if err != nil {
		return nil, err
	}
	return &garagev1.GetEngineResponse{Engine: engineToProto(e)}, nil
}

func (h *Engines) ListEngines(ctx context.Context, req *garagev1.ListEnginesRequest) (*garagev1.ListEnginesResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	team, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	engines, next, err := h.svc.ListEngines(ctx, user, team, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &garagev1.ListEnginesResponse{NextPageToken: next, Engines: make([]*garagev1.Engine, 0, len(engines))}
	for _, e := range engines {
		resp.Engines = append(resp.Engines, engineToProto(e))
	}
	return resp, nil
}

func (h *Engines) UpdateEngine(ctx context.Context, req *garagev1.UpdateEngineRequest) (*garagev1.UpdateEngineResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetEngineId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	update, err := engineUpdateFromProto(req)
	if err != nil {
		return nil, err
	}
	e, err := h.svc.UpdateEngine(ctx, user, team, app.UpdateEngineInput{
		RequestID: requestID, EngineID: id, Version: req.GetVersion(), Update: update,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.UpdateEngineResponse{Engine: engineToProto(e)}, nil
}

func (h *Engines) DeleteEngine(ctx context.Context, req *garagev1.DeleteEngineRequest) (*garagev1.DeleteEngineResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetEngineId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteEngine(ctx, user, team, requestID, id, req.GetVersion()); err != nil {
		return nil, err
	}
	return &garagev1.DeleteEngineResponse{}, nil
}

func (h *Engines) MountEngine(ctx context.Context, req *garagev1.MountEngineRequest) (*garagev1.MountEngineResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	engine, err := parseUUID(req.GetEngineId())
	if err != nil {
		return nil, err
	}
	car, err := parseUUID(req.GetCarId())
	if err != nil {
		return nil, err
	}
	m, err := h.svc.MountEngine(ctx, user, team, requestID, engine, car)
	if err != nil {
		return nil, err
	}
	return &garagev1.MountEngineResponse{Mount: mountToProto(m)}, nil
}

func (h *Engines) UnmountEngine(ctx context.Context, req *garagev1.UnmountEngineRequest) (*garagev1.UnmountEngineResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	engine, err := parseUUID(req.GetEngineId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.UnmountEngine(ctx, user, team, requestID, engine); err != nil {
		return nil, err
	}
	return &garagev1.UnmountEngineResponse{}, nil
}

func (h *Engines) MoveEngine(ctx context.Context, req *garagev1.MoveEngineRequest) (*garagev1.MoveEngineResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	engine, err := parseUUID(req.GetEngineId())
	if err != nil {
		return nil, err
	}
	car, err := parseUUID(req.GetCarId())
	if err != nil {
		return nil, err
	}
	m, err := h.svc.MoveEngine(ctx, user, team, requestID, engine, car)
	if err != nil {
		return nil, err
	}
	return &garagev1.MoveEngineResponse{Mount: mountToProto(m)}, nil
}

func (h *Engines) ListEngineMounts(ctx context.Context, req *garagev1.ListEngineMountsRequest) (*garagev1.ListEngineMountsResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetEngineId())
	if err != nil {
		return nil, err
	}
	mounts, next, err := h.svc.ListEngineMounts(ctx, user, team, id, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &garagev1.ListEngineMountsResponse{NextPageToken: next, Mounts: make([]*garagev1.EngineMount, 0, len(mounts))}
	for _, m := range mounts {
		resp.Mounts = append(resp.Mounts, mountToProto(m))
	}
	return resp, nil
}

func engineToProto(e domain.Engine) *garagev1.Engine {
	return &garagev1.Engine{
		Id: e.ID.String(), Make: e.Make, Model: e.Model, SerialNumber: e.SerialNumber,
		PurchasedOn: dateString(e.PurchasedOn), Status: assetStatusToProto(e.Status),
		TanksCenti: e.TanksCenti, RunSecondsTotal: e.RunSecondsTotal, SessionsTotal: e.SessionsTotal,
		Notes: e.Notes, CreatedAt: timestamp(e.CreatedAt), Version: e.Version,
	}
}

func mountToProto(m domain.EngineMount) *garagev1.EngineMount {
	p := &garagev1.EngineMount{Id: m.ID.String(), EngineId: m.EngineID.String(), CarId: m.CarID.String(), MountedAt: timestamp(m.MountedAt)}
	if m.UnmountedAt != nil {
		p.UnmountedAt = timestamp(*m.UnmountedAt)
	}
	return p
}

func engineUpdateFromProto(req *garagev1.UpdateEngineRequest) (domain.EngineUpdate, error) {
	var u domain.EngineUpdate
	for _, path := range req.GetUpdateMask().GetPaths() {
		switch path {
		case "make":
			v := req.GetMake()
			u.Make = &v
		case "model":
			v := req.GetModel()
			u.Model = &v
		case "serial_number":
			u.SerialSet = true
			u.SerialNumber = req.SerialNumber
		case "purchased_on":
			u.PurchasedSet = true
			u.PurchasedOn = req.PurchasedOn
		case "status":
			s, err := assetStatusFromProto(req.GetStatus(), "")
			if err != nil || s == "" {
				return domain.EngineUpdate{}, domain.ErrInvalid
			}
			u.Status = &s
		case "notes":
			u.NotesSet = true
			u.Notes = req.Notes
		default:
			return domain.EngineUpdate{}, domain.ErrInvalid
		}
	}
	return u, nil
}
