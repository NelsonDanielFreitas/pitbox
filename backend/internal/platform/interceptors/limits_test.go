package interceptors

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
)

func TestLimitsDeadlines(t *testing.T) {
	t.Parallel()

	const maxUnary, maxStream = 30 * time.Second, 55 * time.Minute
	i, err := Limits(maxUnary, maxStream)
	if err != nil {
		t.Fatalf("Limits: %v", err)
	}
	stream := connect.Spec{StreamType: connect.StreamTypeServer, Procedure: "/pitbox.test.v1.TestService/Watch"}

	tests := []struct {
		name        string
		spec        connect.Spec
		clientWait  time.Duration // 0 = the client sent no deadline
		wantAtMost  time.Duration
		wantAtLeast time.Duration
	}{
		{name: "unary without a client deadline gets the default", spec: unarySpec, wantAtMost: maxUnary, wantAtLeast: maxUnary - 2*time.Second},
		{name: "a shorter client deadline is kept", spec: unarySpec, clientWait: 2 * time.Second, wantAtMost: 2 * time.Second, wantAtLeast: time.Second},
		{name: "a longer client deadline is cut", spec: unarySpec, clientWait: 10 * time.Minute, wantAtMost: maxUnary, wantAtLeast: maxUnary - 2*time.Second},
		{name: "a stream gets the stream ceiling", spec: stream, wantAtMost: maxStream, wantAtLeast: maxStream - 2*time.Second},
		{name: "a stream is also cut", spec: stream, clientWait: 3 * time.Hour, wantAtMost: maxStream, wantAtLeast: maxStream - 2*time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tc.clientWait > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.clientWait)
				defer cancel()
			}
			var remaining time.Duration
			h := i(func(ctx context.Context, _ connect.Spec, _ connect.ServerStream) error {
				d, ok := ctx.Deadline()
				if !ok {
					t.Error("the handler has no deadline")
				}
				remaining = time.Until(d)
				return nil
			})
			if err := h(ctx, tc.spec, nopStream{}); err != nil {
				t.Fatalf("handler: %v", err)
			}
			if remaining > tc.wantAtMost || remaining < tc.wantAtLeast {
				t.Errorf("deadline in %s, want between %s and %s", remaining, tc.wantAtLeast, tc.wantAtMost)
			}
		})
	}
}

func TestLimitsRejectsNonPositiveDurations(t *testing.T) {
	t.Parallel()

	for _, d := range [][2]time.Duration{{0, time.Second}, {time.Second, 0}, {-1, time.Second}} {
		if _, err := Limits(d[0], d[1]); err == nil {
			t.Errorf("Limits(%v, %v) should fail", d[0], d[1])
		}
	}
}
