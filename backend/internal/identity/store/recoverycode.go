package store

import (
	"context"
	"errors"
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

// CountUnused returns how many unused recovery codes the user has.
func (r *RecoveryCodes) CountUnused(ctx context.Context, userID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var n int
	if err := r.db.QueryRow(ctx, `SELECT identity.count_unused_recovery_codes($1::uuid)`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return n, nil
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

// RecoveryAttempt is the outcome of trying a recovery code for an account known only by its
// identity provider subject.
type RecoveryAttempt string

// The fixed outcomes of identity.attempt_recovery_by_subject.
const (
	AttemptUnknownUser RecoveryAttempt = "unknown_user"
	AttemptNoCodes     RecoveryAttempt = "no_codes"
	AttemptInvalid     RecoveryAttempt = "invalid"
	AttemptConsumed    RecoveryAttempt = "consumed"
)

// AttemptBySubject tries the candidate hashes (none is allowed) for the account with this
// subject. A result outside the fixed set is an error, never a guess.
func (r *RecoveryCodes) AttemptBySubject(ctx context.Context, subject string, codeHashes [][]byte) (RecoveryAttempt, error) {
	if len(codeHashes) > 16 {
		return "", fmt.Errorf("attempt recovery: %d hashes", len(codeHashes))
	}
	for i, h := range codeHashes {
		if len(h) != 32 {
			return "", fmt.Errorf("attempt recovery: hash %d has length %d, want 32", i, len(h))
		}
	}
	if codeHashes == nil {
		codeHashes = [][]byte{} // an empty array, not NULL
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out string
	if err := r.db.QueryRow(ctx, `SELECT identity.attempt_recovery_by_subject($1, $2)`, subject, codeHashes).Scan(&out); err != nil {
		return "", fmt.Errorf("attempt recovery: %w", err)
	}
	switch got := RecoveryAttempt(out); got {
	case AttemptUnknownUser, AttemptNoCodes, AttemptInvalid, AttemptConsumed:
		return got, nil
	}
	return "", errors.New("attempt recovery: unexpected outcome")
}

// InvalidateBySubject spends every unused code of the account with this subject.
func (r *RecoveryCodes) InvalidateBySubject(ctx context.Context, subject string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var n int
	if err := r.db.QueryRow(ctx, `SELECT identity.invalidate_recovery_codes_by_subject($1)`, subject).Scan(&n); err != nil {
		return fmt.Errorf("invalidate recovery codes: %w", err)
	}
	return nil
}
