package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	RemovalStock = "stock"
	RemovalScrap = "scrap"
	RemovalSpare = "spare"

	ReasonBroken = "broken"
	ReasonWorn   = "worn"
	ReasonLost   = "lost"

	// UnitCenti is one unit of a part in hundredths: each installation is one unit.
	UnitCenti = 100
)

// PartInstall is one installation of one unit of a part in a car or an engine.
// RunSeconds and Sessions stay at zero until usage accounting (F2-04).
type PartInstall struct {
	ID                 uuid.UUID
	PartID             uuid.UUID
	CarID              *uuid.UUID
	EngineID           *uuid.UUID
	InstalledAt        time.Time
	RemovedAt          *time.Time
	RemovalDestination string
	RemovalReason      string
	RunSeconds         int32
	Sessions           int32
	Version            int32
}

func (i PartInstall) Active() bool { return i.RemovedAt == nil }

// NewInstall is a validated request to install a part in exactly one target.
type NewInstall struct {
	PartID      uuid.UUID
	CarID       *uuid.UUID
	EngineID    *uuid.UUID
	InstalledAt *time.Time
}

func NewPartInstall(partID uuid.UUID, carID, engineID *uuid.UUID, installedAt *time.Time, now time.Time) (NewInstall, error) {
	if partID == uuid.Nil || (carID == nil) == (engineID == nil) {
		return NewInstall{}, fmt.Errorf("%w: install target", ErrInvalid)
	}
	if (carID != nil && *carID == uuid.Nil) || (engineID != nil && *engineID == uuid.Nil) {
		return NewInstall{}, fmt.Errorf("%w: install target", ErrInvalid)
	}
	out := NewInstall{PartID: partID, CarID: carID, EngineID: engineID}
	if installedAt != nil {
		if installedAt.IsZero() || installedAt.After(now) {
			return NewInstall{}, fmt.Errorf("%w: install time", ErrInvalid)
		}
		t := installedAt.UTC()
		out.InstalledAt = &t
	}
	return out, nil
}

// Removal says where a removed unit goes. A reason is required exactly for scrap.
type Removal struct {
	Destination string
	Reason      string
}

func NewRemoval(destination, reason string) (Removal, error) {
	switch destination {
	case RemovalStock, RemovalSpare:
		if reason != "" {
			return Removal{}, fmt.Errorf("%w: removal reason", ErrInvalid)
		}
	case RemovalScrap:
		if reason != ReasonBroken && reason != ReasonWorn && reason != ReasonLost {
			return Removal{}, fmt.Errorf("%w: removal reason", ErrInvalid)
		}
	default:
		return Removal{}, fmt.Errorf("%w: removal destination", ErrInvalid)
	}
	return Removal{Destination: destination, Reason: reason}, nil
}

// InstallFilter selects installs by exactly one of part, car or engine.
type InstallFilter struct {
	PartID   *uuid.UUID
	CarID    *uuid.UUID
	EngineID *uuid.UUID
}

func (f InstallFilter) Valid() bool {
	n := 0
	for _, id := range []*uuid.UUID{f.PartID, f.CarID, f.EngineID} {
		if id != nil {
			if *id == uuid.Nil {
				return false
			}
			n++
		}
	}
	return n == 1
}
