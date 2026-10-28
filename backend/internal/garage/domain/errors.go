// Package domain holds the garage entities and rules. No infrastructure.
package domain

import (
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

var (
	ErrNotFound        = fmt.Errorf("garage: not found: %w", apperr.ErrNotFound)
	ErrConflict        = fmt.Errorf("garage: version conflict: %w", apperr.ErrConflict)
	ErrForbidden       = fmt.Errorf("garage: role not allowed: %w", apperr.ErrPermissionDenied)
	ErrInvalid         = fmt.Errorf("garage: invalid value: %w", apperr.ErrInvalidArgument)
	ErrAlreadyExists   = fmt.Errorf("garage: already exists: %w", apperr.ErrAlreadyExists)
	ErrMountConflict   = fmt.Errorf("garage: engine or car already mounted: %w", apperr.ErrFailedPrecondition)
	ErrInstallConflict = fmt.Errorf("garage: part cannot be installed or removed: %w", apperr.ErrFailedPrecondition)
	ErrCatalogInactive = fmt.Errorf("garage: catalogue entry is inactive: %w", apperr.ErrFailedPrecondition)
	ErrOpenMount       = fmt.Errorf("garage: resource has an open engine mount: %w", apperr.ErrFailedPrecondition)
)

const (
	AssetActive  = "active"
	AssetSpare   = "spare"
	AssetRetired = "retired"

	TyreNew     = "new"
	TyreUsed    = "used"
	TyreWorn    = "worn"
	TyreRetired = "retired"
)

func ValidAssetStatus(s string) bool {
	return s == AssetActive || s == AssetSpare || s == AssetRetired
}

func ValidTyreStatus(s string) bool {
	return s == TyreNew || s == TyreUsed || s == TyreWorn || s == TyreRetired
}
