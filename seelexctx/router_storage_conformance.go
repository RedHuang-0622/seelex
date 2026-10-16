// 消费侧一致性断言（M3-S4）。
//
// `store.RouterStorage` 是「把 Router 当 ContextActor 冷存储」的适配器，方法集
// 与 `lifecycle.Storage[T]` 同形。这条断言必须钉在**消费侧**：
//
//   - 底座不许 import 上层（I6，见 e2e/store_boundary_test.go）——断言原来在
//     `store/router_storage.go`，正是那条被删掉的向上依赖；
//   - 本包同时持有 `seelexctx/lifecycle` 与 `store`，是这条接缝的天然宿主。
//
// 断言由编译器钉住：`lifecycle.Storage` 增删方法、或适配器改签名，这里立刻编译失败，
// 而不是等到冷恢复真把消息写丢才发现。
package seelexctx

import (
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx/lifecycle"
	"github.com/RedHuang-0622/seelex/store"
)

// 编译期断言：RouterStorage 实现 lifecycle.Storage[types.Message]。
var _ lifecycle.Storage[types.Message] = (*store.RouterStorage)(nil)
