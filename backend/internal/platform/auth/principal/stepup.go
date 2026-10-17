package principal

import (
	"errors"
	"time"
)

// StepUpWindow is the maximum age of authentication for sensitive operations.
// Operations that change credentials, MFA or account deletion require a fresh
// authentication within this window.
const StepUpWindow = 5 * time.Minute

// ErrStepUp is returned when a sensitive operation requires recent
// reauthentication with a strong factor.
var ErrStepUp = errors.New("auth: step-up required")

// IsStrongFactor reports whether amr contains a phishing-resistant or
// possession-based factor that is suitable for step-up. It deliberately does
// not trust the generic "mfa" value because some identity providers (including
// Zitadel with SMS/email) include it for weaker factors.
//
// Recognized strong values:
//   - "user": user presence test (passkey, U2F, WebAuthn);
//   - "otp":  time-based one-time password (TOTP).
func IsStrongFactor(amr []string) bool {
	for _, v := range amr {
		switch v {
		case "user", "otp":
			return true
		}
	}
	return false
}

// RequiresReauthentication reports whether p needs step-up for sensitive
// operations. It returns true when the authentication is older than
// StepUpWindow or when no strong factor was used.
func RequiresReauthentication(p Principal, now time.Time) bool {
	if !IsStrongFactor(p.AMR) {
		return true
	}
	return now.Sub(p.AuthTime) > StepUpWindow
}
