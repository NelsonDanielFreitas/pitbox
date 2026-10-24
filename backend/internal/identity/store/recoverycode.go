package store

import (
	"context"
	"fmt"
	"time"
)

// RecoveryCodes persists and consumes single-use MFA recovery codes.
type RecoveryCodes struct{ db Querier }

// NewRecoveryCodes takes the pool of the application role.
func NewRecoveryCodes(db Querier) *RecoveryCodes { return &RecoveryCodes{db: db} }

// Replace deletes all unused codes for the user and inserts the new hashes atomically.
// keyID identifies the active key that produced them.
func (r *RecoveryCodes) Replace(ctx context.Context, userID, keyID string, codeHashes [][]byte) error {
	if len(codeHashes) == 0 || len(codeHashes) > 16 {
		return fmt.Errorf("replace recovery codes: %d hashes", len(codeHashes))
	}
	for i, h := range codeHashes {
		if len(h) != 32 {
			return fmt.Errorf("replace recovery codes: hash %d has length %d, want 32", i, len(h))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := r.db.Exec(ctx, `SELECT identity.replace_recovery_codes($1::uuid, $2, $3)`, userID, codeHashes, keyID)
	if err != nil {
		return fmt.Errorf("replace recovery codes: %w", err)
	}
	return nil
}

// Consume marks one code as used if any candidate hash exists and is unused for the user.
func (r *RecoveryCodes) Consume(ctx context.Context, userID string, codeHashes [][]byte) (bool, error) {
	if len(codeHashes) == 0 || len(codeHashes) > 16 {
		return false, fmt.Errorf("consume recovery code: %d hashes", len(codeHashes))
	}
	for i, h := range codeHashes {
		if len(h) != 32 {
			return false, fmt.Errorf("consume recovery code: hash %d has length %d, want 32", i, len(h))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ok bool
	err := r.db.QueryRow(ctx,
		`SELECT identity.consume_recovery_code($1::uuid, $2)`, userID, codeHashes).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("consume recovery code: %w", err)
	}
	return ok, nil
}
