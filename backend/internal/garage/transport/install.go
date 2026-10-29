package transport

import (
	"context"
	"time"

	"github.com/google/uuid"

	garagev1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

func (h *Parts) InstallPart(ctx context.Context, req *garagev1.InstallPartRequest) (*garagev1.InstallPartResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	partID, err := parseUUID(req.GetPartId())
	if err != nil {
		return nil, err
	}
	carID, err := optionalUUID(req.GetCarId())
	if err != nil {
		return nil, err
	}
	engineID, err := optionalUUID(req.GetEngineId())
	if err != nil {
		return nil, err
	}
	var installedAt *time.Time
	if req.InstalledAt != nil {
		t := req.GetInstalledAt().AsTime()
		installedAt = &t
	}
	install, part, err := h.svc.InstallPart(ctx, user, team, app.InstallPartInput{
		RequestID: requestID, PartID: partID, CarID: carID, EngineID: engineID,
		InstalledAt: installedAt, Note: req.Note,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.InstallPartResponse{Install: installToProto(install), Part: partToProto(part)}, nil
}

func (h *Parts) RemovePart(ctx context.Context, req *garagev1.RemovePartRequest) (*garagev1.RemovePartResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	installID, err := parseUUID(req.GetInstallId())
	if err != nil {
		return nil, err
	}
	destination, err := removalDestinationFromProto(req.GetDestination())
	if err != nil {
		return nil, err
	}
	reason, err := removalReasonFromProto(req.GetReason())
	if err != nil {
		return nil, err
	}
	install, part, err := h.svc.RemovePart(ctx, user, team, app.RemovePartInput{
		RequestID: requestID, InstallID: installID, Destination: destination, Reason: reason, Note: req.Note,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.RemovePartResponse{Install: installToProto(install), Part: partToProto(part)}, nil
}

func (h *Parts) ListPartInstalls(ctx context.Context, req *garagev1.ListPartInstallsRequest) (*garagev1.ListPartInstallsResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	team, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	var filter domain.InstallFilter
	if filter.PartID, err = optionalUUID(req.GetPartId()); err != nil {
		return nil, err
	}
	if filter.CarID, err = optionalUUID(req.GetCarId()); err != nil {
		return nil, err
	}
	if filter.EngineID, err = optionalUUID(req.GetEngineId()); err != nil {
		return nil, err
	}
	installs, next, err := h.svc.ListPartInstalls(ctx, user, team, filter, req.GetIncludeHistory(), req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &garagev1.ListPartInstallsResponse{NextPageToken: next, Installs: make([]*garagev1.PartInstall, 0, len(installs))}
	for _, i := range installs {
		resp.Installs = append(resp.Installs, installToProto(i))
	}
	return resp, nil
}

func optionalUUID(value string) (*uuid.UUID, error) {
	if value == "" {
		return nil, nil
	}
	id, err := parseUUID(value)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func installToProto(i domain.PartInstall) *garagev1.PartInstall {
	out := &garagev1.PartInstall{
		Id: i.ID.String(), PartId: i.PartID.String(), InstalledAt: timestamp(i.InstalledAt),
		RemovalDestination: removalDestinationToProto(i.RemovalDestination),
		RemovalReason:      removalReasonToProto(i.RemovalReason),
		RunSeconds:         i.RunSeconds, Sessions: i.Sessions, Version: i.Version,
	}
	switch {
	case i.CarID != nil:
		out.Target = &garagev1.PartInstall_CarId{CarId: i.CarID.String()}
	case i.EngineID != nil:
		out.Target = &garagev1.PartInstall_EngineId{EngineId: i.EngineID.String()}
	}
	if i.RemovedAt != nil {
		out.RemovedAt = timestamp(*i.RemovedAt)
	}
	return out
}

func removalDestinationToProto(d string) garagev1.RemovalDestination {
	switch d {
	case domain.RemovalStock:
		return garagev1.RemovalDestination_REMOVAL_DESTINATION_STOCK
	case domain.RemovalScrap:
		return garagev1.RemovalDestination_REMOVAL_DESTINATION_SCRAP
	case domain.RemovalSpare:
		return garagev1.RemovalDestination_REMOVAL_DESTINATION_SPARE
	}
	return garagev1.RemovalDestination_REMOVAL_DESTINATION_UNSPECIFIED
}

func removalDestinationFromProto(d garagev1.RemovalDestination) (string, error) {
	switch d {
	case garagev1.RemovalDestination_REMOVAL_DESTINATION_STOCK:
		return domain.RemovalStock, nil
	case garagev1.RemovalDestination_REMOVAL_DESTINATION_SCRAP:
		return domain.RemovalScrap, nil
	case garagev1.RemovalDestination_REMOVAL_DESTINATION_SPARE:
		return domain.RemovalSpare, nil
	}
	return "", domain.ErrInvalid
}

func removalReasonToProto(r string) garagev1.RemovalReason {
	switch r {
	case domain.ReasonBroken:
		return garagev1.RemovalReason_REMOVAL_REASON_BROKEN
	case domain.ReasonWorn:
		return garagev1.RemovalReason_REMOVAL_REASON_WORN
	case domain.ReasonLost:
		return garagev1.RemovalReason_REMOVAL_REASON_LOST
	}
	return garagev1.RemovalReason_REMOVAL_REASON_UNSPECIFIED
}

// removalReasonFromProto maps UNSPECIFIED to "" (no reason); domain.NewRemoval decides
// whether that is allowed for the destination.
func removalReasonFromProto(r garagev1.RemovalReason) (string, error) {
	switch r {
	case garagev1.RemovalReason_REMOVAL_REASON_UNSPECIFIED:
		return "", nil
	case garagev1.RemovalReason_REMOVAL_REASON_BROKEN:
		return domain.ReasonBroken, nil
	case garagev1.RemovalReason_REMOVAL_REASON_WORN:
		return domain.ReasonWorn, nil
	case garagev1.RemovalReason_REMOVAL_REASON_LOST:
		return domain.ReasonLost, nil
	}
	return "", domain.ErrInvalid
}
