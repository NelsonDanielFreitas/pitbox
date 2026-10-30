package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxFeedbackZone = 80
	maxFeedbackNote = 2000
)

// Feedback is what the driver felt on one version of a setup (ADR 0026). It is only ever
// appended. A scale is 1..5 and every one is optional, but there must be at least one value
// or a note.
type Feedback struct {
	ID       uuid.UUID
	SetupID  uuid.UUID
	Version  int32
	AuthorID uuid.UUID

	SteeringEntry, SteeringMid, SteeringExit *int16
	Traction, JumpStability, Braking         *int16
	EngineLowEnd, EngineTopEnd               *int16

	TrackZone *string
	Note      *string
	CreatedAt time.Time
}

// Validate normalises the texts (trimmed, empty means absent) and checks the rules. It
// mirrors the CHECK constraints so the database never has to be the first to refuse.
func (f *Feedback) Validate() error {
	scales := []*int16{f.SteeringEntry, f.SteeringMid, f.SteeringExit, f.Traction, f.JumpStability, f.Braking, f.EngineLowEnd, f.EngineTopEnd}
	values := 0
	for _, s := range scales {
		if s == nil {
			continue
		}
		if *s < 1 || *s > 5 {
			return fmt.Errorf("%w: feedback scale", ErrInvalid)
		}
		values++
	}
	var err error
	if f.TrackZone, err = trimOptional(f.TrackZone, maxFeedbackZone); err != nil {
		return err
	}
	if f.Note, err = trimOptional(f.Note, maxFeedbackNote); err != nil {
		return err
	}
	if f.Note != nil {
		values++
	}
	if values == 0 || f.SetupID == uuid.Nil || f.Version < 1 {
		return fmt.Errorf("%w: feedback content", ErrInvalid)
	}
	return nil
}

func trimOptional(s *string, max int) (*string, error) {
	if s == nil {
		return nil, nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(t) > max {
		return nil, fmt.Errorf("%w: feedback text", ErrInvalid)
	}
	return &t, nil
}
