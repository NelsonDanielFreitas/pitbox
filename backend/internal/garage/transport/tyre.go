package transport

import (
	"context"

	"github.com/google/uuid"

	garagev1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1/garagev1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

type Tyres struct {
	garagev1connect.UnimplementedTyreServiceHandler
	svc *app.Service
}

func NewTyres(svc *app.Service) *Tyres { return &Tyres{svc: svc} }

func (h *Tyres) CreateTyreSet(ctx context.Context, req *garagev1.CreateTyreSetRequest) (*garagev1.CreateTyreSetResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	status, err := tyreStatusFromProto(req.GetStatus(), domain.TyreNew)
	if err != nil {
		return nil, err
	}
	t, err := h.svc.CreateTyreSet(ctx, user, team, app.CreateTyreSetInput{
		RequestID: requestID, Label: req.GetLabel(), Make: req.GetMake(), Model: req.GetModel(),
		Compound: req.Compound, InsertType: req.InsertType, GluedOn: req.GluedOn,
		Status: status, WearNotes: req.WearNotes,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.CreateTyreSetResponse{TyreSet: tyreToProto(t)}, nil
}

func (h *Tyres) GetTyreSet(ctx context.Context, req *garagev1.GetTyreSetRequest) (*garagev1.GetTyreSetResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetTyreSetId())
	if err != nil {
		return nil, err
	}
	t, err := h.svc.GetTyreSet(ctx, user, team, id)
	if err != nil {
		return nil, err
	}
	return &garagev1.GetTyreSetResponse{TyreSet: tyreToProto(t)}, nil
}

func (h *Tyres) ListTyreSets(ctx context.Context, req *garagev1.ListTyreSetsRequest) (*garagev1.ListTyreSetsResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	team, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	sets, next, err := h.svc.ListTyreSets(ctx, user, team, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &garagev1.ListTyreSetsResponse{NextPageToken: next, TyreSets: make([]*garagev1.TyreSet, 0, len(sets))}
	for _, t := range sets {
		resp.TyreSets = append(resp.TyreSets, tyreToProto(t))
	}
	return resp, nil
}

func (h *Tyres) UpdateTyreSet(ctx context.Context, req *garagev1.UpdateTyreSetRequest) (*garagev1.UpdateTyreSetResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetTyreSetId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	update, err := tyreUpdateFromProto(req)
	if err != nil {
		return nil, err
	}
	t, err := h.svc.UpdateTyreSet(ctx, user, team, app.UpdateTyreSetInput{
		RequestID: requestID, TyreSetID: id, Version: req.GetVersion(), Update: update,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.UpdateTyreSetResponse{TyreSet: tyreToProto(t)}, nil
}

func (h *Tyres) DeleteTyreSet(ctx context.Context, req *garagev1.DeleteTyreSetRequest) (*garagev1.DeleteTyreSetResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetTyreSetId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteTyreSet(ctx, user, team, requestID, id, req.GetVersion()); err != nil {
		return nil, err
	}
	return &garagev1.DeleteTyreSetResponse{}, nil
}

func (h *Tyres) GlueTyreSet(ctx context.Context, req *garagev1.GlueTyreSetRequest) (*garagev1.GlueTyreSetResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	tyrePart, err := parseUUID(req.GetTyrePartId())
	if err != nil {
		return nil, err
	}
	insertPart, err := parseUUID(req.GetInsertPartId())
	if err != nil {
		return nil, err
	}
	set, tyre, insert, err := h.svc.GlueTyreSet(ctx, user, team, app.GlueTyreSetInput{
		RequestID: requestID,
		Glue: domain.GlueInput{
			TyrePartID: tyrePart, InsertPartID: insertPart,
			TyreQty: req.GetTyreQty(), InsertQty: req.GetInsertQty(), Label: req.GetLabel(),
			Make: req.Make, Model: req.Model, Compound: req.Compound, InsertType: req.InsertType,
			GluedOn: req.GluedOn, Note: req.Note,
		},
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.GlueTyreSetResponse{TyreSet: tyreToProto(set), TyrePart: partToProto(tyre), InsertPart: partToProto(insert)}, nil
}

func tyreToProto(t domain.TyreSet) *garagev1.TyreSet {
	return &garagev1.TyreSet{
		Id: t.ID.String(), Label: t.Label, Make: t.Make, Model: t.Model, Compound: t.Compound,
		InsertType: t.InsertType, GluedOn: dateString(t.GluedOn), Status: tyreStatusToProto(t.Status),
		SessionsTotal: t.SessionsTotal, RunSecondsTotal: t.RunSecondsTotal, WearNotes: t.WearNotes,
		CreatedAt: timestamp(t.CreatedAt), Version: t.Version,
		TyrePartId: uuidString(t.TyrePartID), InsertPartId: uuidString(t.InsertPartID),
	}
}

func uuidString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func tyreUpdateFromProto(req *garagev1.UpdateTyreSetRequest) (domain.TyreUpdate, error) {
	var u domain.TyreUpdate
	for _, path := range req.GetUpdateMask().GetPaths() {
		switch path {
		case "label":
			v := req.GetLabel()
			u.Label = &v
		case "make":
			v := req.GetMake()
			u.Make = &v
		case "model":
			v := req.GetModel()
			u.Model = &v
		case "compound":
			u.CompoundSet = true
			u.Compound = req.Compound
		case "insert_type":
			u.InsertSet = true
			u.InsertType = req.InsertType
		case "glued_on":
			u.GluedSet = true
			u.GluedOn = req.GluedOn
		case "status":
			s, err := tyreStatusFromProto(req.GetStatus(), "")
			if err != nil || s == "" {
				return domain.TyreUpdate{}, domain.ErrInvalid
			}
			u.Status = &s
		case "wear_notes":
			u.WearSet = true
			u.WearNotes = req.WearNotes
		default:
			return domain.TyreUpdate{}, domain.ErrInvalid
		}
	}
	return u, nil
}
