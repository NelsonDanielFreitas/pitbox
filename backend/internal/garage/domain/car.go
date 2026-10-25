package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultScale      = "1/8"
	DefaultPowertrain = "nitro"
)

// Car is a chassis owned by one team.
type Car struct {
	ID                uuid.UUID
	Make              string
	Model             string
	ModelYear         *int32
	SerialNumber      *string
	Nickname          *string
	TransponderNumber *string
	Status            string
	Scale             string
	Powertrain        string
	Notes             *string
	CreatedAt         time.Time
	Version           int32
}

// NewCar validates a new chassis. Empty scale/powertrain use the v0.1 defaults.
func NewCar(make, model string, modelYear *int32, serial, nickname, transponder *string, status, scale, powertrain string, notes *string) (Car, error) {
	make, err := requiredString(make, maxMakeModel)
	if err != nil {
		return Car{}, err
	}
	model, err = requiredString(model, maxMakeModel)
	if err != nil {
		return Car{}, err
	}
	serial, err = optionalString(serial, maxSerial)
	if err != nil {
		return Car{}, err
	}
	nickname, err = optionalString(nickname, maxNickname)
	if err != nil {
		return Car{}, err
	}
	transponder, err = optionalString(transponder, maxTransponder)
	if err != nil {
		return Car{}, err
	}
	notes, err = optionalString(notes, maxNotes)
	if err != nil {
		return Car{}, err
	}
	if modelYear != nil && (*modelYear < 1990 || *modelYear > 2100) {
		return Car{}, fmt.Errorf("%w: model year", ErrInvalid)
	}
	if status == "" {
		status = AssetActive
	}
	if !ValidAssetStatus(status) {
		return Car{}, fmt.Errorf("%w: status", ErrInvalid)
	}
	scale = trimOr(scale, DefaultScale)
	powertrain = trimOr(powertrain, DefaultPowertrain)
	if err := validCatalogCode(scale, powertrain); err != nil {
		return Car{}, err
	}
	return Car{
		Make: make, Model: model, ModelYear: modelYear, SerialNumber: serial,
		Nickname: nickname, TransponderNumber: transponder, Status: status,
		Scale: scale, Powertrain: powertrain, Notes: notes,
	}, nil
}

// CarUpdate selects the fields of an update. The Set flags distinguish "clear" from
// "not in the mask".
type CarUpdate struct {
	Make              *string
	Model             *string
	ModelYearSet      bool
	ModelYear         *int32
	SerialNumberSet   bool
	SerialNumber      *string
	NicknameSet       bool
	Nickname          *string
	TransponderSet    bool
	TransponderNumber *string
	Status            *string
	Scale             *string
	Powertrain        *string
	NotesSet          bool
	Notes             *string
}

func (u CarUpdate) Empty() bool {
	return u.Make == nil && u.Model == nil && !u.ModelYearSet && !u.SerialNumberSet && !u.NicknameSet &&
		!u.TransponderSet && u.Status == nil && u.Scale == nil && u.Powertrain == nil && !u.NotesSet
}

func ApplyCarUpdate(c Car, u CarUpdate) (Car, error) {
	if u.Empty() {
		return Car{}, fmt.Errorf("%w: empty update mask", ErrInvalid)
	}
	var err error
	if u.Make != nil {
		c.Make, err = requiredString(*u.Make, maxMakeModel)
		if err != nil {
			return Car{}, err
		}
	}
	if u.Model != nil {
		c.Model, err = requiredString(*u.Model, maxMakeModel)
		if err != nil {
			return Car{}, err
		}
	}
	if u.ModelYearSet {
		if u.ModelYear != nil && (*u.ModelYear < 1990 || *u.ModelYear > 2100) {
			return Car{}, fmt.Errorf("%w: model year", ErrInvalid)
		}
		c.ModelYear = u.ModelYear
	}
	if u.SerialNumberSet {
		c.SerialNumber, err = optionalString(u.SerialNumber, maxSerial)
		if err != nil {
			return Car{}, err
		}
	}
	if u.NicknameSet {
		c.Nickname, err = optionalString(u.Nickname, maxNickname)
		if err != nil {
			return Car{}, err
		}
	}
	if u.TransponderSet {
		c.TransponderNumber, err = optionalString(u.TransponderNumber, maxTransponder)
		if err != nil {
			return Car{}, err
		}
	}
	if u.NotesSet {
		c.Notes, err = optionalString(u.Notes, maxNotes)
		if err != nil {
			return Car{}, err
		}
	}
	if u.Status != nil {
		if !ValidAssetStatus(*u.Status) {
			return Car{}, fmt.Errorf("%w: status", ErrInvalid)
		}
		c.Status = *u.Status
	}
	if u.Scale != nil {
		if _, err := requiredString(*u.Scale, 16); err != nil {
			return Car{}, err
		}
		c.Scale = *u.Scale
	}
	if u.Powertrain != nil {
		if _, err := requiredString(*u.Powertrain, 32); err != nil {
			return Car{}, err
		}
		c.Powertrain = *u.Powertrain
	}
	return c, nil
}

func trimOr(value, fallback string) string {
	value = trimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
