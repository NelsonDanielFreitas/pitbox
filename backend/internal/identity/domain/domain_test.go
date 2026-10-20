package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

func TestNewTeam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		teamName    string
		category    string
		club        *string
		country     string
		wantName    string
		wantCat     string
		wantClub    *string
		wantInvalid bool
	}{
		{name: "trims and defaults", teamName: "  Equipa  ", category: "", country: "PT", wantName: "Equipa", wantCat: DefaultCategory},
		{name: "club trimmed", teamName: "E", category: "1/8", club: ptr("  Clube  "), country: "PT", wantName: "E", wantCat: "1/8", wantClub: ptr("Clube")},
		{name: "empty club becomes nil", teamName: "E", category: "1/8", club: ptr("   "), country: "PT", wantName: "E", wantCat: "1/8"},
		{name: "empty name", teamName: "  ", category: "1/8", country: "PT", wantInvalid: true},
		{name: "name too long", teamName: strings.Repeat("á", 81), category: "1/8", country: "PT", wantInvalid: true},
		{name: "name 80 characters", teamName: strings.Repeat("á", 80), category: "1/8", country: "PT", wantName: strings.Repeat("á", 80), wantCat: "1/8"},
		{name: "category too long", teamName: "E", category: strings.Repeat("c", 41), country: "PT", wantInvalid: true},
		{name: "unknown country", teamName: "E", category: "1/8", country: "ZZ", wantInvalid: true},
		{name: "lower case country", teamName: "E", category: "1/8", country: "pt", wantInvalid: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewTeam(tc.teamName, tc.category, tc.club, tc.country)
			if tc.wantInvalid {
				require.ErrorIs(t, err, ErrInvalid)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantName, got.Name)
			require.Equal(t, tc.wantCat, got.Category)
			require.Equal(t, tc.wantClub, got.Club)
			require.Equal(t, tc.country, got.CountryCode)
		})
	}
}

func TestApplyTeamUpdate(t *testing.T) {
	t.Parallel()

	base := Team{Name: "Old", Category: "1/8", Club: ptr("Clube"), CountryCode: "PT", Version: 3}
	name, category, country, club := "New", "1/10", "ES", ""

	t.Run("only club may be cleared", func(t *testing.T) {
		got, err := ApplyTeamUpdate(base, TeamUpdate{ClubSet: true})
		require.NoError(t, err)
		require.Nil(t, got.Club)
	})
	t.Run("name cannot be cleared", func(t *testing.T) {
		_, err := ApplyTeamUpdate(base, TeamUpdate{Name: ptr("  ")})
		require.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("category cannot be cleared", func(t *testing.T) {
		_, err := ApplyTeamUpdate(base, TeamUpdate{Category: ptr("")})
		require.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("multiple fields", func(t *testing.T) {
		got, err := ApplyTeamUpdate(base, TeamUpdate{Name: &name, Category: &category, CountryCode: &country, ClubSet: true, Club: &club})
		require.NoError(t, err)
		require.Equal(t, "New", got.Name)
		require.Equal(t, "1/10", got.Category)
		require.Equal(t, "ES", got.CountryCode)
		require.Nil(t, got.Club)
		require.Equal(t, int32(3), got.Version)
	})
	t.Run("empty mask", func(t *testing.T) {
		_, err := ApplyTeamUpdate(base, TeamUpdate{})
		require.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("unknown country", func(t *testing.T) {
		zz := "ZZ"
		_, err := ApplyTeamUpdate(base, TeamUpdate{CountryCode: &zz})
		require.ErrorIs(t, err, ErrInvalid)
	})
}

func TestRoles(t *testing.T) {
	t.Parallel()

	require.True(t, RoleOwner.TeamRelation())
	require.True(t, RoleMechanic.TeamRelation())
	require.True(t, RolePilot.TeamRelation())
	require.False(t, RoleGuest.TeamRelation())
	require.NoError(t, AssignableRole(RoleMechanic))
	require.ErrorIs(t, AssignableRole(RoleOwner), ErrForbidden)
	require.ErrorIs(t, AssignableRole(Role("admin")), ErrInvalid)
	require.True(t, TransferTarget(RoleMechanic))
	require.True(t, TransferTarget(RolePilot))
	require.False(t, TransferTarget(RoleGuest))
	require.False(t, TransferTarget(RoleOwner))
}

func TestInvitationToken(t *testing.T) {
	t.Parallel()

	token, hash, err := NewInvitationToken()
	require.NoError(t, err)
	require.Len(t, token, TokenLength)
	require.Len(t, hash, 32)
	got, err := HashInvitationToken(token)
	require.NoError(t, err)
	require.Equal(t, hash, got)

	// A different token hashes differently; a tampered token is not accepted.
	other, _, err := NewInvitationToken()
	require.NoError(t, err)
	require.NotEqual(t, token, other)
	for _, bad := range []string{"", "short", token + "x", strings.Repeat("!", TokenLength)} {
		_, err := HashInvitationToken(bad)
		require.ErrorIs(t, err, ErrInvalidInvitation, "token %q", bad)
	}
}

func TestInvitationExpiryAndEmail(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	require.Equal(t, now.Add(7*24*time.Hour), InvitationExpiry(now))

	got, err := InvitationEmail(nil)
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = InvitationEmail(ptr("  A@Example.PT "))
	require.NoError(t, err)
	require.Equal(t, "a@example.pt", *got)

	_, err = InvitationEmail(ptr(""))
	require.ErrorIs(t, err, ErrInvalid)
	_, err = InvitationEmail(ptr("no-at-sign"))
	require.ErrorIs(t, err, ErrInvalid)
}

func TestTransferPending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tr := OwnershipTransfer{ExpiresAt: now.Add(time.Hour)}
	require.True(t, tr.Pending(now))
	require.False(t, tr.Pending(now.Add(2*time.Hour)))
	confirmed := tr
	nowPtr := now
	confirmed.ConfirmedAt = &nowPtr
	require.False(t, confirmed.Pending(now))
}

func TestErrorsCarryThePublicSentinel(t *testing.T) {
	t.Parallel()
	require.True(t, errors.Is(ErrInvalidInvitation, apperr.ErrNotFound))
	require.True(t, errors.Is(ErrOwnerRequired, apperr.ErrFailedPrecondition))
	require.True(t, errors.Is(ErrTransferTarget, apperr.ErrFailedPrecondition))
	require.True(t, errors.Is(ErrConflict, apperr.ErrConflict))
	require.True(t, errors.Is(ErrForbidden, apperr.ErrPermissionDenied))
	require.True(t, errors.Is(ErrInvalid, apperr.ErrInvalidArgument))
}

func ptr[T any](v T) *T { return &v }

// FuzzHashInvitationToken: an arbitrary token is either INVALID or hashes to 32 bytes.
func FuzzHashInvitationToken(f *testing.F) {
	f.Add("")
	f.Add("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	f.Fuzz(func(t *testing.T, token string) {
		hash, err := HashInvitationToken(token)
		if err != nil && !errors.Is(err, ErrInvalidInvitation) {
			t.Fatalf("unexpected error: %v", err)
		}
		if err == nil && len(hash) != 32 {
			t.Fatalf("hash length = %d, want 32", len(hash))
		}
	})
}
