# Seele 新版框架接入状态（本地 replace）— P2 落地后

- 日期：2026-09-15（P1 跟进 + P2 落地同日更新）
- 方式：`replace github.com/RedHuang-0622/Seele => G:/Program/go/Seele`
- 结论：**P2 已落地**——权限判定整体交给框架 `permission.Gate`，seelex 只保留
  harness 三件套（授权表 / 执行选择页面 / 会话级提权）。全量测试 73 包全绿。

---

## 1. P1 复核（框架侧跟进结果）

| 缺口 | 状态 | 证据 |
| --- | --- | --- |
| B1 审批请求丢会话 | **已修** | `tools/permission/middleware.go`：`ApprovalContext` 新增 `Context context.Context`；`NewApprovalRequest(ctx, name, meta, argsJSON, timeout)` 填齐 `ID`（`appr-<ns>-<seq>` 全局唯一）/ `Preview` / `Risk` / `SessionID`（`WithSessionID` 注入）/ `Timeout`（<=0 取 `DefaultApprovalTimeout` = 2min） |
| B3 `Gate` 不消费 `BitEnforcer` | **已修** | `Gate.Enforcer BitEnforcer`，在 `evaluate` 里**最先**判定；`ok=false` 才落到位/组/规则 |
| B2 无按主体全权 | **无需框架改** | `Gate.Enforcer` 即挂载点：seelex 用它做会话级全权短路（`ok=false` 落回框架判定） |
| B4 审批范围口径 | **由开关表达** | `Gate.DenyWithoutPrompt` 决定「拒绝是否也走选择页面」，默认 false |

## 2. P2 落地形态（与原定义的差异）

原定义是「`Approval: nil` + `DenyWithoutPrompt: true`，外壳自己开选择页面」。P1 之后框架已经能填齐
`ID`/`SessionID`/`Risk`/`Timeout`，**没有理由再把选择页面抢回产品侧**，因此落地形态改为：

- `PermissionGate.Middleware` 由 `tools.Middleware` 改为 `tools.MetaMiddleware`；
- 内部组装 `permission.Gate{Checker, Approval, Enforcer, Timeout, DenyWithoutPrompt: true}` 并调用
  `Gate.Decide` —— 判定、拒绝文案、审批请求组装全部由框架负责；
- seelex 保留的三件事：
  - **授权表** `PermissionChecker`（`Set` 装配，`Set` 时重建）；
  - **执行选择页面** `ApprovalHandler`（= `main.go:newPermissionBridge` → `ApprovalBroker`）；
  - **会话级全权** 实现为 `func (state *PermissionGate) Enforce(...)`：`FullAccessFor(sessionFromContext(ctx))`
    为真则 `ActionAllow, true`（在位/组/规则之前短路），否则 `ok=false` 落回框架判定。
- 会话归属透出：中间件把 `sessionFromContext(ctx)` 同时注入 `WithEngine`（作为授权主体 Subject）与
  `WithSessionID`（作为审批路由键），因此视图单格再次拿到会话 ID。
- `DenyWithoutPrompt: true`：维持 seelex 现行语义 —— 配置里的 `deny` 仍是**立即拒绝**（英文错误、可
  `errors.Is` 归类），只有 `ask`（无命中规则）才弹选择页面。若产品要采纳框架默认的「拒绝也走选择
  页面」，去掉这个开关即可（一处）。

### 改动清单

| 文件 | 改动 |
| --- | --- |
| `seelebridge/tools/registry_state.go` | `NewRegistryState`：事件与权限门改走 `WithMetaMiddleware`（新增 `asMetaMiddleware` 适配器），诊断留普通链，嵌套顺序不变；`Middleware` 改为 `MetaMiddleware` 并委托 `Gate.Decide`；新增 `gate()` 快照组装与 `Enforce()`；删除 `previewArguments`（预览改由框架 `FormatPreview` 生成）；移除 `fmt` 依赖 |
| `seelebridge/tools/permission_framework_gate_test.go` | 新增 3 个用例（拒绝归类 / 工具自带簇属路由 / 框架填齐审批请求） |
| `seelebridge/tools/permission_{session,state,session_isolation}_test.go` | `Middleware(...)` 调用形状补 `ToolMeta` 实参（断言未变） |
| `CHANGELOG.md` | `[Unreleased] / Changed` 记录本次接入 |

## 3. 验证证据

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | 退出码 0 |
| `go vet ./...` | 退出码 0 |
| `gofmt -l seelebridge/tools` | 无输出 |
| `go test ./... -count=1` | **73 个包全部 ok，0 FAIL** |
| `go test -race ./seelebridge/tools/... ./application/approval/...` | ok，无 DATA RACE |
| 9 个既有权限用例 | 全 PASS（全权跨会话不泄漏、Set 后全权不丢、会话归属进审批请求等断言未改） |
| 3 个新增用例 | 全 PASS |

关键新能力已被测试钉住：**同一个工具，声明了 `ToolMeta{Groups:["ro"],Bits:BitRead}` 之后，持有 r 位的
会话被放行、缺位的会话得到 `ErrToolNotVisible`**；且该结论只可能来自工具自带簇属（测试里的组
`Match` 故意不匹配任何工具名）。

## 4. 仍未做（按优先级）

1. **工具尚未登记 `ToolMeta`** —— 现在所有工具都是零值 `ToolMeta`，位/组/主体路由因此**尚未生效**
   （退化为按名字路由，也就是等价旧行为）。下一步是给产品工具批量登记
   `Meta: &ToolMeta{Kind, Groups, Bits}`（Part A 的表）。
2. **`ToolKindControl` 的 root 门必须先处理主体映射** —— `Gate.evaluate` 里
   `meta.Kind == ToolKindControl && subject != SubjectRoot → 不可见`。当前主体 = 会话 ID，永远不等于
   `root`，所以**一旦有工具被声明为 control 类，它会对所有主体不可见**。登记 control 类工具之前，
   必须先把主 agent 会话映射到 `permission.SubjectRoot`（或设置 `Gate.Resolve`）。
3. **审批展示文案变化** —— `Preview` 由「原始参数 JSON（200 截断）」变为框架的 `name(args)`（80 截断）；
   `Risk` 由空变为按名字回退的 `high/medium/low`（未声明 Kind 时）。两者都随全量测试通过，但 GUI 上
   可见，若产品要保留旧文案需在 bridge 里覆盖。
4. **提权台账未接管** —— 仍用框架默认的 `CheckerElevator`；审批响应 `Scope` 为空 = `once`，不持久。
   若要做「记住此工具 / 整会话」的台账，需要 harness 实现 `Elevator` + `Checker.SetElevationSource`。
5. **`replace` 仍是本地路径** —— 发布前必须移除，或等框架发版后改成版本号依赖。
