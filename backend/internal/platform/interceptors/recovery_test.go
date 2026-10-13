package interceptors

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
)

func TestRecoveryTurnsAPanicIntoAFixedInternalError(t *testing.T) {
	t.Parallel()

	var logs syncBuffer
	err := call(t, Recovery(newLogger(&logs)), unarySpec, nil,
		func(context.Context, connect.Spec, connect.ServerStream) error {
			panic("boom: password=hunter2")
		})

	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeInternal {
		t.Fatalf("err = %v, want a CodeInternal *connect.Error", err)
	}
	if ce.Message() != publicInternalMessage {
		t.Errorf("message = %q, want the fixed %q", ce.Message(), publicInternalMessage)
	}
	if strings.Contains(ce.Message(), "hunter2") {
		t.Error("the panic value reached the public message")
	}

	recs := logs.records(t)
	if len(recs) != 1 || recs[0]["msg"] != "panic in rpc handler" {
		t.Fatalf("log = %v", recs)
	}
	if recs[0]["procedure"] != unarySpec.Procedure || !strings.Contains(recs[0]["stack"].(string), "recovery_test.go") {
		t.Errorf("the panic log lacks the procedure or the stack: %v", recs[0])
	}
}

func TestRecoveryPassesThroughWithoutPanic(t *testing.T) {
	t.Parallel()

	want := connect.NewError(connect.CodeNotFound, "nope")
	var logs syncBuffer
	err := call(t, Recovery(newLogger(&logs)), unarySpec, nil,
		func(context.Context, connect.Spec, connect.ServerStream) error { return want })
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want the handler's error untouched", err)
	}
	if logs.String() != "" {
		t.Errorf("nothing should be logged without a panic: %s", logs.String())
	}
}

// net/http uses panic(http.ErrAbortHandler) to abort a response silently; the
// interceptor must not swallow it.
func TestRecoveryRepanicsErrAbortHandler(t *testing.T) {
	t.Parallel()

	defer func() {
		r := recover()
		if e, ok := r.(error); !ok || !errors.Is(e, http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler", r)
		}
	}()
	_ = call(t, Recovery(newLogger(&syncBuffer{})), unarySpec, nil,
		func(context.Context, connect.Spec, connect.ServerStream) error { panic(http.ErrAbortHandler) })
	t.Error("the panic was swallowed")
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate(strings.Repeat("a", 20), 5); got != "aaaaa...(truncated)" {
		t.Errorf("truncate = %q", got)
	}
}
