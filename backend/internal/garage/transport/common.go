package transport

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	garagev1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

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

func assetStatusToProto(s string) garagev1.AssetStatus {
	switch s {
	case domain.AssetActive:
		return garagev1.AssetStatus_ASSET_STATUS_ACTIVE
	case domain.AssetSpare:
		return garagev1.AssetStatus_ASSET_STATUS_SPARE
	case domain.AssetRetired:
		return garagev1.AssetStatus_ASSET_STATUS_RETIRED
	default:
		return garagev1.AssetStatus_ASSET_STATUS_UNSPECIFIED
	}
}

func assetStatusFromProto(v garagev1.AssetStatus, fallback string) (string, error) {
	switch v {
	case garagev1.AssetStatus_ASSET_STATUS_UNSPECIFIED:
		return fallback, nil
	case garagev1.AssetStatus_ASSET_STATUS_ACTIVE:
		return domain.AssetActive, nil
	case garagev1.AssetStatus_ASSET_STATUS_SPARE:
		return domain.AssetSpare, nil
	case garagev1.AssetStatus_ASSET_STATUS_RETIRED:
		return domain.AssetRetired, nil
	default:
		return "", domain.ErrInvalid
	}
}

func tyreStatusToProto(s string) garagev1.TyreStatus {
	switch s {
	case domain.TyreNew:
		return garagev1.TyreStatus_TYRE_STATUS_NEW
	case domain.TyreUsed:
		return garagev1.TyreStatus_TYRE_STATUS_USED
	case domain.TyreWorn:
		return garagev1.TyreStatus_TYRE_STATUS_WORN
	case domain.TyreRetired:
		return garagev1.TyreStatus_TYRE_STATUS_RETIRED
	default:
		return garagev1.TyreStatus_TYRE_STATUS_UNSPECIFIED
	}
}

func tyreStatusFromProto(v garagev1.TyreStatus, fallback string) (string, error) {
	switch v {
	case garagev1.TyreStatus_TYRE_STATUS_UNSPECIFIED:
		return fallback, nil
	case garagev1.TyreStatus_TYRE_STATUS_NEW:
		return domain.TyreNew, nil
	case garagev1.TyreStatus_TYRE_STATUS_USED:
		return domain.TyreUsed, nil
	case garagev1.TyreStatus_TYRE_STATUS_WORN:
		return domain.TyreWorn, nil
	case garagev1.TyreStatus_TYRE_STATUS_RETIRED:
		return domain.TyreRetired, nil
	default:
		return "", domain.ErrInvalid
	}
}

func partKindToProto(k string) garagev1.PartKind {
	switch k {
	case domain.PartGlowPlug:
		return garagev1.PartKind_PART_KIND_GLOW_PLUG
	case domain.PartClutchShoe:
		return garagev1.PartKind_PART_KIND_CLUTCH_SHOE
	case domain.PartClutchSpring:
		return garagev1.PartKind_PART_KIND_CLUTCH_SPRING
	case domain.PartClutchBell:
		return garagev1.PartKind_PART_KIND_CLUTCH_BELL
	case domain.PartBearing:
		return garagev1.PartKind_PART_KIND_BEARING
	case domain.PartConrod:
		return garagev1.PartKind_PART_KIND_CONROD
	case domain.PartPistonSleeve:
		return garagev1.PartKind_PART_KIND_PISTON_SLEEVE
	case domain.PartAirFilter:
		return garagev1.PartKind_PART_KIND_AIR_FILTER
	case domain.PartShockOil:
		return garagev1.PartKind_PART_KIND_SHOCK_OIL
	case domain.PartDiffOil:
		return garagev1.PartKind_PART_KIND_DIFF_OIL
	case domain.PartFuel:
		return garagev1.PartKind_PART_KIND_FUEL
	case domain.PartSpring:
		return garagev1.PartKind_PART_KIND_SPRING
	case domain.PartTyre:
		return garagev1.PartKind_PART_KIND_TYRE
	case domain.PartInsert:
		return garagev1.PartKind_PART_KIND_INSERT
	case domain.PartBody:
		return garagev1.PartKind_PART_KIND_BODY
	case domain.PartWing:
		return garagev1.PartKind_PART_KIND_WING
	case domain.PartOther:
		return garagev1.PartKind_PART_KIND_OTHER
	default:
		return garagev1.PartKind_PART_KIND_UNSPECIFIED
	}
}

func partKindFromProto(v garagev1.PartKind) (string, error) {
	switch v {
	case garagev1.PartKind_PART_KIND_GLOW_PLUG:
		return domain.PartGlowPlug, nil
	case garagev1.PartKind_PART_KIND_CLUTCH_SHOE:
		return domain.PartClutchShoe, nil
	case garagev1.PartKind_PART_KIND_CLUTCH_SPRING:
		return domain.PartClutchSpring, nil
	case garagev1.PartKind_PART_KIND_CLUTCH_BELL:
		return domain.PartClutchBell, nil
	case garagev1.PartKind_PART_KIND_BEARING:
		return domain.PartBearing, nil
	case garagev1.PartKind_PART_KIND_CONROD:
		return domain.PartConrod, nil
	case garagev1.PartKind_PART_KIND_PISTON_SLEEVE:
		return domain.PartPistonSleeve, nil
	case garagev1.PartKind_PART_KIND_AIR_FILTER:
		return domain.PartAirFilter, nil
	case garagev1.PartKind_PART_KIND_SHOCK_OIL:
		return domain.PartShockOil, nil
	case garagev1.PartKind_PART_KIND_DIFF_OIL:
		return domain.PartDiffOil, nil
	case garagev1.PartKind_PART_KIND_FUEL:
		return domain.PartFuel, nil
	case garagev1.PartKind_PART_KIND_SPRING:
		return domain.PartSpring, nil
	case garagev1.PartKind_PART_KIND_TYRE:
		return domain.PartTyre, nil
	case garagev1.PartKind_PART_KIND_INSERT:
		return domain.PartInsert, nil
	case garagev1.PartKind_PART_KIND_BODY:
		return domain.PartBody, nil
	case garagev1.PartKind_PART_KIND_WING:
		return domain.PartWing, nil
	case garagev1.PartKind_PART_KIND_OTHER:
		return domain.PartOther, nil
	default:
		return "", domain.ErrInvalid
	}
}

func stockReasonToProto(r string) garagev1.StockReason {
	switch r {
	case domain.StockPurchase:
		return garagev1.StockReason_STOCK_REASON_PURCHASE
	case domain.StockConsumed:
		return garagev1.StockReason_STOCK_REASON_CONSUMED
	case domain.StockInstalled:
		return garagev1.StockReason_STOCK_REASON_INSTALLED
	case domain.StockAdjustment:
		return garagev1.StockReason_STOCK_REASON_ADJUSTMENT
	case domain.StockLost:
		return garagev1.StockReason_STOCK_REASON_LOST
	case domain.StockReturned:
		return garagev1.StockReason_STOCK_REASON_RETURNED
	default:
		return garagev1.StockReason_STOCK_REASON_UNSPECIFIED
	}
}

func stockReasonFromProto(v garagev1.StockReason) (string, error) {
	switch v {
	case garagev1.StockReason_STOCK_REASON_PURCHASE:
		return domain.StockPurchase, nil
	case garagev1.StockReason_STOCK_REASON_CONSUMED:
		return domain.StockConsumed, nil
	case garagev1.StockReason_STOCK_REASON_INSTALLED:
		return domain.StockInstalled, nil
	case garagev1.StockReason_STOCK_REASON_ADJUSTMENT:
		return domain.StockAdjustment, nil
	case garagev1.StockReason_STOCK_REASON_LOST:
		return domain.StockLost, nil
	case garagev1.StockReason_STOCK_REASON_RETURNED:
		return domain.StockReturned, nil
	default:
		return "", domain.ErrInvalid
	}
}

func timestamp(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func dateString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format("2006-01-02")
	return &s
}

func specsToProto(v map[string]any) *structpb.Struct {
	if v == nil {
		return nil
	}
	s, _ := structpb.NewStruct(v)
	return s
}

func specsFromProto(v *structpb.Struct) map[string]any {
	if v == nil {
		return nil
	}
	return v.AsMap()
}
