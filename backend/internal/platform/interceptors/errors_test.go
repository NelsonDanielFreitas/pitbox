package interceptors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

func TestPublicError(t *testing.T) {
	t.Parallel()

	authored := connect.NewError(connect.CodeNotFound, "team not found")
	tests := []struct {
		name        string
		err         error
		wantNil     bool
		wantCode    connect.Code
		wantMessage string // exact public message; "" = not checked
		leak        string // must not appear in the public message
	}{
		{name: "nil stays nil", err: nil, wantNil: true},
		{name: "plain error becomes internal", err: errors.New("pg: connection refused"), wantCode: connect.CodeInternal, wantMessage: publicInternalMessage, leak: "pg:"},
		{name: "authored error is kept", err: authored, wantCode: connect.CodeNotFound, wantMessage: "team not found"},
		{name: "wrapped authored error keeps its code", err: fmt.Errorf("get team: %w", authored), wantCode: connect.CodeNotFound, wantMessage: "team not found", leak: "get team"},
		{name: "remote error is not forwarded", err: connect.NewError(connect.CodePermissionDenied, "from another service").WithRemote(), wantCode: connect.CodeInternal, wantMessage: publicInternalMessage, leak: "another service"},
		{name: "deadline exceeded", err: fmt.Errorf("query: %w", context.DeadlineExceeded), wantCode: connect.CodeDeadlineExceeded, wantMessage: publicDeadlineMessage, leak: "query"},
		{name: "canceled", err: context.Canceled, wantCode: connect.CodeCanceled, wantMessage: publicCanceledMessage},
		// One place maps it: a resource of another team and one that does not exist look the same.
		{name: "not found, bare", err: apperr.ErrNotFound, wantCode: connect.CodeNotFound, wantMessage: publicNotFoundMessage},
		{name: "invalid argument, bare", err: apperr.ErrInvalidArgument, wantCode: connect.CodeInvalidArgument, wantMessage: publicInvalidMessage},
		{name: "invalid argument, wrapped with context that is not for the client", err: fmt.Errorf("team id 0000: %w", apperr.ErrInvalidArgument), wantCode: connect.CodeInvalidArgument, wantMessage: publicInvalidMessage, leak: "0000"},
		{name: "not found, wrapped with context that is not for the client", err: fmt.Errorf("team 0190f2a4: user 0190aaaa is not a member: %w", apperr.ErrNotFound), wantCode: connect.CodeNotFound, wantMessage: publicNotFoundMessage, leak: "0190"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := publicError(tc.err)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}
			var ce *connect.Error
			if !errors.As(got, &ce) {
				t.Fatalf("got %T, want a *connect.Error", got)
			}
			if ce.Code() != tc.wantCode {
				t.Errorf("code = %v, want %v", ce.Code(), tc.wantCode)
			}
			if tc.wantMessage != "" && ce.Message() != tc.wantMessage {
				t.Errorf("message = %q, want %q", ce.Message(), tc.wantMessage)
			}
			if tc.leak != "" && strings.Contains(ce.Message(), tc.leak) {
				t.Errorf("message %q leaks %q", ce.Message(), tc.leak)
			}
			if tc.err != nil && !errors.Is(got, tc.err) && tc.wantCode != connect.CodeNotFound {
				t.Errorf("the cause is lost: errors.Is(got, original) is false")
			}
		})
	}
}

func TestErrorsLogsOneLinePerRPC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantLevel   string
		wantCode    string
		wantCause   bool   // the cause is logged
		causeSubstr string // text expected in the log
	}{
		{name: "success is debug", err: nil, wantLevel: "DEBUG", wantCode: "ok"},
		{name: "internal is an error with its cause", err: errors.New("pg: connection refused"), wantLevel: "ERROR", wantCode: "internal", wantCause: true, causeSubstr: "pg: connection refused"},
		{name: "client error is info and logs no cause", err: connect.NewError(connect.CodeInvalidArgument, "name too long").WithCause(errors.New("name=Alice Silva")), wantLevel: "INFO", wantCode: "invalid_argument"},
		{name: "invalid argument is info, not an error, and logs no cause", err: fmt.Errorf("team id Alice: %w", apperr.ErrInvalidArgument), wantLevel: "INFO", wantCode: "invalid_argument"},
		{name: "not found is info and logs no cause", err: fmt.Errorf("team Alice: %w", apperr.ErrNotFound), wantLevel: "INFO", wantCode: "not_found"},
		{name: "canceled is info", err: context.Canceled, wantLevel: "INFO", wantCode: "canceled"},
		{name: "deadline is a warning", err: context.DeadlineExceeded, wantLevel: "WARN", wantCode: "deadline_exceeded", wantCause: true, causeSubstr: "context deadline exceeded"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs syncBuffer
			_ = call(t, Errors(newLogger(&logs)), unarySpec, nil,
				func(context.Context, connect.Spec, connect.ServerStream) error { return tc.err })

			recs := logs.records(t)
			if len(recs) != 1 {
				t.Fatalf("got %d log lines, want 1: %s", len(recs), logs.String())
			}
			r := recs[0]
			if r["level"] != tc.wantLevel || r["code"] != tc.wantCode || r["procedure"] != unarySpec.Procedure {
				t.Errorf("record = %v", r)
			}
			if _, ok := r["duration_ms"]; !ok {
				t.Error("no duration in the record")
			}
			_, hasErr := r["cause"]
			if tc.err == nil {
				hasErr = false
			}
			if hasErr != tc.wantCause {
				t.Errorf("cause logged = %v, want %v: %v", hasErr, tc.wantCause, r)
			}
			if tc.causeSubstr != "" && !strings.Contains(logs.String(), tc.causeSubstr) {
				t.Errorf("log lacks %q: %s", tc.causeSubstr, logs.String())
			}
			// The cause of a client error can echo request data: never logged.
			if strings.Contains(logs.String(), "Alice") {
				t.Errorf("a client error's cause was logged: %s", logs.String())
			}
		})
	}
}

func TestLogLevelCoversEveryCode(t *testing.T) {
	t.Parallel()

	for _, c := range []connect.Code{connect.CodeInternal, connect.CodeUnknown, connect.CodeDataLoss, connect.CodeUnavailable} {
		if logLevel(c, true) != slog.LevelError {
			t.Errorf("%v should be an error", c)
		}
	}
	for _, c := range []connect.Code{connect.CodeInvalidArgument, connect.CodeNotFound, connect.CodePermissionDenied, connect.CodeUnauthenticated, connect.CodeResourceExhausted, connect.CodeCanceled} {
		if logLevel(c, true) != slog.LevelInfo {
			t.Errorf("%v should be info", c)
		}
	}
}
