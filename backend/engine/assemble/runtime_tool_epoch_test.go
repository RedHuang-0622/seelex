package assemble

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"
	taskcontext "github.com/RedHuang-0622/seelex/kernel/task"
)

type changingToolRuntime struct {
	tools []types.Tool
}

func (runtime *changingToolRuntime) VisibleTools(context.Context) []types.Tool {
	return append([]types.Tool(nil), runtime.tools...)
}

func (runtime *changingToolRuntime) Dispatch(_ context.Context, name, _ string) (string, error) {
	for _, tool := range runtime.tools {
		if tool.Function.Name == name {
			return "ok", nil
		}
	}
	return "", bridge.ErrToolNotVisible
}

func toolDefinition(name string) types.Tool {
	return types.Tool{Type: "function", Function: types.ToolFunction{Name: name}}
}

func TestFrozenToolRuntimeKeepsSchemaAndRejectsCurrentRemoval(t *testing.T) {
	runtime := &Runtime{}
	base := &changingToolRuntime{tools: []types.Tool{toolDefinition("read")}}
	runtime.SetToolCatalogForSession("session-a", 1, []Tool{{Name: "read"}})
	wrapped := &frozenToolRuntime{runtime: runtime, base: base}
	ctx := taskcontext.WithSessionID(context.Background(), "session-a")

	first := wrapped.VisibleTools(ctx)
	if len(first) != 1 || first[0].Function.Name != "read" {
		t.Fatalf("initial frozen schema = %+v", first)
	}
	base.tools = append(base.tools, toolDefinition("bash"))
	second := wrapped.VisibleTools(ctx)
	if len(second) != 1 || second[0].Function.Name != "read" {
		t.Fatalf("availability change leaked into top-level schema: %+v", second)
	}
	base.tools = nil
	if _, err := wrapped.Dispatch(ctx, "read", "{}"); err == nil || !strings.Contains(err.Error(), "epoch 1") {
		t.Fatalf("removed tool rejection=%v, want structured epoch", err)
	}

	base.tools = []types.Tool{toolDefinition("read"), toolDefinition("bash")}
	runtime.SetToolCatalogForSession("session-a", 2, []Tool{{Name: "read"}, {Name: "bash"}})
	third := wrapped.VisibleTools(ctx)
	if len(third) != 2 {
		t.Fatalf("catalog epoch bump did not refresh schema: %+v", third)
	}
}
