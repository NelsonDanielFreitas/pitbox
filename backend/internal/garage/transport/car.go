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

type Cars struct {
	garagev1connect.UnimplementedCarServiceHandler
	svc *app.Service
}

func NewCars(svc *app.Service) *Cars { return &Cars{svc: svc} }

func (h *Cars) CreateCar(ctx context.Context, req *garagev1.CreateCarRequest) (*garagev1.CreateCarResponse, error) {
	user, team, requestID, err := callerTeamRequest(ctx, req.GetTeamId(), req.GetRequestId())
	if err != nil {
		return nil, err
	}
	status, err := assetStatusFromProto(req.GetStatus(), domain.AssetActive)
	if err != nil {
		return nil, err
	}
	car, err := h.svc.CreateCar(ctx, user, team, app.CreateCarInput{
		RequestID: requestID, Make: req.GetMake(), Model: req.GetModel(), ModelYear: req.ModelYear,
		SerialNumber: req.SerialNumber, Nickname: req.Nickname, TransponderNumber: req.TransponderNumber,
		Status: status, Scale: req.GetScale(), Powertrain: req.GetPowertrain(), Notes: req.Notes,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.CreateCarResponse{Car: carToProto(car)}, nil
}

func (h *Cars) GetCar(ctx context.Context, req *garagev1.GetCarRequest) (*garagev1.GetCarResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetCarId())
	if err != nil {
		return nil, err
	}
	car, err := h.svc.GetCar(ctx, user, team, id)
	if err != nil {
		return nil, err
	}
	return &garagev1.GetCarResponse{Car: carToProto(car)}, nil
}

func (h *Cars) ListCars(ctx context.Context, req *garagev1.ListCarsRequest) (*garagev1.ListCarsResponse, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	team, err := db.ParseTeamID(req.GetTeamId())
	if err != nil {
		return nil, err
	}
	cars, next, err := h.svc.ListCars(ctx, user, team, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp := &garagev1.ListCarsResponse{NextPageToken: next, Cars: make([]*garagev1.Car, 0, len(cars))}
	for _, car := range cars {
		resp.Cars = append(resp.Cars, carToProto(car))
	}
	return resp, nil
}

func (h *Cars) UpdateCar(ctx context.Context, req *garagev1.UpdateCarRequest) (*garagev1.UpdateCarResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetCarId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	update, err := carUpdateFromProto(req)
	if err != nil {
		return nil, err
	}
	car, err := h.svc.UpdateCar(ctx, user, team, app.UpdateCarInput{
		RequestID: requestID, CarID: id, Version: req.GetVersion(), Update: update,
	})
	if err != nil {
		return nil, err
	}
	return &garagev1.UpdateCarResponse{Car: carToProto(car)}, nil
}

func (h *Cars) DeleteCar(ctx context.Context, req *garagev1.DeleteCarRequest) (*garagev1.DeleteCarResponse, error) {
	user, team, id, err := callerTeamID(ctx, req.GetTeamId(), req.GetCarId())
	if err != nil {
		return nil, err
	}
	requestID, err := parseUUID(req.GetRequestId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteCar(ctx, user, team, requestID, id, req.GetVersion()); err != nil {
		return nil, err
	}
	return &garagev1.DeleteCarResponse{}, nil
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

func carToProto(c domain.Car) *garagev1.Car {
	return &garagev1.Car{
		Id: c.ID.String(), Make: c.Make, Model: c.Model, ModelYear: c.ModelYear,
		SerialNumber: c.SerialNumber, Nickname: c.Nickname, TransponderNumber: c.TransponderNumber,
		Status: assetStatusToProto(c.Status), Scale: c.Scale, Powertrain: c.Powertrain,
		Notes: c.Notes, CreatedAt: timestamp(c.CreatedAt), Version: c.Version,
	}
}

func carUpdateFromProto(req *garagev1.UpdateCarRequest) (domain.CarUpdate, error) {
	var u domain.CarUpdate
	for _, path := range req.GetUpdateMask().GetPaths() {
		switch path {
		case "make":
			v := req.GetMake()
			u.Make = &v
		case "model":
			v := req.GetModel()
			u.Model = &v
		case "model_year":
			u.ModelYearSet = true
			u.ModelYear = req.ModelYear
		case "serial_number":
			u.SerialNumberSet = true
			u.SerialNumber = req.SerialNumber
		case "nickname":
			u.NicknameSet = true
			u.Nickname = req.Nickname
		case "transponder_number":
			u.TransponderSet = true
			u.TransponderNumber = req.TransponderNumber
		case "notes":
			u.NotesSet = true
			u.Notes = req.Notes
		case "status":
			s, err := assetStatusFromProto(req.GetStatus(), "")
			if err != nil || s == "" {
				return domain.CarUpdate{}, domain.ErrInvalid
			}
			u.Status = &s
		case "scale":
			v := req.GetScale()
			u.Scale = &v
		case "powertrain":
			v := req.GetPowertrain()
			u.Powertrain = &v
		default:
			return domain.CarUpdate{}, domain.ErrInvalid
		}
	}
	return u, nil
}
