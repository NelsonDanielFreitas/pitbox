package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Engine is a nitro engine owned by the team. Counters are maintained by F2-04.
type Engine struct {
	ID              uuid.UUID
	Make            string
	Model           string
	SerialNumber    *string
	PurchasedOn     *time.Time
	Status          string
	TanksCenti      int64
	RunSecondsTotal int32
	SessionsTotal   int32
	Notes           *string
	CreatedAt       time.Time
	Version         int32
}

func NewEngine(make, model string, serial *string, purchasedOn *string, status string, notes *string) (Engine, error) {
	make, err := requiredString(make, maxMakeModel)
	if err != nil {
		return Engine{}, err
	}
	model, err = requiredString(model, maxMakeModel)
	if err != nil {
		return Engine{}, err
	}
	serial, err = optionalString(serial, maxSerial)
	if err != nil {
		return Engine{}, err
	}
	date, err := validDate(purchasedOn)
	if err != nil {
		return Engine{}, err
	}
	notes, err = optionalString(notes, maxNotes)
	if err != nil {
		return Engine{}, err
	}
	if status == "" {
		status = AssetActive
	}
	if !ValidAssetStatus(status) {
		return Engine{}, fmt.Errorf("%w: status", ErrInvalid)
	}
	return Engine{Make: make, Model: model, SerialNumber: serial, PurchasedOn: date, Status: status, Notes: notes}, nil
}

type EngineUpdate struct {
	Make         *string
	Model        *string
	SerialSet    bool
	SerialNumber *string
	PurchasedSet bool
	PurchasedOn  *string
	Status       *string
	NotesSet     bool
	Notes        *string
}

func (u EngineUpdate) Empty() bool {
	return u.Make == nil && u.Model == nil && !u.SerialSet && !u.PurchasedSet && u.Status == nil && !u.NotesSet
}

func ApplyEngineUpdate(e Engine, u EngineUpdate) (Engine, error) {
	if u.Empty() {
		return Engine{}, fmt.Errorf("%w: empty update mask", ErrInvalid)
	}
	var err error
	if u.Make != nil {
		e.Make, err = requiredString(*u.Make, maxMakeModel)
		if err != nil {
			return Engine{}, err
		}
	}
	if u.Model != nil {
		e.Model, err = requiredString(*u.Model, maxMakeModel)
		if err != nil {
			return Engine{}, err
		}
	}
	if u.SerialSet {
		e.SerialNumber, err = optionalString(u.SerialNumber, maxSerial)
		if err != nil {
			return Engine{}, err
		}
	}
	if u.PurchasedSet {
		e.PurchasedOn, err = validDate(u.PurchasedOn)
		if err != nil {
			return Engine{}, err
		}
	}
	if u.Status != nil {
		if !ValidAssetStatus(*u.Status) {
			return Engine{}, fmt.Errorf("%w: status", ErrInvalid)
		}
		e.Status = *u.Status
	}
	if u.NotesSet {
		e.Notes, err = optionalString(u.Notes, maxNotes)
		if err != nil {
			return Engine{}, err
		}
	}
	return e, nil
}
