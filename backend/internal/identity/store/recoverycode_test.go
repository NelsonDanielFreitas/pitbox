package store_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

func TestRecoveryCodesAgainstPostgres(t *testing.T) {
	t.Parallel()
	pg := testutil.StartPostgres(t)
	pool, err := db.Open(context.Background(), pg.URL)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)

	logins := store.NewLogins(pool)
	codes := store.NewRecoveryCodes(pool)
	ctx := context.Background()

	userID, err := logins.ResolveLogin(ctx, "sub-rc", "rc@exemplo.pt", "RC")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	key := sha256.Sum256([]byte("recovery code test key"))
	kr, err := auth.NewKeyring([]config.AuthKey{{ID: "k1", Secret: key[:]}})
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	hasher, err := kr.RecoveryCodeHasher()
	if err != nil {
		t.Fatalf("RecoveryCodeHasher: %v", err)
	}

	hash := func(s string) []byte {
		sum := sha256.Sum256([]byte(s))
		return sum[:]
	}

	t.Run("replace and consume a code", func(t *testing.T) {
		if err := codes.Replace(ctx, userID, "k1", [][]byte{hash("A"), hash("B")}); err != nil {
			t.Fatalf("replace: %v", err)
		}
		ok, err := codes.Consume(ctx, userID, [][]byte{hash("wrong"), hash("A")})
		if err != nil {
			t.Fatalf("consume: %v", err)
		}
		if !ok {
			t.Fatal("expected code A to be consumed")
		}
		ok, err = codes.Consume(ctx, userID, [][]byte{hash("A")})
		if err != nil {
			t.Fatalf("consume again: %v", err)
		}
		if ok {
			t.Fatal("code A must be single-use")
		}
	})

	t.Run("replace removes previous unused codes", func(t *testing.T) {
		if err := codes.Replace(ctx, userID, "k2", [][]byte{hash("C")}); err != nil {
			t.Fatalf("replace: %v", err)
		}
		// B from the previous set must be gone because it was unused.
		ok, err := codes.Consume(ctx, userID, [][]byte{hash("B")})
		if err != nil {
			t.Fatalf("consume: %v", err)
		}
		if ok {
			t.Fatal("previous unused code B must be removed")
		}
		ok, err = codes.Consume(ctx, userID, [][]byte{hash("C")})
		if err != nil {
			t.Fatalf("consume C: %v", err)
		}
		if !ok {
			t.Fatal("expected code C to be consumed")
		}
		if got := pg.Exec(t, fmt.Sprintf("SELECT key_id FROM identity.recovery_code WHERE user_id = '%s' AND used_at IS NOT NULL AND key_id = 'k2' LIMIT 1", userID)); got != "k2" {
			t.Fatalf("key_id = %q, want k2", got)
		}
	})

	t.Run("wrong user cannot consume", func(t *testing.T) {
		other, err := logins.ResolveLogin(ctx, "sub-other", "other@exemplo.pt", "Other")
		if err != nil {
			t.Fatalf("create other: %v", err)
		}
		if err := codes.Replace(ctx, userID, "k1", [][]byte{hash("D")}); err != nil {
			t.Fatalf("replace: %v", err)
		}
		ok, err := codes.Consume(ctx, other, [][]byte{hash("D")})
		if err != nil {
			t.Fatalf("consume: %v", err)
		}
		if ok {
			t.Fatal("other user must not consume the code")
		}
	})

	t.Run("generated codes round-trip through hashing", func(t *testing.T) {
		plain, hashes, err := hasher.Generate()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(plain) != 8 || len(hashes) != 8 {
			t.Fatalf("got %d codes", len(plain))
		}
		if err := codes.Replace(ctx, userID, hasher.KeyID(), hashes); err != nil {
			t.Fatalf("replace: %v", err)
		}
		for i, c := range plain {
			ok, err := codes.Consume(ctx, userID, hasher.Hashes(c))
			if err != nil {
				t.Fatalf("consume %d: %v", i, err)
			}
			if !ok {
				t.Fatalf("generated code %d not consumed", i)
			}
		}
	})

	t.Run("concurrent consume has one winner", func(t *testing.T) {
		if err := codes.Replace(ctx, userID, "k1", [][]byte{hash("E")}); err != nil {
			t.Fatalf("replace: %v", err)
		}
		var wg sync.WaitGroup
		results := make([]bool, 2)
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i], errs[i] = codes.Consume(ctx, userID, [][]byte{hash("E")})
			}(i)
		}
		wg.Wait()
		for i := range errs {
			if errs[i] != nil {
				t.Fatalf("consume %d: %v", i, errs[i])
			}
		}
		if results[0] == results[1] {
			t.Fatalf("results = %v, want exactly one success", results)
		}
	})

	t.Run("invalid hash length is rejected", func(t *testing.T) {
		before := pg.Exec(t, fmt.Sprintf("SELECT count(*) FROM identity.recovery_code WHERE user_id = '%s'", userID))
		if err := codes.Replace(ctx, userID, "k1", [][]byte{hash("short")[:16]}); err == nil {
			t.Fatal("expected error for short hash")
		}
		if _, err := codes.Consume(ctx, userID, [][]byte{hash("short")[:16]}); err == nil {
			t.Fatal("expected error for short hash")
		}
		after := pg.Exec(t, fmt.Sprintf("SELECT count(*) FROM identity.recovery_code WHERE user_id = '%s'", userID))
		if after != before {
			t.Fatalf("row count changed from %s to %s", before, after)
		}
	})
}
