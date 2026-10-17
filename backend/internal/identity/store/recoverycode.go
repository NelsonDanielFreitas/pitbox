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
func (r *RecoveryCodes) Replace(ctx context.Context, userID string, codeHashes [][]byte) error {
	for i, h := range codeHashes {
		if len(h) != 32 {
			return fmt.Errorf("replace recovery codes: hash %d has length %d, want 32", i, len(h))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := r.db.Exec(ctx, `SELECT identity.replace_recovery_codes($1::uuid, $2)`, userID, codeHashes)
	if err != nil {
		return fmt.Errorf("replace recovery codes: %w", err)
	}
	return nil
}

// Consume marks one code as used if it exists and is unused for the user.
func (r *RecoveryCodes) Consume(ctx context.Context, userID string, codeHash []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ok bool
	err := r.db.QueryRow(ctx,
		`SELECT identity.consume_recovery_code($1::uuid, $2)`, userID, codeHash).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("consume recovery code: %w", err)
	}
	return ok, nil
}
