package telemetry

import (
	"context"
	"errors"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
)

// Wrapper 是 telemetry 钩子的装饰器形态：接收 next，返回包装后的钩子。
// 观察面实现 Wrapper 时只写自己的 Before/After 逻辑，透传、nil 兜底与
// ErrorHook 传播统一由 Chain 负责，避免新增观察面时手抄整段样板。
type Wrapper func(next frameworktelemetry.Hook) frameworktelemetry.Hook

// chainHook 是 Chain 的组装结果：Before/After 委托给最外层组成钩子，
// OnError 按最外层→最内层顺序传播给所有实现了 ErrorHook 的组成钩子。
type chainHook struct {
	outermost  frameworktelemetry.Hook
	errorHooks []frameworktelemetry.ErrorHook
}

// Chain 组装 telemetry 钩子链：wrappers 从左到右依次包在外层（第一个
// wrapper 最外层，最后一个最内层），base 是最内层实现。
// base 为 nil 时降级为 noopHook（透传空实现）；wrapper 或 wrapper 产出
// 为 nil 时跳过。OnError 集中传播：所有实现 ErrorHook 的组成钩子都会
// 被调用（未实现的钩子不接收），错误 errors.Join。
//
// 注意：组成钩子不应再为实现透传而实现 ErrorHook（那是 Chain 的职责）；
// 若某个观察面要观察错误事件，就实现带自身逻辑的 OnError，Chain 会
// 按序调用它。
func Chain(base frameworktelemetry.Hook, wrappers ...Wrapper) frameworktelemetry.Hook {
	hook := base
	if hook == nil {
		hook = noopHook{}
	}
	chain := &chainHook{outermost: hook}
	constructed := []frameworktelemetry.Hook{hook}
	// 逆序应用：最后一个 wrapper 最先包住 base，从而第一个 wrapper 最外层。
	for i := len(wrappers) - 1; i >= 0; i-- {
		if wrappers[i] == nil {
			continue
		}
		if wrapped := wrappers[i](hook); wrapped != nil {
			hook = wrapped
			constructed = append(constructed, hook)
		}
	}
	chain.outermost = hook
	// 构造顺序与嵌套顺序相反：从后往前即最外层→最内层。
	for i := len(constructed) - 1; i >= 0; i-- {
		chain.collect(constructed[i])
	}
	if len(chain.errorHooks) == 0 {
		return hook
	}
	return chain
}

// collect 登记实现了 ErrorHook 的组成钩子（最外层先登记）。
func (chain *chainHook) collect(hook frameworktelemetry.Hook) {
	if errorHook, ok := hook.(frameworktelemetry.ErrorHook); ok {
		chain.errorHooks = append(chain.errorHooks, errorHook)
	}
}

// Before 实现 telemetry.Hook：委托最外层组成钩子。
func (chain *chainHook) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	return chain.outermost.Before(ctx, action)
}

// After 实现 telemetry.Hook：委托最外层组成钩子。
func (chain *chainHook) After(ctx context.Context, invocation frameworktelemetry.Invocation, effect frameworktelemetry.Effect) error {
	return chain.outermost.After(ctx, invocation, effect)
}

// OnError 实现 telemetry.ErrorHook：按最外层→最内层调用所有实现了
// ErrorHook 的组成钩子并聚合错误；无 ErrorHook 时返回 nil。
func (chain *chainHook) OnError(ctx context.Context, name string, err error, attributes frameworktelemetry.Attributes) error {
	var errs []error
	for _, errorHook := range chain.errorHooks {
		if hookErr := errorHook.OnError(ctx, name, err, attributes); hookErr != nil {
			errs = append(errs, hookErr)
		}
	}
	return errors.Join(errs...)
}
