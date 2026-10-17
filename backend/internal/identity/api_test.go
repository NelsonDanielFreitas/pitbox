package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
)

type failingRow struct{ err error }

func (r failingRow) Scan(...any) error { return r.err }

type fakeDB struct{ err error }

func (f fakeDB) QueryRow(context.Context, string, ...any) pgx.Row { return failingRow(f) }
func (f fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}

func TestLoginDirectoryMapsTheFunctionErrors(t *testing.T) {
	t.Parallel()

	infra := errors.New("connection reset")
	tests := []struct {
		name string
		err  error
		is   error
	}{
		{"email in use", &pgconn.PgError{Code: "PB001"}, auth.ErrEmailInUse},
		{"account deleted", &pgconn.PgError{Code: "PB002"}, auth.ErrAccountDeleted},
		// Data the identity provider should never send is a failure, not a refusal of the person.
		{"invalid input", &pgconn.PgError{Code: "22023"}, domain.ErrInvalidLogin},
		{"infrastructure", infra, infra},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := identity.NewLoginDirectory(fakeDB{tc.err}).ResolveLogin(context.Background(), "sub", "a@b.pt", "A")
			if !errors.Is(err, tc.is) {
				t.Fatalf("err = %v, want %v", err, tc.is)
			}
			refusal := errors.Is(err, auth.ErrEmailInUse) || errors.Is(err, auth.ErrAccountDeleted)
			wantRefusal := errors.Is(tc.is, auth.ErrEmailInUse) || errors.Is(tc.is, auth.ErrAccountDeleted)
			if refusal != wantRefusal {
				t.Errorf("refusal = %v, want %v", refusal, wantRefusal)
			}
		})
	}
}
