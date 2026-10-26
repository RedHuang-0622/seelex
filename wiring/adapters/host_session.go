package adapters

import (
	"context"

	"github.com/RedHuang-0622/seelex/backend/engine/assemble"
)

// NewHostSessionForToolCall exposes the read-only Runtime context lookup used
// by goal tools without importing the composition package into Application.
func NewHostSessionForToolCall() func(context.Context) string {
	return assemble.HostSessionForToolCall
}
