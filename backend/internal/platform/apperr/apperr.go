// Package apperr holds the few errors every layer shares, so that the transport can turn
// them into public answers in one place (the errors interceptor) and not in each handler.
package apperr

import "errors"

// ErrNotFound means the resource does not exist for the caller: it does not exist, or it
// belongs to a team the caller is not a member of. The two are deliberately the same error
// (and the same answer, NOT_FOUND), so that ids cannot be probed (ADR 0005). Wrap it
// ("... : %w") to add context for the log; errors.Is still finds it.
var ErrNotFound = errors.New("not found")

// ErrInvalidArgument means a value that came from the client is not acceptable and the
// validation of the contract could not catch it (for example, the nil UUID, which a UUID rule
// accepts). The errors interceptor answers INVALID_ARGUMENT with a fixed message; wrap it with
// context for the log and never put the value in the text.
var ErrInvalidArgument = errors.New("invalid argument")

// ErrPermissionDenied means the caller is authenticated but not authorized for the
// requested action. The errors interceptor answers PERMISSION_DENIED with a fixed message;
// wrap it with context for the log and never put the resource id in the text.
var ErrPermissionDenied = errors.New("permission denied")
