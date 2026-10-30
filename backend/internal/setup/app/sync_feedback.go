package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// FeedbackApplier is the driver's feedback on a setup version (ADR 0026). It is the one
// entity a pilot may write (syncapply.RoleGated): owner, mechanic and pilot add feedback,
// guests never. It is append only, so it never conflicts.
type FeedbackApplier struct {
	svc *Service
	now func() time.Time
}

func NewFeedbackApplier(svc *Service) *FeedbackApplier {
	return &FeedbackApplier{svc: svc, now: time.Now}
}

var (
	_ syncapply.Applier   = (*FeedbackApplier)(nil)
	_ syncapply.RoleGated = (*FeedbackApplier)(nil)
)

func (*FeedbackApplier) Entity() string { return "setup.setup_feedback" }

func (*FeedbackApplier) CanWrite(role string) bool {
	return role == "owner" || role == "mechanic" || role == "pilot"
}

// CurrentVersion: feedback is never edited, so there is no version to compare.
func (*FeedbackApplier) CurrentVersion(context.Context, db.DBTX, db.TeamID, uuid.UUID) (int32, error) {
	return 0, nil
}

var feedbackScaleKeys = []string{
	"steering_entry", "steering_mid", "steering_exit", "traction", "jump_stability", "braking",
	"engine_low_end", "engine_top_end",
}

const (
	feedbackMaxFuture = 5 * time.Minute
	feedbackMaxAge    = 90 * 24 * time.Hour
)

func (a *FeedbackApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	if op.Kind != syncapply.Append {
		return 0, fmt.Errorf("feedback is append only: %w", syncapply.ErrInvalidFields)
	}
	f := syncapply.Fields(op.Fields)
	allowed := append([]string{"setup_id", "version", "track_zone", "note", "created_at"}, feedbackScaleKeys...)
	if err := f.Only(allowed...); err != nil {
		return 0, err
	}
	setupRaw, err := f.RequiredString("setup_id")
	if err != nil {
		return 0, err
	}
	setupID, err := uuid.Parse(setupRaw)
	if err != nil || setupID == uuid.Nil {
		return 0, fmt.Errorf("setup_id: %w", syncapply.ErrInvalidFields)
	}
	set, version, err := f.Int32("version")
	if err != nil || !set || version == nil {
		return 0, fmt.Errorf("version: %w", syncapply.ErrInvalidFields)
	}
	fb := domain.Feedback{
		ID: op.EntityID, SetupID: setupID, Version: *version, AuthorID: uuid.UUID(op.UserID),
	}
	dst := []**int16{
		&fb.SteeringEntry, &fb.SteeringMid, &fb.SteeringExit, &fb.Traction, &fb.JumpStability,
		&fb.Braking, &fb.EngineLowEnd, &fb.EngineTopEnd,
	}
	var errs []error
	for i, key := range feedbackScaleKeys {
		_, v, err := f.Int32(key)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if v != nil {
			if *v < 1 || *v > 5 {
				errs = append(errs, fmt.Errorf("%s: %w", key, syncapply.ErrInvalidFields))
				continue
			}
			n := int16(*v)
			*dst[i] = &n
		}
	}
	_, zone, err1 := f.String("track_zone")
	_, note, err2 := f.String("note")
	_, createdRaw, err3 := f.String("created_at")
	if err := errors.Join(append(errs, err1, err2, err3)...); err != nil {
		return 0, syncapply.ErrInvalidFields
	}
	fb.TrackZone, fb.Note = zone, note
	now := a.now()
	fb.CreatedAt = now
	if createdRaw != nil {
		t, err := time.Parse(time.RFC3339Nano, *createdRaw)
		if err != nil {
			return 0, fmt.Errorf("created_at: %w", syncapply.ErrInvalidFields)
		}
		// The time the driver wrote it, kept inside the window of every offline time.
		if t.Before(now.Add(feedbackMaxFuture)) && t.After(now.Add(-feedbackMaxAge)) {
			fb.CreatedAt = t.UTC()
		}
	}
	if err := fb.Validate(); err != nil {
		return 0, setupSyncErr(err)
	}
	if err := a.svc.store.CreateFeedbackWithID(ctx, tx, uuid.UUID(op.TeamID), fb); err != nil {
		return 0, setupSyncRefErr(err)
	}
	return 1, nil
}

type feedbackWire struct {
	SetupID       uuid.UUID `json:"setup_id"`
	Version       int32     `json:"version"`
	AuthorID      uuid.UUID `json:"author_id"`
	SteeringEntry *int16    `json:"steering_entry"`
	SteeringMid   *int16    `json:"steering_mid"`
	SteeringExit  *int16    `json:"steering_exit"`
	Traction      *int16    `json:"traction"`
	JumpStability *int16    `json:"jump_stability"`
	Braking       *int16    `json:"braking"`
	EngineLowEnd  *int16    `json:"engine_low_end"`
	EngineTopEnd  *int16    `json:"engine_top_end"`
	TrackZone     *string   `json:"track_zone"`
	Note          *string   `json:"note"`
	CreatedAt     time.Time `json:"created_at"`
}

// Read returns the feedback to every member that can pull. The author is an id only: names
// are resolved in memory by the app (ADR 0020).
func (a *FeedbackApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	fb, err := a.svc.store.GetFeedback(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(feedbackWire{
		SetupID: fb.SetupID, Version: fb.Version, AuthorID: fb.AuthorID,
		SteeringEntry: fb.SteeringEntry, SteeringMid: fb.SteeringMid, SteeringExit: fb.SteeringExit,
		Traction: fb.Traction, JumpStability: fb.JumpStability, Braking: fb.Braking,
		EngineLowEnd: fb.EngineLowEnd, EngineTopEnd: fb.EngineTopEnd,
		TrackZone: fb.TrackZone, Note: fb.Note, CreatedAt: fb.CreatedAt,
	})
	return raw, err == nil, err
}
