// Package idempotency stores the request key and response of a write for seven days.
// It is shared by every module; the table is platform.idempotency_key.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/idempotency/sqlcgen"
)

// MaxResponseBytes bounds the stored response. A normal resource snapshot is far smaller.
const MaxResponseBytes = 64 << 10

// ErrResponseTooLarge means the caller tried to store an oversized response.
var ErrResponseTooLarge = errors.New("idempotency: response too large")

// Store reads and writes the shared idempotency table inside the caller's transaction.
type Store struct{}

// NewStore returns the idempotency store.
func NewStore() *Store { return &Store{} }

// Lookup returns a live key's response. A missing key returns found=false. A key from
// another method or with another request hash is INVALID_ARGUMENT: the caller must never
// replay a response for a different request.
func (Store) Lookup(ctx context.Context, tx db.DBTX, userID, requestID uuid.UUID, method string, hash []byte) ([]byte, bool, error) {
	row, err := sqlcgen.New(tx).GetIdempotencyKey(ctx, sqlcgen.GetIdempotencyKeyParams{
		UserID: userID, RequestID: requestID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get idempotency key: %w", err)
	}
	if row.Method != method || !bytes.Equal(row.RequestHash, hash) {
		return nil, false, fmt.Errorf("idempotency request mismatch: %w", apperr.ErrInvalidArgument)
	}
	return row.Response, true, nil
}

// Claim inserts a key, or takes over an expired one. It returns false when a live key
// with the same (user_id, request_id) already exists; the caller must then Lookup and
// replay it.
func (Store) Claim(ctx context.Context, tx db.DBTX, requestID, userID uuid.UUID, method string, hash []byte) (bool, error) {
	_, err := sqlcgen.New(tx).InsertIdempotencyKey(ctx, sqlcgen.InsertIdempotencyKeyParams{
		RequestID: requestID, UserID: userID, Method: method, RequestHash: hash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim idempotency key: %w", err)
	}
	return true, nil
}

// Save stores the response of the operation. The caller must have claimed the key.
func (Store) Save(ctx context.Context, tx db.DBTX, userID, requestID uuid.UUID, response []byte) error {
	if len(response) > MaxResponseBytes {
		return ErrResponseTooLarge
	}
	if err := sqlcgen.New(tx).SetIdempotencyResponse(ctx, sqlcgen.SetIdempotencyResponseParams{
		UserID: userID, RequestID: requestID, Response: response,
	}); err != nil {
		return fmt.Errorf("save idempotency response: %w", err)
	}
	return nil
}

// Hash returns the SHA-256 of the canonical JSON of a request DTO. Callers must include
// team_id and every field that changes the result.
func Hash(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("hash idempotent request: %w", err)
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}
