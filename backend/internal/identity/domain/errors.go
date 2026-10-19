// Package domain holds the identity module's entities and rules. No infrastructure.
package domain

import (
	"errors"
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

var (
	// ErrEmailInUse: the email belongs to another account. Accounts are never linked by email.
	ErrEmailInUse = errors.New("identity: email belongs to another account")
	// ErrAccountDeleted: the account was deleted and cannot sign in.
	ErrAccountDeleted = errors.New("identity: account deleted")
	// ErrInvalidLogin: the subject or the email is not acceptable.
	ErrInvalidLogin = errors.New("identity: invalid login data")

	// ErrInvalidInvitation is the only answer for an invitation that does not
	// exist, expired, was revoked, was already used or has a tampered token. One
	// error for all of them means the response cannot distinguish them
	// (apperr.ErrNotFound → NOT_FOUND).
	ErrInvalidInvitation = fmt.Errorf("identity: invalid invitation: %w", apperr.ErrNotFound)
	// ErrOwnerRequired: the last owner cannot leave, be removed or be demoted.
	ErrOwnerRequired = fmt.Errorf("identity: the team would be left without an owner: %w", apperr.ErrFailedPrecondition)
	// ErrInvitationLimit: a team may have at most 50 pending invitations.
	ErrInvitationLimit = fmt.Errorf("identity: too many pending invitations: %w", apperr.ErrFailedPrecondition)
	// ErrTransferInvalid: the transfer does not exist, expired, was cancelled or
	// was already confirmed.
	ErrTransferInvalid = fmt.Errorf("identity: invalid ownership transfer: %w", apperr.ErrNotFound)
	// ErrConflict: the row changed since the client read it (optimistic concurrency).
	ErrConflict = fmt.Errorf("identity: version conflict: %w", apperr.ErrConflict)
	// ErrForbidden: the role does not allow the action (a domain rule independent
	// of OpenFGA).
	ErrForbidden = fmt.Errorf("identity: role not allowed: %w", apperr.ErrPermissionDenied)
	// ErrInvalid: a value the contract could not refuse is not acceptable.
	ErrInvalid = fmt.Errorf("identity: invalid value: %w", apperr.ErrInvalidArgument)
)
