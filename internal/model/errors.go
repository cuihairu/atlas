package model

import "errors"

// Sentinel errors returned across the Atlas storage and service layers.
//
// Handlers in internal/httpapi map these to the error codes documented in
// docs/api.md#错误响应.
var (
	// ErrInvalid indicates a malformed or out-of-range argument.
	ErrInvalid = errors.New("invalid argument")

	// ErrNotFound indicates the requested server or character index record does
	// not exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict indicates a uniqueness violation. Registration is idempotent by
	// design, so this is reserved for genuinely conflicting writes.
	ErrConflict = errors.New("conflict")
)
