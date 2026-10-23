package main

// chain.go —— 责任链：与框架既有的 `frameworktools.MetaMiddleware` **同形状**。
//
// 形态刻意一致（name+meta → 包装 next）：MVP 证明的是"这类中间件能承载传输选择"，
// 不是发明一套只属于 SSH 的插件机制。落地时它可以原样贴进
// backend/tool/registry/registry_state.go 的 WithMetaMiddleware 列表。

import (
	"context"
)

// Meta 是工具元信息（框架里是 ToolMeta，这里只取 MVP 需要的两项）。
type Meta struct {
	Name string
	// Eligible 标记该工具是否属于"双执行面"（五个内置工具为 true）。
	Eligible bool
}

// Call 是一次工具调用（框架里是 ToolCall）。
type Call struct {
	Name string
	Args map[string]string
}

// Result 是工具结果，额外带**传输决策**——它必须进轨迹，否则"这次为什么在本机
// 改了文件"事后无法归因。
type Result struct {
	Value    any    `json:"value,omitempty"`
	Impl     string `json:"transport"`
	Degraded bool   `json:"degraded,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Handler 是链上的处理器。
type Handler func(ctx context.Context, call Call) (Result, error)

// Middleware 与框架的 MetaMiddleware 同形状。
type Middleware func(meta Meta, next Handler) Handler

// Compose 把中间件按声明顺序组合：**先声明的是外层**（先进入、后退出）。
func Compose(middlewares ...Middleware) func(meta Meta, terminal Handler) Handler {
	return func(meta Meta, terminal Handler) Handler {
		handler := terminal
		for i := len(middlewares) - 1; i >= 0; i-- {
			handler = middlewares[i](meta, handler)
		}
		return handler
	}
}

// ── 中间件 1：审计（进/出各记一条，用于证明顺序与短路）──────────────────

// AuditMiddleware 记录每次进入/离开该工具。
func AuditMiddleware(trace *[]string) Middleware {
	return func(meta Meta, next Handler) Handler {
		return func(ctx context.Context, call Call) (Result, error) {
			*trace = append(*trace, "enter:"+meta.Name)
			res, err := next(ctx, call)
			verdict := "exit:" + meta.Name
			if err != nil {
				verdict += ":err"
			}
			*trace = append(*trace, verdict)
			return res, err
		}
	}
}

// ── 中间件 2：权限（短路演示）────────────────────────────────────────

// PermissionMiddleware 在拒绝时**不进后续中间件**，直接返回。
func PermissionMiddleware(allowed func(name string) bool) Middleware {
	return func(meta Meta, next Handler) Handler {
		return func(ctx context.Context, call Call) (Result, error) {
			if !allowed(meta.Name) {
				return Result{}, toolErr("TOOL_FORBIDDEN", "请求权限或换工具", "工具 %s 被权限链拒绝", meta.Name)
			}
			return next(ctx, call)
		}
	}
}

// ── 中间件 3：传输选择（本设计的核心）────────────────────────────────

// ctxKeyTransport 把选中的执行面塞进 ctx，供终端 handler 取用。
type ctxKeyTransport struct{}

func withTransport(ctx context.Context, transport Transport, decision Decision) context.Context {
	ctx = context.WithValue(ctx, ctxKeyTransport{}, transport)
	return context.WithValue(ctx, ctxKeyDecision{}, decision)
}

type ctxKeyDecision struct{}

func transportFrom(ctx context.Context) (Transport, Decision, bool) {
	transport, ok := ctx.Value(ctxKeyTransport{}).(Transport)
	if !ok {
		return nil, Decision{}, false
	}
	decision, _ := ctx.Value(ctxKeyDecision{}).(Decision)
	return transport, decision, true
}

// SelectorMiddleware 按项目状态 + 显式意图选出执行面。
//
// 顺序要点：它必须**在 pathGate 之后、终端 handler 之前**。pathGate 判的是路径
// 允不允许碰，而本地面与远端面的路径规则不同（同一字符串在两边含义不同）；
// 先选面、再按该面的规则过闸，才不会出现"用本机规则判远端路径"的假通过。
func SelectorMiddleware(state ProjectState, explicit string) Middleware {
	return func(meta Meta, next Handler) Handler {
		return func(ctx context.Context, call Call) (Result, error) {
			if !meta.Eligible {
				// 非双面工具（例如 ssh_hosts 这类只在本地有意义的管理工具）：不介入。
				return next(ctx, call)
			}
			decision, err := Resolve(state, explicit)
			if err != nil {
				return Result{}, err
			}
			transport, ok := registry[decision.Transport]
			if !ok {
				return Result{}, toolErr(CodeBadArg, "检查传输名", "未知传输：%s", decision.Transport)
			}
			res, callErr := next(withTransport(ctx, transport, decision), call)
			res.Impl = decision.Transport
			res.Degraded = decision.Degraded
			res.Reason = decision.Reason
			res.Detail = decision.Detail
			return res, callErr
		}
	}
}

// registry 是"传输名 → 实现"的注册表（策略模式的注入点）。
//
// 生产形态里它由装配层填充（本地一份，SSH 按项目懒建一份），MVP 用包级变量
// 简化，但**取用只经这一处**，不允许在 handler 里 new 传输。
var registry = map[string]Transport{}

// RegisterTransport 注册一个执行面。
func RegisterTransport(transport Transport) {
	registry[transport.Name()] = transport
}

// ── 终端 handler：五件事的唯一分发点 ────────────────────────────────

// TerminalHandler 依据 call.Name 分发到执行面的对应方法。
//
// 这是"一份 handler 服务两个执行面"的落点：本地与远端的差异只体现在 ctx 里
// 那个 Transport 是谁，分发逻辑本身只有一份。
func TerminalHandler() Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		transport, _, ok := transportFrom(ctx)
		if !ok {
			return Result{}, toolErr(CodeBadArg, "检查装配", "调用缺少执行面（选择器未生效）")
		}
		switch call.Name {
		case "read_file":
			content, err := transport.Read(ctx, call.Args["path"], atoiOr(call.Args["limit"], 0))
			if err != nil {
				return Result{}, err
			}
			return Result{Value: content}, nil
		case "write_file":
			written, err := transport.Write(ctx, call.Args["path"], []byte(call.Args["content"]))
			if err != nil {
				return Result{}, err
			}
			return Result{Value: written}, nil
		case "edit_file":
			edited, err := transport.Edit(ctx, call.Args["path"], call.Args["old_text"], call.Args["new_text"],
				call.Args["replace_all"] == "true")
			if err != nil {
				return Result{}, err
			}
			return Result{Value: edited}, nil
		case "bash":
			exec, err := transport.Exec(ctx, call.Args["script"])
			if err != nil {
				return Result{}, err
			}
			return Result{Value: exec}, nil
		case "bg_manage":
			managed, err := transport.Manage(ctx, call.Args["op"], call.Args["handle"])
			if err != nil {
				return Result{}, err
			}
			return Result{Value: managed}, nil
		default:
			return Result{}, toolErr(CodeBadArg, "检查工具名", "未接入双面的工具：%s", call.Name)
		}
	}
}

func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	value := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		value = value*10 + int(r-'0')
	}
	return value
}
