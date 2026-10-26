package contract

import "errors"

// ErrSessionNotFoundArchive is the cross-layer identity for an archive target
// whose session record is missing or belongs to another session.
var ErrSessionNotFoundArchive = errors.New("session not found")
