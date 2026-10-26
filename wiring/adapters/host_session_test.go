package adapters

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/assemble"
)

func TestHostSessionForToolCallMatchesAssemble(t *testing.T) {
	ctx := context.Background()
	if got, want := NewHostSessionForToolCall()(ctx), assemble.HostSessionForToolCall(ctx); got != want {
		t.Fatalf("host session = %q, want %q", got, want)
	}
}
