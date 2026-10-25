package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/idempotency"
)

var maxCursorUUID = uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")

type envelope[T any] struct {
	V        int `json:"v"`
	Resource T   `json:"resource"`
}

func hashInput(team uuid.UUID, in any) ([]byte, error) {
	return idempotency.Hash(struct {
		TeamID uuid.UUID `json:"team_id"`
		Input  any       `json:"input"`
	}{TeamID: team, Input: in})
}

func (s *Service) replay(ctx context.Context, tx db.DBTX, user, requestID uuid.UUID, method string, hash []byte) ([]byte, bool, error) {
	response, found, err := s.idempotency.Lookup(ctx, tx, user, requestID, method, hash)
	if err != nil {
		if errors.Is(err, apperr.ErrInvalidArgument) {
			return nil, false, fmt.Errorf("idempotent replay: %w", domain.ErrInvalid)
		}
		return nil, false, err
	}
	return response, found, nil
}

func (s *Service) claimOrReplay(ctx context.Context, tx db.DBTX, user, requestID uuid.UUID, method string, hash []byte) ([]byte, bool, error) {
	claimed, err := s.idempotency.Claim(ctx, tx, requestID, user, method, hash)
	if err != nil {
		return nil, false, err
	}
	if claimed {
		return nil, false, nil
	}
	response, found, err := s.replay(ctx, tx, user, requestID, method, hash)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, errors.New("idempotency: lost claim without a live key")
	}
	return response, true, nil
}

func encodeResource[T any](v T) ([]byte, error) {
	return json.Marshal(envelope[T]{V: 1, Resource: v})
}

func decodeResource[T any](raw []byte) (T, error) {
	var env envelope[T]
	if len(raw) == 0 || json.Unmarshal(raw, &env) != nil || env.V != 1 {
		var zero T
		return zero, errors.New("idempotency: invalid stored response")
	}
	return env.Resource, nil
}

func (s *Service) saveResource(ctx context.Context, tx db.DBTX, user, requestID uuid.UUID, v any) error {
	raw, err := encodeResource(v)
	if err != nil {
		return err
	}
	return s.idempotency.Save(ctx, tx, user, requestID, raw)
}
