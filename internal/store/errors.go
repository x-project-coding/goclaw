package store

import "errors"

// ErrNotFound is returned by store methods when the requested row does not exist.
// Callers compare with errors.Is(err, store.ErrNotFound) — never with ==.
var ErrNotFound = errors.New("store: not found")
