package principal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsStrongFactor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		amr  []string
		want bool
	}{
		{"passkey", []string{"user"}, true},
		{"totp", []string{"pwd", "otp", "mfa"}, true},
		{"u2f", []string{"pwd", "user"}, true},
		{"generic_mfa_only", []string{"pwd", "mfa"}, false},
		{"password_only", []string{"pwd"}, false},
		{"empty", nil, false},
		{"sms_like_email", []string{"pwd", "mfa", "email"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, IsStrongFactor(tc.amr))
		})
	}
}

func TestRequiresReauthentication(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	t.Run("recent_passkey", func(t *testing.T) {
		t.Parallel()
		p := Principal{AuthTime: now.Add(-2 * time.Minute), AMR: []string{"user"}}
		require.False(t, RequiresReauthentication(p, now))
	})

	t.Run("old_passkey", func(t *testing.T) {
		t.Parallel()
		p := Principal{AuthTime: now.Add(-10 * time.Minute), AMR: []string{"user"}}
		require.True(t, RequiresReauthentication(p, now))
	})

	t.Run("recent_password_only", func(t *testing.T) {
		t.Parallel()
		p := Principal{AuthTime: now.Add(-30 * time.Second), AMR: []string{"pwd"}}
		require.True(t, RequiresReauthentication(p, now))
	})

	t.Run("exactly_at_window", func(t *testing.T) {
		t.Parallel()
		p := Principal{AuthTime: now.Add(-StepUpWindow), AMR: []string{"otp"}}
		require.False(t, RequiresReauthentication(p, now))
	})

	t.Run("one_second_past_window", func(t *testing.T) {
		t.Parallel()
		p := Principal{AuthTime: now.Add(-StepUpWindow - time.Second), AMR: []string{"otp"}}
		require.True(t, RequiresReauthentication(p, now))
	})
}
