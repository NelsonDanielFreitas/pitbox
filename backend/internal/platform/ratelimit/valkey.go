package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

const keyPrefix = "pitbox:rl:"

// gcraScript is an atomic Generic Cell Rate Algorithm check. It stores the theoretical
// arrival time (TAT) of the next allowed request.
//
// Keys: [key]
// ARGV: [increment_ms, burst_ms, now_ms, ttl_ms]
// Returns: 1 if allowed, 0 if rejected.
var gcraScript = valkey.NewLuaScript(`
local key = KEYS[1]
local increment = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])

local tat = redis.call('GET', key)
if not tat then
  tat = now
else
  tat = tonumber(tat)
end

local allow_at = tat - burst
if now < allow_at then
  return 0
end

local new_tat = math.max(now, tat) + increment
redis.call('SET', key, new_tat, 'PX', ttl)
return 1
`)

// valkeyLimiter runs GCRA in Valkey. It assumes the client is already configured with
// the pitbox_api ACL, which only touches pitbox:rl:*.
type valkeyLimiter struct {
	client valkey.Client
}

func newValkeyLimiter(client valkey.Client) *valkeyLimiter {
	return &valkeyLimiter{client: client}
}

// allow returns true if the request is within the distributed quota.
func (v *valkeyLimiter) allow(ctx context.Context, kind Kind, id string, limit Limit, now time.Time) (bool, error) {
	if limit.RPS <= 0 {
		return true, nil
	}
	incrementMs := 1000 / limit.RPS
	burstMs := (limit.Burst - 1) * incrementMs
	if burstMs <= 0 {
		burstMs = incrementMs
	}
	ttlMs := burstMs + 60*incrementMs
	key := keyPrefix + keySuffix(kind, id)
	nowMs := now.UnixMilli()

	res, err := gcraScript.Exec(ctx, v.client, []string{key}, []string{
		strconv.Itoa(incrementMs),
		strconv.Itoa(burstMs),
		strconv.FormatInt(nowMs, 10),
		strconv.Itoa(ttlMs),
	}).AsInt64()
	if err != nil {
		return false, fmt.Errorf("gcra %s: %w", kind, err)
	}
	return res == 1, nil
}

func keySuffix(kind Kind, id string) string {
	switch kind {
	case KindUserRead:
		return "user:" + id + ":read"
	case KindUserWrite:
		return "user:" + id + ":write"
	case KindTeamRead:
		return "team:" + id + ":read"
	case KindTeamWrite:
		return "team:" + id + ":write"
	case KindPublicRead:
		return "public:" + id + ":read"
	case KindPublicWrite:
		return "public:" + id + ":write"
	default:
		return "unknown:" + id
	}
}
