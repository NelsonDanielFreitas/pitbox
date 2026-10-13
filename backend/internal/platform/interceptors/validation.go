package interceptors

import (
	"fmt"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect/v2"
	"connectrpc.com/validate"
	"google.golang.org/protobuf/proto"
)

// NewValidator builds the protovalidate validator for the given request types.
//
//   - The rules are compiled now, so a broken rule fails at startup, not on the
//     first request (fail closed), and a client cannot make the server compile rules
//     in the middle of a request.
//   - WithDisableLazy: a message type that is not in the list is an error instead of
//     passing without validation.
//   - WithFailFast: only the first violation is reported, which costs less CPU on a
//     hostile message; the app validates with Zod before sending anyway.
func NewValidator(requests ...proto.Message) (protovalidate.Validator, error) {
	v, err := protovalidate.New(
		protovalidate.WithMessages(requests...),
		protovalidate.WithDisableLazy(),
		protovalidate.WithFailFast(),
	)
	if err != nil {
		return nil, fmt.Errorf("build protovalidate validator: %w", err)
	}
	return v, nil
}

// Validation rejects a request that breaks its protovalidate rules with
// CodeInvalidArgument, before the service implementation runs. The error carries a
// buf.validate.Violations detail (field path and rule id, not the sent value).
func Validation(v protovalidate.Validator) connect.ServerInterceptor {
	return validate.NewServerInterceptor(validate.WithValidator(v))
}
