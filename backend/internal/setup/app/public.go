package app

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// OpenShareLink is the only public setup use case. It has no principal and no
// team context; the database function is the sole reader.
func (s *Service) OpenShareLink(ctx context.Context, token string) (domain.SharedSetup, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hash := sha256.Sum256([]byte(token))
	out, err := s.shares.Open(ctx, hash[:])
	if err != nil {
		return domain.SharedSetup{}, err
	}
	stripReferences(out.Data)
	return out, nil
}

func stripReferences(data map[string]any) {
	if engine, ok := data["engine"].(map[string]any); ok {
		delete(engine, "engine_id")
	}
	if tyres, ok := data["tyres"].(map[string]any); ok {
		delete(tyres, "tyre_set_id")
	}
}
