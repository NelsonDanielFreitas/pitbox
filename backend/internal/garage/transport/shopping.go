package transport

import (
	"context"

	garagev1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1/garagev1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

type Shopping struct {
	garagev1connect.UnimplementedShoppingServiceHandler
	svc *app.Service
}

func NewShopping(svc *app.Service) *Shopping { return &Shopping{svc: svc} }

func (h *Shopping) ListShoppingList(ctx context.Context, req *garagev1.ListShoppingListRequest) (*garagev1.ListShoppingListResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	team, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	lines, next, err := h.svc.ListShoppingList(ctx, user, team, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &garagev1.ListShoppingListResponse{NextPageToken: next, Lines: make([]*garagev1.ShoppingLine, 0, len(lines))}
	for _, l := range lines {
		resp.Lines = append(resp.Lines, shoppingLineToProto(l))
	}
	return resp, nil
}

func (h *Shopping) SetShoppingItem(ctx context.Context, req *garagev1.SetShoppingItemRequest) (*garagev1.SetShoppingItemResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	partID, err := parseUUID(req.GetPartId())
	if err != nil {
		return nil, err
	}
	line, err := h.svc.SetShoppingItem(ctx, user, team, app.SetShoppingItemInput{
		RequestID: requestID, PartID: partID, QtyCenti: req.QtyCenti, Note: req.Note,
		ExpectedVersion: req.GetExpectedVersion(),
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.SetShoppingItemResponse{Line: shoppingLineToProto(line)}, nil
}

func (h *Shopping) RemoveShoppingItem(ctx context.Context, req *garagev1.RemoveShoppingItemRequest) (*garagev1.RemoveShoppingItemResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	partID, err := parseUUID(req.GetPartId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.RemoveShoppingItem(ctx, user, team, app.RemoveShoppingItemInput{RequestID: requestID, PartID: partID}); err != nil {
		return nil, err
	}
	return &garagev1.RemoveShoppingItemResponse{}, nil
}

func shoppingLineToProto(l domain.ShoppingLine) *garagev1.ShoppingLine {
	return &garagev1.ShoppingLine{
		PartId: l.PartID.String(), QtyCenti: l.QtyCenti, SuggestedQtyCenti: l.SuggestedQtyCenti,
		BelowMin: l.BelowMin, Note: l.Note, ItemVersion: l.ItemVersion,
		EstimatedCostCents: l.EstimatedCostCents(),
	}
}
