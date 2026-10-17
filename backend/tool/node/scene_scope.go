package node

import (
	"context"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// SceneScope 是契约层场景作用域的同身份别名：node 域是执行者，测试与节点装配
// 可以直接从本包取用，避免每个调用点同时引两个包。类型本体仍在 contract/dto。
type SceneScope = dto.SceneScope

// WithSceneScope / SceneScopeFromContext / SceneScopeFromContextOrEmpty 是同一
// 契约函数的同身份转发。
var (
	WithSceneScope               = dto.WithSceneScope
	SceneScopeFromContext        = dto.SceneScopeFromContext
	SceneScopeFromContextOrEmpty = dto.SceneScopeFromContextOrEmpty
)

var _ func(context.Context, SceneScope) context.Context = WithSceneScope
