package e2e

import (
	"sort"
	"strings"
	"testing"
)

// 本文件是 docs/2026-10-15-architecture-reshape/plan.md 的 **I6** 机械断言，
// 并按 docs/2026-10-27-store-ddd-split/milestones.md §4 M0-6 扩到
// `./store/...` **全量**包：
//
//	I6 store 无向上依赖 —— 底座 `store/**` 的 Seelex 内依赖闭包只允许是它自己；
//	   `seelexctx/*`、`application/*`、`seelebridge/*`、`backend/*`、`middle/*` 一律不许出现。
//
// 与 `facade_boundary_test.go` 的 I1 同款手法（`go list -deps` + 只许收缩的允许清单），
// 但判据相反：那边管"门面能碰到谁"，这边管"底座碰不到谁"。
//
// 为什么需要它（而不是靠口头约定）：M3-S2 把数据根锁 / 原子发布 / 共享读 / 残尾 /
// `Settings` 下成 `store/root`，其价值只有在"闭包 = 纯标准库"成立时才存在；
// 一旦有人在底座里 import 一次 `backend/engine/context/seelectx` 或 `store`，依赖方向就倒过来。
//
// 为什么改成**枚举**而不是人肉清单：Store 拆分（M1–M9）会不断新增 `store/<领域>` 包，
// 手写清单意味着每加一个包就得记得同步登记一次——漏登记就等于该包完全不受 I6 约束，
// 而漏登记在评审里看不出来（清单少一行，测试照样绿）。现在默认对 `go list ./store/...`
// 的每个包零允许，登记表只用来记录**已知例外**。
//
// 允许清单仍是 **ratchet（只许收缩）**：`store` 那条 `kernel/event` 向上边是
// "把事件端口下沉 Contract"之前的历史遗留，删掉后对应 scope 的允许清单归零、上限跟着降；
// 除此之外只许减、不许加。

// storeBoundaryScope 是一个受 I6 约束的包族。
type storeBoundaryScope struct {
	// name 是报错文案里的可读名字。
	name string
	// pattern 是 `go list` 的包匹配式（`.` + 去掉模块前缀的包路径）。
	pattern string
	// allow 是当前**已知待清理**的 Seelex 内依赖（只许收缩）。
	allow []string
	// max 是 allow 的规模上限；收缩后必须一起调小，防止清单变成历史包袱登记表。
	max int
	// note 说明为什么这些条目还没清掉。
	note string
}

// storeBoundaryRegisteredScopes 只登记**有例外**的包；其余 `./store/...` 包按默认零允许判定。
//
// 2026-10-27 实测闭包（`go list -deps <pkg>` 去掉本包、`store/*`、`contract/*`）：
//
//	store            → kernel/event
//	store/root       → （无）
//	store/schedule   → kernel/event
//	store/workspace  → internal/winhide
var storeBoundaryRegisteredScopes = []storeBoundaryScope{
	{
		name:    "store",
		pattern: "./store",
		allow: []string{
			"github.com/RedHuang-0622/seelex/kernel/event",
		},
		max: 1,
		note: "M3-S4 删掉了唯一一条真正的向上边（`router_storage.go` 的 `seelexctx/lifecycle` " +
			"`Storage[types.Message]` 编译期断言）：断言整条挪到消费侧 " +
			"`seelexctx/router_storage_conformance.go`。2026-10-17 P3 后 `contract` 闭包只剩" +
			"`contract/{dto,protocol}` 与 kernel/event；该 event 边来自 `Dependencies.Events` " +
			"的 Hub 形状，待 Store 拆分前把事件端口下沉 Contract。",
	},
	{
		name:    "store/root",
		pattern: "./store/root",
		note:    "M3-S2 抽出的基座机制：闭包 = 纯标准库（`go list -deps ./store/root` 除自身外无带点的包）。",
	},
	{
		name:    "store/schedule",
		pattern: "./store/schedule",
		allow: []string{
			"github.com/RedHuang-0622/seelex/kernel/event",
		},
		max: 1,
		note: "2026-10-16 M4-C 抽出的定时任务全局 JSONL。实测传递闭包只有标准库 + " +
			"`contract/{dto,protocol}` + `kernel/event`（与 `store` 同一条事件端口边），" +
			"此前注释里“闭包会带进 application/*、seelexctx/*”的说法已不成立。",
	},
	{
		name:    "store/workspace",
		pattern: "./store/workspace",
		allow: []string{
			"github.com/RedHuang-0622/seelex/internal/winhide",
		},
		max: 1,
		note: "项目与会话绑定、git 读写。`internal/winhide` 是 Windows 子进程窗口隐藏的" +
			"机制叶子（只有 stdlib + syscall，自身闭包为零 Seelex 依赖），位在 store 之下，" +
			"不是业务上层；登记它而不是放过整个 `internal/` 前缀，是为了让新依赖照样要显式登记。",
	},
}

// storeBasePrefix 是底座自身的路径前缀：底座内部互相依赖是允许的。
const storeBasePrefix = seelexModulePrefix + "/store/"

// TestStoreDependencyClosureHasNoSeelexUpwardDependency 钉 I6 的主判据。
func TestStoreDependencyClosureHasNoSeelexUpwardDependency(t *testing.T) {
	for _, scope := range resolveStoreBoundaryScopes(t) {
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
			// contract 是零实现依赖的底座词汇（2026-10-17 P3 后其闭包只剩
			// 纯形状包）：store 依赖 contract/dto 是**向下**，不是向上。
			if dependency == seelexModulePrefix+"/contract" ||
				strings.HasPrefix(dependency, seelexModulePrefix+"/contract/") {
				continue
			}
			if _, ok := allow[dependency]; ok {
				continue
			}
			t.Errorf("%s 的依赖闭包出现了向上的 Seelex 依赖（I6：底座只许向下）：\n  %s（%s 层）\n"+
				"底座不得反向依赖 %s；要么改掉这条依赖，要么在 storeBoundaryRegisteredScopes 登记并说明为什么还没清掉。\n"+
				"scope 说明：%s", scope.name, dependency, seelexLayerOf(dependency), seelexLayerOf(dependency), scope.note)
		}
	}
}

// TestStoreTestFilesImportNothingUpward 钉 I6 的第二半：**测试文件也不许向上**。
//
// 为什么单独一条：`go list -deps` 不含测试文件的 import，而"测试里 import 一下上游"
// 恰恰是底座腐化的常见入口（M3-S4 要搬的 `router_storage_test.go` 就是这种形状）。
func TestStoreTestFilesImportNothingUpward(t *testing.T) {
	for _, scope := range resolveStoreBoundaryScopes(t) {
		allow := stringSet(scope.allow)
		for _, imported := range goListTestImports(t, scope.pattern) {
			if !strings.HasPrefix(imported, seelexModulePrefix) {
				continue
			}
			if imported == scope.patternPath() || strings.HasPrefix(imported, storeBasePrefix) {
				continue
			}
			if imported == seelexModulePrefix+"/contract" ||
				strings.HasPrefix(imported, seelexModulePrefix+"/contract/") {
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

// TestStoreBoundaryEnumeratesEveryStorePackage 证明门禁覆盖的是"枚举出来的全部包"，
// 而不是登记表里那几个名字。
//
// 判据自己也要有失败面：如果 `go list ./store/...` 被改窄（比如写成 `./store/root`
// 或加上 build tags 把包过滤掉），主判据会静默漏判——这里先钉住枚举结果。
func TestStoreBoundaryEnumeratesEveryStorePackage(t *testing.T) {
	scopes := resolveStoreBoundaryScopes(t)
	byPattern := make(map[string]storeBoundaryScope, len(scopes))
	for _, scope := range scopes {
		if _, seen := byPattern[scope.pattern]; seen {
			t.Fatalf("%s 被重复判定：解析逻辑把同一个包收了两遍", scope.pattern)
		}
		byPattern[scope.pattern] = scope
	}

	// 已知的四个包必须出现在枚举结果里（M1–M9 新包会在此之后继续追加，不走白名单）。
	for _, pattern := range []string{"./store", "./store/root", "./store/schedule", "./store/workspace"} {
		if _, ok := byPattern[pattern]; !ok {
			t.Fatalf("枚举结果缺少 %s：I6 的匹配式必须覆盖 store 全部子包，不能只判登记过的包", pattern)
		}
	}

	// 未登记的包必须拿到零允许：否则"新包默认宽松"会让 I6 失去意义。
	registered := make(map[string]struct{}, len(storeBoundaryRegisteredScopes))
	for _, scope := range storeBoundaryRegisteredScopes {
		registered[scope.pattern] = struct{}{}
	}
	for _, scope := range scopes {
		if _, ok := registered[scope.pattern]; ok {
			continue
		}
		if len(scope.allow) != 0 || scope.max != 0 {
			t.Fatalf("%s 未登记却带上了允许清单 %v（上限 %d）：未登记包的默认必须是零允许",
				scope.pattern, scope.allow, scope.max)
		}
	}
}

// resolveStoreBoundaryScopes 枚举 `./store/...` 的每个包并与登记表合并。
//
// 登记表里失效的条目（包已删除/改名）会报错：残留条目会让 ratchet 记录失真。
func resolveStoreBoundaryScopes(t *testing.T) []storeBoundaryScope {
	t.Helper()
	packages := runGoList(t, "list", "./store/...")
	if len(packages) == 0 {
		t.Fatal("go list ./store/... 没有返回任何包：I6 门禁会变成空转")
	}
	pending := make(map[string]storeBoundaryScope, len(storeBoundaryRegisteredScopes))
	for _, scope := range storeBoundaryRegisteredScopes {
		pending[scope.pattern] = scope
	}
	scopes := make([]storeBoundaryScope, 0, len(packages))
	for _, packagePath := range packages {
		if packagePath != seelexModulePrefix+"/store" && !strings.HasPrefix(packagePath, storeBasePrefix) {
			t.Fatalf("go list ./store/... 返回了 store 之外的包 %s：匹配式被改宽了", packagePath)
		}
		pattern := "." + strings.TrimPrefix(packagePath, seelexModulePrefix)
		scope, ok := pending[pattern]
		if ok {
			delete(pending, pattern)
		} else {
			scope = storeBoundaryScope{
				name:    strings.TrimPrefix(pattern, "./"),
				pattern: pattern,
				note:    "未登记在 storeBoundaryRegisteredScopes：默认零允许（新领域包不得引入任何向上的 Seelex 依赖）。",
			}
		}
		scopes = append(scopes, scope)
	}
	for _, leftover := range pending {
		t.Errorf("登记了 %s，但 go list ./store/... 已不再返回它：条目失效，请从登记表删除", leftover.name)
	}
	sort.Slice(scopes, func(left, right int) bool { return scopes[left].pattern < scopes[right].pattern })
	return scopes
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
