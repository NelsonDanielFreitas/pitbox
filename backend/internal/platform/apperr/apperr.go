// Package apperr holds the few errors every layer shares, so that the transport can turn
// them into public answers in one place (the errors interceptor) and not in each handler.
package apperr

import "errors"

// ErrNotFound means the resource does not exist for the caller: it does not exist, or it
// belongs to a team the caller is not a member of. The two are deliberately the same error
// (and the same answer, NOT_FOUND), so that ids cannot be probed (ADR 0005). Wrap it
// ("... : %w") to add context for the log; errors.Is still finds it.
var ErrNotFound = errors.New("not found")
