package app

import "context"

// NoopMailer is the F5 placeholder: it accepts the invitation but sends nothing
// and never logs the email or the token. The real provider replaces it in F5.
type NoopMailer struct{}

// Invitation implements Mailer.
func (NoopMailer) Invitation(context.Context, string, string) error { return nil }
