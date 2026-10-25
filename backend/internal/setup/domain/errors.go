// Package domain holds the pure setup rules: schema v1, comparison and the
// entities. It has no database, proto or transport dependency.
package domain

import (
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

var (
	ErrNotFound      = fmt.Errorf("setup: not found: %w", apperr.ErrNotFound)
	ErrConflict      = fmt.Errorf("setup: version conflict: %w", apperr.ErrConflict)
	ErrForbidden     = fmt.Errorf("setup: role not allowed: %w", apperr.ErrPermissionDenied)
	ErrInvalid       = fmt.Errorf("setup: invalid value: %w", apperr.ErrInvalidArgument)
	ErrShareLinkFull = fmt.Errorf("setup: too many active share links: %w", apperr.ErrFailedPrecondition)
)

// ValidationError is a schema v1 violation. It carries a stable path and rule
// id and never the rejected value.
type ValidationError struct {
	Path   string
	RuleID string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("setup: invalid data at %s (%s)", e.Path, e.RuleID)
}

func (e *ValidationError) Unwrap() error { return ErrInvalid }

const (
	RuleSchemaVersion = "setup.schema_version"
	RuleSize          = "setup.data.size"
	RuleDepth         = "setup.data.depth"
	RuleUnknown       = "setup.data.unknown"
	RuleType          = "setup.data.type"
	RuleRange         = "setup.data.range"
	RuleLength        = "setup.data.length"
	RuleUUID          = "setup.data.uuid"
	RuleBarsConflict  = "setup.data.bars_conflict"
)
