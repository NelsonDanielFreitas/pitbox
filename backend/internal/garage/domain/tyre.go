package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TyreSet struct {
	ID              uuid.UUID
	Label           string
	Make            string
	Model           string
	Compound        *string
	InsertType      *string
	GluedOn         *time.Time
	Status          string
	SessionsTotal   int32
	RunSecondsTotal int32
	WearNotes       *string
	CreatedAt       time.Time
	Version         int32
}

func NewTyreSet(label, make, model string, compound, insertType *string, gluedOn *string, status string, wearNotes *string) (TyreSet, error) {
	label, err := requiredString(label, maxLabel)
	if err != nil {
		return TyreSet{}, err
	}
	make, err = requiredString(make, maxMakeModel)
	if err != nil {
		return TyreSet{}, err
	}
	model, err = requiredString(model, maxMakeModel)
	if err != nil {
		return TyreSet{}, err
	}
	compound, err = optionalString(compound, maxCompound)
	if err != nil {
		return TyreSet{}, err
	}
	insertType, err = optionalString(insertType, maxCompound)
	if err != nil {
		return TyreSet{}, err
	}
	date, err := validDate(gluedOn)
	if err != nil {
		return TyreSet{}, err
	}
	wearNotes, err = optionalString(wearNotes, maxNotes)
	if err != nil {
		return TyreSet{}, err
	}
	if status == "" {
		status = TyreNew
	}
	if !ValidTyreStatus(status) {
		return TyreSet{}, fmt.Errorf("%w: tyre status", ErrInvalid)
	}
	return TyreSet{Label: label, Make: make, Model: model, Compound: compound, InsertType: insertType, GluedOn: date, Status: status, WearNotes: wearNotes}, nil
}

type TyreUpdate struct {
	Label       *string
	Make        *string
	Model       *string
	CompoundSet bool
	Compound    *string
	InsertSet   bool
	InsertType  *string
	GluedSet    bool
	GluedOn     *string
	Status      *string
	WearSet     bool
	WearNotes   *string
}

func (u TyreUpdate) Empty() bool {
	return u.Label == nil && u.Make == nil && u.Model == nil && !u.CompoundSet && !u.InsertSet &&
		!u.GluedSet && u.Status == nil && !u.WearSet
}

func ApplyTyreUpdate(t TyreSet, u TyreUpdate) (TyreSet, error) {
	if u.Empty() {
		return TyreSet{}, fmt.Errorf("%w: empty update mask", ErrInvalid)
	}
	var err error
	if u.Label != nil {
		t.Label, err = requiredString(*u.Label, maxLabel)
		if err != nil {
			return TyreSet{}, err
		}
	}
	if u.Make != nil {
		t.Make, err = requiredString(*u.Make, maxMakeModel)
		if err != nil {
			return TyreSet{}, err
		}
	}
	if u.Model != nil {
		t.Model, err = requiredString(*u.Model, maxMakeModel)
		if err != nil {
			return TyreSet{}, err
		}
	}
	if u.CompoundSet {
		t.Compound, err = optionalString(u.Compound, maxCompound)
		if err != nil {
			return TyreSet{}, err
		}
	}
	if u.InsertSet {
		t.InsertType, err = optionalString(u.InsertType, maxCompound)
		if err != nil {
			return TyreSet{}, err
		}
	}
	if u.GluedSet {
		t.GluedOn, err = validDate(u.GluedOn)
		if err != nil {
			return TyreSet{}, err
		}
	}
	if u.Status != nil {
		if !ValidTyreStatus(*u.Status) {
			return TyreSet{}, fmt.Errorf("%w: tyre status", ErrInvalid)
		}
		t.Status = *u.Status
	}
	if u.WearSet {
		t.WearNotes, err = optionalString(u.WearNotes, maxNotes)
		if err != nil {
			return TyreSet{}, err
		}
	}
	return t, nil
}
