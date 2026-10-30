package domain

import (
	"errors"
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

// Errors the transport turns into public answers (see platform/apperr).
var (
	ErrNotFound    = fmt.Errorf("sync: not found: %w", apperr.ErrNotFound)
	ErrForbidden   = fmt.Errorf("sync: role not allowed: %w", apperr.ErrPermissionDenied)
	ErrInvalid     = fmt.Errorf("sync: invalid value: %w", apperr.ErrInvalidArgument)
	ErrDuplicateOp = errors.New("sync: operation already recorded")
	ErrBusy        = fmt.Errorf("sync: entity busy: %w", apperr.ErrConflict)
	ErrLimit       = fmt.Errorf("sync: too many devices: %w", apperr.ErrFailedPrecondition)
)
