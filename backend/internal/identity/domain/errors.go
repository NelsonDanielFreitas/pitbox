// Package domain holds the identity module's entities and rules. No infrastructure.
package domain

import "errors"

var (
	// ErrEmailInUse: the email belongs to another account. Accounts are never linked by email.
	ErrEmailInUse = errors.New("identity: email belongs to another account")
	// ErrAccountDeleted: the account was deleted and cannot sign in.
	ErrAccountDeleted = errors.New("identity: account deleted")
	// ErrInvalidLogin: the subject or the email is not acceptable.
	ErrInvalidLogin = errors.New("identity: invalid login data")
)
