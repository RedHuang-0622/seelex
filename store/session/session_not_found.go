package session

import (
	"errors"
	"io/fs"
)

// IsSessionNotFound is the single missing-session criterion. The JSON v8
// session store encodes absence as fs.ErrNotExist; retired SQL backends do not
// exist and must not add sql.ErrNoRows as a second truth.
func IsSessionNotFound(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
