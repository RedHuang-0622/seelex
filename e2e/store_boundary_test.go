package e2e

import (
	"strings"
	"testing"
)

// 本文件是 docs/2026-10-15-architecture-reshape/plan.md 的 **I6** 机械断言：
//
//	I6 store 无向上依赖 —— 底座 `store/**` 的 Seelex 内依赖闭包只允许是它自己；
//	   `seelexctx/*`、`application/*`、`seelebridge/*`、`backend/*`、`middle/*` 一律不许出现。
//
// 与 `facade_boundary_test.go` 的 I1 同款手法（`go list -deps` + 只许收缩的允许清单），
// 但判据相反：那边管"门面能碰到谁"，这边管"底座碰不到谁"。
//
// 为什么需要它（而不是靠口头约定）：M3-S2 把数据根锁 / 原子发布 / 共享读 / 残尾 /
// `Settings` 下成 `store/root`，其价值只有在"闭包 = 纯标准库"成立时才存在；
// 一旦有人在底座里 import 一次 `seelexctx` 或 `store`，依赖方向就倒过来，
// 而这种事在代码评审里恰恰最容易溜过去（看起来只是"复用一下"）。
//
// 允许清单是 **ratchet（只许收缩）**：`store` 那条 `seelexctx/lifecycle` 向上边
// 是 M3-S4 的删除目标（`router_storage.go` 的适配器上移 + 编译期断言挪到消费侧），
// 删掉后对应 scope 的允许清单归零、上限跟着降；除此之外只许减、不许加。

// storeBoundaryScope 是一个受 I6 约束的包族。
type storeBoundaryScope struct {
	// name 是报错文案里的可读名字。
	name string
	// pattern 是 `go list` 的包匹配式。
	pattern string
	// allow 是当前**已知待清理**的 Seelex 内依赖（只许收缩）。
	allow []string
	// max 是 allow 的规模上限；收缩后必须一起调小，防止清单变成历史包袱登记表。
	max int
	// note 说明为什么这些条目还没清掉。
	note string
}

var storeBoundaryScopes = []storeBoundaryScope{
	{
		name:    "store/root",
		pattern: "./store/root",
		note:    "M3-S2 抽出的基座机制：闭包 = 纯标准库（`go list -deps ./store/root` 除自身外无带点的包）。",
	},
	{
		name:    "store",
		pattern: "./store",
		note: "M3-S4 删掉了唯一一条向上边（`router_storage.go` 的 `seelexctx/lifecycle` " +
			"`Storage[types.Message]` 编译期断言）：断言整条挪到消费侧 " +
			"`seelexctx/router_storage_conformance.go`，底座只剩与上层无关的实现。因此清单为空。" +
			"（设计稿 A 的「契约层端口」路线在本工作区不可行：`contract/ports.go` import " +
			"`seelexctx/search`、后者 import `store` ⇒ 会成环；见 m3-record.md D-M3-11。）",
	},
}

// storeBasePrefix 是底座自身的路径前缀：底座内部互相依赖是允许的。
const storeBasePrefix = seelexModulePrefix + "/store/"

// TestStoreDependencyClosureHasNoSeelexUpwardDependency 钉 I6 的主判据。
func TestStoreDependencyClosureHasNoSeelexUpwardDependency(t *testing.T) {
	for _, scope := range storeBoundaryScopes {
		if len(scope.allow) > scope.max {
			t.Errorf("%s 的允许清单有 %d 条，超过上限 %d：这张表只许收缩。"+
				"新增条目必须先删旧条目（I6）", scope.name, len(scope.allow), scope.max)
		}
		allow := stringSet(scope.allow)
		for _, dependency := range goListDependencies(t, scope.pattern) {
			if !strings.HasPrefix(dependency, seelexModulePrefix) {
				continue
			}
			if dependency == scope.patternPath() || strings.HasPrefix(dependency, storeBasePrefix) {
				continue
			}
			if _, ok := allow[dependency]; ok {
				continue
			}
			t.Errorf("%s 的依赖闭包出现了向上的 Seelex 依赖（I6：底座只许向下）：\n  %s（%s 层）\n"+
				"底座不得反向依赖 %s；要么改掉这条依赖，要么在 storeBoundaryScopes 登记并说明为什么还没清掉。\n"+
				"scope 说明：%s", scope.name, dependency, seelexLayerOf(dependency), seelexLayerOf(dependency), scope.note)
		}
	}
}

// TestStoreTestFilesImportNothingUpward 钉 I6 的第二半：**测试文件也不许向上**。
//
// 为什么单独一条：`go list -deps` 不含测试文件的 import，而"测试里 import 一下上游"
// 恰恰是底座腐化的常见入口（M3-S4 要搬的 `router_storage_test.go` 就是这种形状）。
func TestStoreTestFilesImportNothingUpward(t *testing.T) {
	for _, scope := range storeBoundaryScopes {
		allow := stringSet(scope.allow)
		for _, imported := range goListTestImports(t, scope.pattern) {
			if !strings.HasPrefix(imported, seelexModulePrefix) {
				continue
			}
			if imported == scope.patternPath() || strings.HasPrefix(imported, storeBasePrefix) {
				continue
			}
			if _, ok := allow[imported]; ok {
				continue
			}
			t.Errorf("%s 的测试文件 import 了向上的 Seelex 包（I6）：\n  %s（%s 层）\n"+
				"底座用例只许用标准库、底座自身与本 scope 已登记的条目。",
				scope.name, imported, seelexLayerOf(imported))
		}
	}
}

// patternPath 把 `./store/root` 这样的匹配式还原成可比较的包路径。
func (scope storeBoundaryScope) patternPath() string {
	return seelexModulePrefix + strings.TrimPrefix(scope.pattern, ".")
}

// seelexLayerOf 返回依赖所属的顶层板块名，只用于报错文案。
func seelexLayerOf(dependency string) string {
	trimmed := strings.TrimPrefix(dependency, seelexModulePrefix+"/")
	if index := strings.Index(trimmed, "/"); index >= 0 {
		return trimmed[:index]
	}
	return trimmed
}
