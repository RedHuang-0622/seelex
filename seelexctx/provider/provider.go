// Package provider 抽象上下文来源接口及内置实现。
//
// 核心接口：
//   - Provider     — 上下文导出（Export + Name）
//   - Mergable     — 双向合并（MergeBack）
//   - Compactable  — 上下文压缩（Compact）
//
// 内置实现：
//   - EngineProvider — 从 engine.Engine 导出
//   - TraceProvider  — 从 tracer.Tree 自动提取结构信息
package provider

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// Provider 抽象上下文来源。
type Provider interface {
	Export(ctx context.Context) (*snapshot.ContextSnapshot, error)
	Name() string
}

// Mergable 可合并接口（双向上下文继承）。
type Mergable interface {
	MergeBack(parent, child *snapshot.ContextSnapshot) error
}

// Compactable 可压缩接口。
type Compactable interface {
	Compact(snap *snapshot.ContextSnapshot, budget int) (*snapshot.ContextSnapshot, error)
}

// Compile-time checks
var (
	_ Provider = (*EngineProvider)(nil)
	_ Provider = (*TraceProvider)(nil)
)
