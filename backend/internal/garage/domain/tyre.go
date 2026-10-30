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
	// TyrePartID and InsertPartID are the stock parts the set was glued from (nil when it
	// was created already glued).
	TyrePartID   *uuid.UUID
	InsertPartID *uuid.UUID
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

const (
	// DefaultGlueQty is the number of tyres and of inserts in a set (four wheels).
	DefaultGlueQty = 4
	// MaxGlueQty bounds one gluing; the contract enforces the same limit.
	MaxGlueQty = 16
)

// GlueInput is what the mechanic chooses when gluing a set from stock. Zero quantities mean
// DefaultGlueQty; nil text fields fall back to what the parts say.
type GlueInput struct {
	TyrePartID   uuid.UUID
	InsertPartID uuid.UUID
	TyreQty      int32
	InsertQty    int32
	Label        string
	Make         *string
	Model        *string
	Compound     *string
	InsertType   *string
	GluedOn      *string
	Note         *string
}

// Glue is the outcome of gluing: the new set and the two stock movements that pay for it.
// All three are written in one transaction.
type Glue struct {
	Set            TyreSet
	TyreMovement   StockMovement
	InsertMovement StockMovement
}

// Normalize applies the default quantities and rejects the combinations that can never work,
// so the use case fails before it opens a transaction.
func (in GlueInput) Normalize() (GlueInput, error) {
	if in.TyreQty == 0 {
		in.TyreQty = DefaultGlueQty
	}
	if in.InsertQty == 0 {
		in.InsertQty = DefaultGlueQty
	}
	if in.TyreQty < 1 || in.TyreQty > MaxGlueQty || in.InsertQty < 1 || in.InsertQty > MaxGlueQty {
		return GlueInput{}, fmt.Errorf("%w: glue quantity", ErrInvalid)
	}
	if in.TyrePartID == uuid.Nil || in.InsertPartID == uuid.Nil || in.TyrePartID == in.InsertPartID {
		return GlueInput{}, fmt.Errorf("%w: glue parts", ErrInvalid)
	}
	return in, nil
}

// NewGlue builds the set and the movements from the two locked parts (with their current
// stock). The set takes make and model from the tyre part and the compound and insert type
// from the specs, unless the request overrides them. A short stock is refused: gluing is a
// deliberate act at the bench, not an offline record of something that already happened.
func NewGlue(in GlueInput, tyre, insert Part, now time.Time) (Glue, error) {
	in, err := in.Normalize()
	if err != nil {
		return Glue{}, err
	}
	if tyre.ID != in.TyrePartID || insert.ID != in.InsertPartID || tyre.Kind != PartTyre || insert.Kind != PartInsert {
		return Glue{}, fmt.Errorf("%w: glue part kind", ErrInvalid)
	}
	tyreCenti, insertCenti := int64(in.TyreQty)*UnitCenti, int64(in.InsertQty)*UnitCenti
	if tyre.QtyCenti < tyreCenti || insert.QtyCenti < insertCenti {
		return Glue{}, ErrInsufficientStock
	}
	make, model := in.Make, in.Model
	if make == nil {
		make = tyre.Brand
	}
	if model == nil {
		model = &tyre.Description
	}
	if make == nil {
		return Glue{}, fmt.Errorf("%w: tyre make", ErrInvalid)
	}
	compound := in.Compound
	if compound == nil {
		compound = specText(tyre.Specs, "compound")
	}
	insertType := in.InsertType
	if insertType == nil {
		insertType = specText(insert.Specs, "insert_type")
	}
	gluedOn := in.GluedOn
	if gluedOn == nil {
		today := now.UTC().Format(time.DateOnly)
		gluedOn = &today
	}
	set, err := NewTyreSet(in.Label, *make, *model, compound, insertType, gluedOn, TyreNew, nil)
	if err != nil {
		return Glue{}, err
	}
	set.TyrePartID, set.InsertPartID = &in.TyrePartID, &in.InsertPartID
	tyreMove, err := NewStockMovement(tyre.ID, -tyreCenti, StockConsumed, in.Note, now, MovementDetails{})
	if err != nil {
		return Glue{}, err
	}
	insertMove, err := NewStockMovement(insert.ID, -insertCenti, StockConsumed, in.Note, now, MovementDetails{})
	if err != nil {
		return Glue{}, err
	}
	return Glue{Set: set, TyreMovement: tyreMove, InsertMovement: insertMove}, nil
}

// specText returns the text value of a spec key, or nil when it is missing or not text.
func specText(specs map[string]any, key string) *string {
	if s, ok := specs[key].(string); ok && s != "" {
		return &s
	}
	return nil
}
