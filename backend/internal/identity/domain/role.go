package domain

import "fmt"

// Role is a user's role in a team. It mirrors identity.team_role.
type Role string

const (
	RoleOwner    Role = "owner"
	RoleMechanic Role = "mechanic"
	RolePilot    Role = "pilot"
	RoleGuest    Role = "guest"
)

// Valid reports whether r is one of the four roles.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleMechanic, RolePilot, RoleGuest:
		return true
	}
	return false
}

// TeamRelation reports whether the role is mirrored as a relation on the team
// object in OpenFGA. A guest has no team relation (it only sees what is shared
// with it).
func (r Role) TeamRelation() bool {
	return r == RoleOwner || r == RoleMechanic || r == RolePilot
}

// ManageMembers reports whether the role may invite, change roles or remove
// members. Only the owner may.
func (r Role) ManageMembers() bool { return r == RoleOwner }

// ParseRole converts a database or wire value, refusing anything outside the enum.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", fmt.Errorf("%w: role", ErrInvalid)
	}
	return r, nil
}

// AssignableRole refuses an owner target: ownership changes only through a
// two-sided transfer.
func AssignableRole(r Role) error {
	if !r.Valid() {
		return fmt.Errorf("%w: role", ErrInvalid)
	}
	if r == RoleOwner {
		return fmt.Errorf("%w: owner is not assignable", ErrForbidden)
	}
	return nil
}

// TransferTarget reports whether a membership may receive ownership: only a
// mechanic or a pilot.
func TransferTarget(r Role) bool { return r == RoleMechanic || r == RolePilot }
