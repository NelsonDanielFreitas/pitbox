package transport

import (
	"context"

	garagev1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1/garagev1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

type Parts struct {
	garagev1connect.UnimplementedPartServiceHandler
	svc *app.Service
}

func NewParts(svc *app.Service) *Parts { return &Parts{svc: svc} }

func (h *Parts) CreatePart(ctx context.Context, req *garagev1.CreatePartRequest) (*garagev1.CreatePartResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	kind, err := partKindFromProto(req.GetKind())
	if err != nil {
		return nil, err
	}
	p, err := h.svc.CreatePart(ctx, user, team, app.CreatePartInput{
		RequestID: requestID, Kind: kind, Brand: req.Brand, Reference: req.Reference,
		Description: req.GetDescription(), Unit: req.GetUnit(), Specs: specsFromProto(req.GetSpecs()),
		MinStockCenti: req.GetMinStockCenti(), UnitCostCents: req.UnitCostCents,
		Currency: req.GetCurrency(), HasServiceLife: req.GetHasServiceLife(),
		InitialQtyCenti: req.GetInitialQtyCenti(),
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.CreatePartResponse{Part: partToProto(p)}, nil
}

func (h *Parts) GetPart(ctx context.Context, req *garagev1.GetPartRequest) (*garagev1.GetPartResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetPartId())
	if err != nil {
		return nil, err
	}
	p, err := h.svc.GetPart(ctx, user, team, id)
	if err != nil {
		return nil, err
	}
	return &garagev1.GetPartResponse{Part: partToProto(p)}, nil
}

func (h *Parts) ListParts(ctx context.Context, req *garagev1.ListPartsRequest) (*garagev1.ListPartsResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	team, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	parts, next, err := h.svc.ListParts(ctx, user, team, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &garagev1.ListPartsResponse{NextPageToken: next, Parts: make([]*garagev1.Part, 0, len(parts))}
	for _, p := range parts {
		resp.Parts = append(resp.Parts, partToProto(p))
	}
	return resp, nil
}

func (h *Parts) UpdatePart(ctx context.Context, req *garagev1.UpdatePartRequest) (*garagev1.UpdatePartResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetPartId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	update, err := partUpdateFromProto(req)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.UpdatePart(ctx, user, team, app.UpdatePartInput{
		RequestID: requestID, PartID: id, Version: req.GetVersion(), Update: update,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.UpdatePartResponse{Part: partToProto(p)}, nil
}

func (h *Parts) DeletePart(ctx context.Context, req *garagev1.DeletePartRequest) (*garagev1.DeletePartResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetPartId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeletePart(ctx, user, team, requestID, id, req.GetVersion()); err != nil {
		return nil, err
	}
	return &garagev1.DeletePartResponse{}, nil
}

func (h *Parts) AddStockMovement(ctx context.Context, req *garagev1.AddStockMovementRequest) (*garagev1.AddStockMovementResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	partID, err := parseUUID(req.GetPartId())
	if err != nil {
		return nil, err
	}
	reason, err := stockReasonFromProto(req.GetReason())
	if err != nil {
		return nil, err
	}
	p, m, err := h.svc.AddStockMovement(ctx, user, team, app.AddStockMovementInput{
		RequestID: requestID, PartID: partID, QtyDeltaCenti: req.GetQtyDeltaCenti(),
		Reason: reason, Note: req.Note,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.AddStockMovementResponse{Part: partToProto(p), Movement: movementToProto(m)}, nil
}

func (h *Parts) ListStockMovements(ctx context.Context, req *garagev1.ListStockMovementsRequest) (*garagev1.ListStockMovementsResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetPartId())
	if err != nil {
		return nil, err
	}
	movements, next, err := h.svc.ListStockMovements(ctx, user, team, id, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &garagev1.ListStockMovementsResponse{NextPageToken: next, Movements: make([]*garagev1.StockMovement, 0, len(movements))}
	for _, m := range movements {
		resp.Movements = append(resp.Movements, movementToProto(m))
	}
	return resp, nil
}

func partToProto(p domain.Part) *garagev1.Part {
	return &garagev1.Part{
		Id: p.ID.String(), Kind: partKindToProto(p.Kind), Brand: p.Brand, Reference: p.Reference,
		Description: p.Description, Unit: p.Unit, Specs: specsToProto(p.Specs),
		MinStockCenti: p.MinStockCenti, UnitCostCents: p.UnitCostCents, Currency: p.Currency,
		HasServiceLife: p.HasServiceLife, QtyCenti: p.QtyCenti, BelowMin: p.BelowMin,
		CreatedAt: timestamp(p.CreatedAt), Version: p.Version,
	}
}

func movementToProto(m domain.StockMovement) *garagev1.StockMovement {
	return &garagev1.StockMovement{
		Id: m.ID.String(), PartId: m.PartID.String(), QtyDeltaCenti: m.QtyDeltaCenti,
		Reason: stockReasonToProto(m.Reason), Note: m.Note, OccurredAt: timestamp(m.OccurredAt),
	}
}

func partUpdateFromProto(req *garagev1.UpdatePartRequest) (domain.PartUpdate, error) {
	var u domain.PartUpdate
	for _, path := range req.GetUpdateMask().GetPaths() {
		switch path {
		case "kind":
			k, err := partKindFromProto(req.GetKind())
			if err != nil {
				return domain.PartUpdate{}, err
			}
			u.Kind = &k
		case "brand":
			u.BrandSet = true
			u.Brand = req.Brand
		case "reference":
			u.ReferenceSet = true
			u.Reference = req.Reference
		case "description":
			v := req.GetDescription()
			u.Description = &v
		case "unit":
			v := req.GetUnit()
			u.Unit = &v
		case "specs":
			u.SpecsSet = true
			u.Specs = specsFromProto(req.GetSpecs())
		case "min_stock_centi":
			u.MinSet = true
			u.MinStockCenti = req.GetMinStockCenti()
		case "unit_cost_cents":
			u.CostSet = true
			u.UnitCostCents = req.UnitCostCents
		case "currency":
			v := req.GetCurrency()
			u.Currency = &v
		case "has_service_life":
			v := req.GetHasServiceLife()
			u.ServiceLife = &v
		default:
			return domain.PartUpdate{}, domain.ErrInvalid
		}
	}
	return u, nil
}
