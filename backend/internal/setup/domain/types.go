package domain

import (
	"time"

	"github.com/google/uuid"
)

// Setup is the live identity of a versioned setup sheet.
type Setup struct {
	ID             string
	CarID          string
	Name           string
	CurrentVersion int32
	Notes          *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int32
}

// VersionRef identifies one immutable version without its data.
type VersionRef struct {
	SetupID        string
	Version        int32
	SchemaVersion  int32
	BasedOnSetupID *string
	BasedOnVersion *int32
	ChangeSummary  *string
	CreatedAt      time.Time
}

// Version is one immutable snapshot.
type Version struct {
	Ref  VersionRef
	Data map[string]any
}

// DifferenceKind says how a field changed.
type DifferenceKind string

const (
	DifferenceAdded   DifferenceKind = "added"
	DifferenceRemoved DifferenceKind = "removed"
	DifferenceChanged DifferenceKind = "changed"
)

// Difference is one leaf difference between two versions.
type Difference struct {
	Path   string
	Before any
	After  any
	Kind   DifferenceKind
}

// ShareExpiry is the requested lifetime of a public link.
type ShareExpiry string

const (
	ShareExpiry24h   ShareExpiry = "24h"
	ShareExpiry7d    ShareExpiry = "7d"
	ShareExpiryNever ShareExpiry = "never"
)

// ValidShareExpiry reports whether the expiry is one of the public options.
func ValidShareExpiry(v ShareExpiry) bool {
	return v == ShareExpiry24h || v == ShareExpiry7d || v == ShareExpiryNever
}

// ShareLink is the listable metadata of a public link. It never contains the
// token or its hash.
type ShareLink struct {
	ID            string
	SetupID       string
	PinnedVersion *int32
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
	LastUsedAt    *time.Time
	UseCount      int32
	CreatedAt     time.Time
}

// SharedSetup is the public view through a link.
type SharedSetup struct {
	Name             string
	Version          int32
	SchemaVersion    int32
	Data             map[string]any
	ChangeSummary    *string
	VersionCreatedAt time.Time
	// TemplateDefinition is the definition of the template version a v2 version was made with.
	TemplateDefinition map[string]any
}

// SheetTemplateInfo is the identity of a sheet template (ADR 0030). Global means the
// platform catalogue (no team); a team template is a copy a mechanic owns.
type SheetTemplateInfo struct {
	ID             uuid.UUID
	Global         bool
	Slug           string
	Make           string
	Model          string
	Scale          string
	Powertrain     string
	Name           string
	SourceURL      *string
	CurrentVersion int32
	BasedOnID      *uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int32
}

// SheetTemplateVersionRow is one stored, immutable template version. Definition is the raw
// JSON; callers parse it with ParseTemplate before trusting it.
type SheetTemplateVersionRow struct {
	TemplateID uuid.UUID
	Version    int32
	Definition []byte
	CreatedAt  time.Time
	// Global is true for the platform catalogue: the same for every team, so it can be cached.
	Global bool
}
