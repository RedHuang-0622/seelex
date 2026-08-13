package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// scoped_tools.go 只保留 Runtime 门面：scoped 工具族注册委托与 bash 诊断
// 事件投递；工具实现已迁入 seelebridge/tools.Router（Deps 注入）。

// BashDiagnosticEvent / BashDiagnosticObserver 兼容别名（实现下沉 tools/ 域）。
type (
	BashDiagnosticEvent    = tools.BashDiagnosticEvent
	BashDiagnosticObserver = tools.BashDiagnosticObserver
)

// registerProjectScopedTools overrides the Seele builtin filesystem tools
// （委托 tools.Router；RegisterBuiltins 内调用）。
func (r *Runtime) registerProjectScopedTools() {
	tools.NewRouter(r.scopedToolsDeps()).Register()
}

// resolveNodePath 解析工具路径的根（委托 tools.Router；测试/门面使用）。
func (r *Runtime) resolveNodePath(ctx context.Context, path string, forWrite bool) (string, error) {
	return tools.NewRouter(r.scopedToolsDeps()).ResolveNodePath(ctx, path, forWrite)
}

// scopedBash 是 bash 工具的委托入口（测试/门面使用）。
func (r *Runtime) scopedBash(ctx context.Context, argsJSON string) (string, error) {
	return tools.NewRouter(r.scopedToolsDeps()).ScopedBash(ctx, argsJSON)
}

// observeBash 投递 scoped bash 诊断事件（工具调用不可被诊断改变；观察者
// 意外 panic 也不影响工具调用）。
func (r *Runtime) observeBash(event BashDiagnosticEvent) {
	if r == nil {
		return
	}
	r.bashObserverMu.RLock()
	observer := r.bashObserver
	r.bashObserverMu.RUnlock()
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}
