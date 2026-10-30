package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func seed(b byte) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string(rune(b)), 32)))
}

func TestParseSyncPassKeys(t *testing.T) {
	keys, err := parseSyncPassKeys("k1:" + seed('a') + ",k2:" + seed('b'))
	if err != nil || len(keys) != 2 || keys[0].ID != "k1" || len(keys[0].Private()) != 64 {
		t.Fatalf("valid ring: %v %+v", err, keys)
	}
	for name, bad := range map[string]string{
		"no kid":       seed('a'),
		"bad kid":      "K1:" + seed('a'),
		"not base64":   "k1:***",
		"too short":    "k1:" + base64.StdEncoding.EncodeToString([]byte("short")),
		"too long":     "k1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 33))),
		"repeated kid": "k1:" + seed('a') + ",k1:" + seed('b'),
		"empty entry":  "k1:" + seed('a') + ",",
	} {
		_, err := parseSyncPassKeys(bad)
		if err == nil {
			t.Errorf("%s accepted", name)
			continue
		}
		if strings.Contains(err.Error(), seed('a')) || strings.Contains(err.Error(), "short") {
			t.Errorf("%s: the error repeats the secret: %v", name, err)
		}
	}
}

func TestSyncPassKeysRequiredOutsideLocal(t *testing.T) {
	c := Config{Env: EnvProduction}
	if errs := c.validateSync(); len(errs) != 1 {
		t.Fatalf("production without keys: %v", errs)
	}
	c.SyncPassKeys = "k1:" + seed('a')
	if errs := c.validateSync(); len(errs) != 0 {
		t.Fatalf("production with keys: %v", errs)
	}
	if errs := (Config{Env: EnvLocal}).validateSync(); len(errs) != 0 {
		t.Fatalf("local without keys must be allowed: %v", errs)
	}
}
