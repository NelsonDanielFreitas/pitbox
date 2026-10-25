package domain

import "time"

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
}
