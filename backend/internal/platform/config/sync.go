package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// SyncPassKey is one Ed25519 key that signs or verifies offline passes.
type SyncPassKey struct {
	ID   string
	Seed []byte // 32 bytes; the private key is derived from it
}

// Private derives the signing key.
func (k SyncPassKey) Private() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(k.Seed) }

// SyncConfigured reports whether the sync service can run: it needs a pass signing key.
func (c Config) SyncConfigured() bool { return strings.TrimSpace(c.SyncPassKeys) != "" }

// SyncPassKeyring parses SyncPassKeys. The first key signs; all of them verify.
func (c Config) SyncPassKeyring() ([]SyncPassKey, error) { return parseSyncPassKeys(c.SyncPassKeys) }

// parseSyncPassKeys never puts key material in an error: the value is a secret.
func parseSyncPassKeys(raw string) ([]SyncPassKey, error) {
	if raw == "" {
		return nil, nil
	}
	var keys []SyncPassKey
	for i, entry := range strings.Split(raw, ",") {
		id, b64, ok := strings.Cut(entry, ":")
		if !ok || !keyID.MatchString(id) {
			return nil, fmt.Errorf("PITBOX_SYNC_PASS_KEYS: entry %d is not <kid>:<base64> (kid: 1-16 chars of a-z, 0-9, -)", i+1)
		}
		seed, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("PITBOX_SYNC_PASS_KEYS: entry %d is not standard base64", i+1)
		}
		if len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("PITBOX_SYNC_PASS_KEYS: entry %d is not exactly %d bytes", i+1, ed25519.SeedSize)
		}
		if slices.ContainsFunc(keys, func(k SyncPassKey) bool { return k.ID == id }) {
			return nil, fmt.Errorf("PITBOX_SYNC_PASS_KEYS: entry %d repeats the kid of an earlier entry", i+1)
		}
		keys = append(keys, SyncPassKey{ID: id, Seed: seed})
	}
	return keys, nil
}

func (c Config) validateSync() []error {
	var errs []error
	if _, err := parseSyncPassKeys(c.SyncPassKeys); err != nil {
		errs = append(errs, err)
	}
	if c.Env != EnvLocal && !c.SyncConfigured() {
		errs = append(errs, errors.New("PITBOX_SYNC_PASS_KEYS: required outside local (offline passes cannot be signed without it)"))
	}
	return errs
}
