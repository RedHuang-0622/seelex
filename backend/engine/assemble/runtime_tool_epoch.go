package assemble

import (
	"context"
	"errors"
	"fmt"

	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"
)

type toolRuntimeBase interface {
	VisibleTools(context.Context) []types.Tool
	Dispatch(context.Context, string, string) (string, error)
}

// frozenToolRuntime keeps the top-level provider schema stable for one session.
// Dispatch still delegates to Seele's current visibility/permission policy;
// unavailable calls are converted into a structured, attributable rejection.
type frozenToolRuntime struct {
	runtime *Runtime
	base    toolRuntimeBase
}

func (runtime *frozenToolRuntime) VisibleTools(ctx context.Context) []types.Tool {
	if runtime == nil || runtime.base == nil {
		return nil
	}
	sessionID := runtimeSessionID(ctx)
	if sessionID == "" {
		return runtime.base.VisibleTools(ctx)
	}
	runtime.runtime.contextEpochMu.RLock()
	frozen := append([]types.Tool(nil), runtime.runtime.frozenToolsBySID[sessionID]...)
	runtime.runtime.contextEpochMu.RUnlock()
	if len(frozen) > 0 {
		return frozen
	}
	current := runtime.base.VisibleTools(ctx)
	runtime.runtime.contextEpochMu.Lock()
	if runtime.runtime.frozenToolsBySID == nil {
		runtime.runtime.frozenToolsBySID = make(map[string][]types.Tool)
	}
	runtime.runtime.frozenToolsBySID[sessionID] = append([]types.Tool(nil), current...)
	runtime.runtime.contextEpochMu.Unlock()
	return append([]types.Tool(nil), current...)
}

func (runtime *frozenToolRuntime) Dispatch(ctx context.Context, name, argsJSON string) (string, error) {
	if runtime == nil || runtime.base == nil {
		return "", fmt.Errorf("tool runtime unavailable")
	}
	result, err := runtime.base.Dispatch(ctx, name, argsJSON)
	if err == nil || !errors.Is(err, bridge.ErrToolNotVisible) {
		return result, err
	}
	sessionID := runtimeSessionID(ctx)
	runtime.runtime.contextEpochMu.RLock()
	epoch := runtime.runtime.toolEpochBySID[sessionID]
	runtime.runtime.contextEpochMu.RUnlock()
	return "", fmt.Errorf("tool %q unavailable in toolset epoch %d: %w", name, epoch, bridge.ErrToolNotVisible)
}
