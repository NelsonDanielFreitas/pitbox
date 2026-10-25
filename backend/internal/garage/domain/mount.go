package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EngineMount is one interval of an engine in a car.
type EngineMount struct {
	ID          uuid.UUID
	EngineID    uuid.UUID
	CarID       uuid.UUID
	MountedAt   time.Time
	UnmountedAt *time.Time
}

func NewEngineMount(engineID, carID uuid.UUID, mountedAt time.Time) (EngineMount, error) {
	if engineID == uuid.Nil || carID == uuid.Nil || mountedAt.IsZero() {
		return EngineMount{}, fmt.Errorf("%w: engine mount", ErrInvalid)
	}
	return EngineMount{EngineID: engineID, CarID: carID, MountedAt: mountedAt.UTC()}, nil
}

func (m EngineMount) Open() bool { return m.UnmountedAt == nil }
