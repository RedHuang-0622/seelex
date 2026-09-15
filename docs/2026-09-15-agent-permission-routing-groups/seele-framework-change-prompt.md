# Seele 框架改动提示词 — 权限子系统升格为「主体 × 路由组 × 位 + sudo」

> 用法：把本文件（`---` 之间的正文）整段粘给在 **Seele 框架仓库**里工作的 agent。
> 配套设计依据：`docs/2026-09-15-agent-permission-routing-groups/README.md`（Part B 需求单）。

---

## 任务

把 Seele 的 `tools/permission`（及 `tools/gateway`、`tools` 的元数据面）从**扁平 allow/ask/deny 规则**
升格为 **Linux 式权限模型**：`主体（user/group/other）× 路由组（工具簇）× 位（rwx）+ sudo`。
目标：产品层（Seelex）不再需要维护任何旁路的「工具可见性硬编码名单」，只用框架就能表达
「谁能用哪些工具、以什么位、是否需要 sudo 口令」。

## 仓库与硬约束

- 目标仓库：`github.com/RedHuang-0622/Seele`，本任务改动集中在 `tools/` 包树。
- **只做加法（additive）**，绝不破坏现有调用方（Seelex 目前 pin `v0.2.0`）。
  下面「现状」列的每个符号都必须继续**原样编译通过**（可标注 deprecated，不能删、不能改签名）。
- 改动完成后版本升 `v0.3.0`，并在 `CHANGELOG.md` 记一条。
- 文档：按仓库 `AGENTS.md` 指向的 `docs/DOCUMENTATION_STANDARD.md` 更新
  `tools/README.md`、`tools/permission/README.md`（模块 README 必改）。
- 验证必须真跑：`go test ./tools/...`、`go vet ./tools/...` 全绿；新增逻辑必须有 table-driven 测试。
- 一切新类型零值必须安全（缺省 = 现有行为）。

## 现状（必须对齐的真实符号，勿臆造）

```
tools/tools.go
  type ToolEntry struct { Definition types.Tool; Handler ToolHandler; OutputSchema map[string]interface{}; Metadata map[string]string }
  type ToolProvider interface { ProviderName() string; Tools() []ToolEntry }
  type Middleware func(name string, next ToolHandler) ToolHandler
  type Registry struct{...}; NewRegistry(...); WithCallTimeout; WithMiddleware; WithDispatchRetries
  func (r *Registry) Dispatch(ctx, ToolCall) (string, error)
  var ErrToolNotFound / ErrDuplicateTool / ErrInvalidEntry / ErrUnavailable
  // chain(name, handler, middlewares) 在 rebuild 时闭包捕获工具名

tools/permission/types.go
  type Mode string; ModeFullAccess | ModeManual
  type Action string; ActionAllow | ActionAsk | ActionDeny
  type PermissionRule struct { ToolName string; Patterns []string; Action Action }
  type PermissionConfig struct { Mode Mode; Rules []PermissionRule }
  func (PermissionConfig) EffectiveMode() Mode   // 空 = full_access
  type ApprovalRequest struct { ID, ToolName, Arguments, Preview, Risk string; Options []ApproveOption; Timeout time.Duration; SessionID string }
  type ApprovalResponse struct { RequestID, Choice string; Remember bool; Timestamp time.Time }
  type ApprovalHandler func(*ApprovalContext) (*ApprovalResponse, error)
  func DefaultApproveOptions() []ApproveOption

tools/permission/checker.go
  type CheckResult int; ResultAllow | ResultAsk | ResultDeny
  func NewPermissionChecker(PermissionConfig) *PermissionChecker
  func (pc *PermissionChecker) Check(toolName, argsJSON string) CheckResult   // ← 无主体
  func (pc *PermissionChecker) AddAllowRule(toolName, argsJSON string)
  func matchGlob(pattern, name string) bool   // 已支持 * 通配（可复用）

tools/gateway/default.go
  type DefaultGateway struct{...}
  func (g *DefaultGateway) Dispatch(ctx, name, argsJSON) (string, error)   // → checkPermission(ctx,name,argsJSON) → pc.Check(name,argsJSON)
  func (g *DefaultGateway) checkPermission(ctx, name, argsJSON string) error
  func assessRisk(name string) string   // ← 又一个硬编码工具名 switch（与本任务同类反模式）
  func (g *DefaultGateway) SetPermissionConfig(PermissionConfig, ApprovalHandler)

tools/holder/holder.go
  type Holder struct{...}; IsPluginActive(); Plugin().Filter(tools); Dispatch(...)
```

## 需求（R1–R8，按此实现）

### R1 主体维度进 Checker（必改）
- 新增 `type Subject string`（框架不枚举产品角色，只做不透明标识；`""` = 匿名/进程级）。
- 新增 `func (pc *PermissionChecker) CheckFor(subject Subject, toolName, argsJSON string) CheckResult`。
- 保留 `Check(toolName, argsJSON)`，实现为 `CheckFor("", toolName, argsJSON)`，**行为与今天完全一致**。
- 新增 `type SubjectResolver func(ctx context.Context) Subject`，并让网关/Registry 能装上它：
  `WithSubjectResolver`（给 `Registry`）或 `DefaultGateway.SetSubjectResolver`。
- 验收：`CheckFor` 对不同 subject 可返回不同结果；`Check` 旧测试一个不改仍全绿。

### R2 工具元数据：kind / group / 位 / 可见主体（必改）
- `ToolEntry` **已有** `Metadata map[string]string`——优先用它承载，避免大改结构；同时新增可选
  `Meta *ToolMeta`（指针，零值安全）供强类型使用。两者至少实现其一，推荐 `Meta`：
```go
type ToolKind string // "read" | "write" | "control" | "admin"
type ToolMeta struct {
    Kind       ToolKind // 必填语义：读写/控制/管理
    Groups     []string // 路由组名（可与 Kind 冗余，产品可自定义）
    Bits       uint8    // 该工具所需位：r=4 w=2 x=1
    Visibility []string // 可见主体；空 = 全员可见
    Resource   string   // "project" | "desktop" | ""（共享资源限定）
    Signal     string   // 仅 Kind=control：term|stop|chld|pause
}
```
- **关键**：`Middleware` 必须能读到被装饰工具的元数据。现在签名是
  `func(name string, next ToolHandler) ToolHandler`，只有名字。两种做法择一：
  - a) 新增 `type MetaMiddleware func(name string, meta ToolMeta, next ToolHandler) ToolHandler` +
    `WithMetaMiddleware(...)`，在 `chain`/`rebuildLocked` 里传入 `entry.Meta`；
  - b) 或提供 `WithRegistryOption` 让 middleware 从 registry 快照按名查元数据。
- 验收：中间件能按 `meta.Kind`/`meta.Groups` 路由；未声明 `Meta` 的旧 provider 不受影响（零值）。

### R3 组（group）作为规则一等维度（必改）
```go
type PermissionGroup struct {
    Name     string   // 组名，如 "ro" / "rw" / "ctl" / "adm"
    Match    []string // 工具名 glob（复用现有 matchGlob）
    Mode     uint8    // 该组所需位
    Default  Action   // 位齐时的默认动作
    Resource string   // 可选
}
type PermissionConfig struct {
    Mode       Mode                      // 保留
    Rules      []PermissionRule          // 保留：最细粒度覆盖层
    Groups     []PermissionGroup         // 新增
    Subjects   map[Subject]SubjectGrant  // 新增
    MissingBit Action                    // 新增：缺位策略（零值语义见 R4）
}
```
- Checker 求值顺序：**先 `route(toolName) → group`**（名字 glob，多组命中取最后一个=与 Rules 一致的 LMRW），
  再判位，最后**再套 `Rules`**（Rules 永远最后、最细，覆盖组默认）。
- 验收：不写 `Rules`、只写 `Groups` 就能表达「一组工具统一动作」；`Groups` 为空时退化为纯 Rules 旧行为。

### R4 位（bits）与动作（action）分离（必改）
- 位定义 `const (BitRead uint8 = 4; BitWrite uint8 = 2; BitExecute uint8 = 1)`。
- 主体授权：
```go
type SudoMode string // "none" | "password" | "nopasswd"
type GrantBit struct { Bits uint8; Resources []string } // Resources 空 = 全部
type SubjectGrant struct { Bits map[string]GrantBit; Sudo SudoMode } // group -> grant
```
- 判定：`grant(subject, group).Bits & group.Mode == group.Mode` 且 resource 匹配 → 位齐；
  否则走 `MissingBit`（**零值必须等价今天的 `ask`**；`deny` 语义 = 不可路由，见 R7）。
- `PermissionRule` 增可选 `Bits *uint8`（nil = 不按位判，保持旧语义）。
- 验收：位运算表 `(subject,group) → allow/ask/deny` 有 table-driven 测试覆盖全 4×4 组合。

### R5 升级（sudo / elevate）原语（必改）
- `ApprovalResponse` 增 `Scope string`（`"once" | "tool" | "session" | "args"`；**空 = 等价旧行为 once**）。
- 新增审计面（不绑定 UI）：
```go
type ElevationEvent struct { Subject Subject; Group, Tool, Reason string; Scope string; Granted bool; At time.Time }
type ElevationAuditor func(ElevationEvent)
func (pc *PermissionChecker) SetElevationAuditor(ElevationAuditor)
```
- 允许产品实现「一次放行 / 记住此工具 / 整会话」；框架只透传 `Scope` 并回调审计。
- 验收：`Scope="once"` 不放宽后续调用；`Scope="session"` 只在同一 subject+group 内复用；审计每次提权必发。

### R6 控制类工具的「信号」语义（建议但本任务要做）
- `ToolMeta.Kind == "control"` 的工具，gate 默认**仅 root（subject 带全局位/管理员组）可路由**。
- `ToolMeta.Signal` 透出给上层 loop：`term`（正常停机）/`stop`（挂起）/`chld`（在途打点）/`pause`（转人工）。
- 验收：control 类工具对受限 subject 表现为不可路由；`Signal` 可被上层读取。

### R7 两类「不可用」语义统一（建议但本任务要做）
- 在 `tools` 包新增哨兵错误：`var ErrToolNotVisible = errors.New("tool not visible to subject")`、
  `var ErrPermissionDenied = errors.New("permission denied")`（后者可 wrap 旧文案）。
- 语义：
  - **断位** ⇒ `ErrToolNotVisible`（等价「不在 PATH」），且该工具**不出现在可见工具列表**；
  - **位齐但被 Rules/审批拒** ⇒ `ErrPermissionDenied`（等价 EPERM）。
- `DefaultGateway.checkPermission` 必须改为返回上面两类可 `errors.Is` 辨别的错误（替换现在
  「permission denied: tool %q is not allowed by policy」的一把抓文案）。
- 验收：调用方 `errors.Is` 能区分两种；`VisibleTools(ctx)` 不再返回断位工具。

### R8 位与沙箱/路径的挂载点（建议但本任务要做）
```go
type BitEnforcer interface {
    Enforce(ctx context.Context, subject Subject, meta ToolMeta, toolName, argsJSON string) (Action, bool)
}
```
- 产品层注入它来定义「r/w 只作用于项目根、x 只作用于命令白名单」；框架只负责位比较与路由。
- 安装点：`WithBitEnforcer(...)`（Registry）或 `DefaultGateway.SetBitEnforcer(...)`；返回 `ok=false` 表示框架不干预。
- 验收：装/不装行为可辨；框架内不出现任何路径/命令解析逻辑。

## 顺带清理（同源反模式）

- `tools/gateway/default.go` 的 `assessRisk(name)` 硬编码工具名 switch → 改为优先读 `ToolMeta.Kind`，
  仅在无 Meta 时回退旧 switch（不删旧分支）。

## 非目标（别做）

- 不在框架里实现产品配置加载（YAML 解析留在 Seelex）。
- 不在框架里实现 UI / 审批弹窗 / 会话归属（`SessionID` 语义保持不变）。
- 不引入 OS 级沙箱、容器或路径校验（那是产品的 `ProjectScope`）。
- 不改 `Registry`/`ToolProvider`/`Dispatcher` 的既有签名；不改 `ToolEntry` 既有字段含义。
- 不动 `workplan` / `agent` / `mcp` / `microhub` 的行为。

## 兼容性铁律（自查清单）

- [ ] 现有 `PermissionConfig{Mode:..., Rules:...}` 字面量仍编译，行为不变。
- [ ] `Check(toolName, argsJSON)` 仍存在且等价 `CheckFor("", ...)`。
- [ ] `NewPermissionChecker` / `AddAllowRule` / `DefaultApproveOptions` / `ApprovalRequest` 字段不变。
- [ ] `ToolEntry` 新增字段是**可选指针**，用命名初始化的旧字面量仍编译。
- [ ] `tools.Registry` 的 `WithCallTimeout` / `WithMiddleware` / `WithDispatchRetries` 行为不变。
- [ ] `go build ./...`、`go test ./tools/...`、`go vet ./tools/...` 全绿；无新增 vet 警告。

## 交付物

1. 代码：上述 R1–R8 的 additive 实现 + table-driven 测试（`tools/permission/checker_test.go`、
   `tools/gateway/*_test.go`、`tools/tools_test.go` 增补）。
2. 文档：更新 `tools/README.md`、`tools/permission/README.md`（新增公开接口表 + 调用示例），
   新增一份设计说明（放 `docs/`，遵循 `docs/DOCUMENTATION_STANDARD.md`），并在 `CHANGELOG.md` 记 `v0.3.0`。
3. 迁移指南：一段「Seelex v0.2.0 → v0.3.0 如何把 `Check` 换成 `CheckFor` + 用 `Groups/Subjects` 替换硬编码名单」的说明。

## 完成判据（自证，逐条给证据）

- [ ] 给出 `CheckFor` 在 `subject ∈ {main, sub}` × `group ∈ {ro, rw, ctl, adm}` 的判定矩阵测试输出。
- [ ] 给出「只写 Groups 不写 Rules」即可表达分组默认动作的测试。
- [ ] 给出 `ErrToolNotVisible` 与 `ErrPermissionDenied` 的 `errors.Is` 区分测试。
- [ ] 给出 `Scope=once|session` 的提权行为差异测试 + 审计回调被调用的断言。
- [ ] 给出 `VisibleTools` 不再包含断位工具的测试。
- [ ] 贴出 `go test ./tools/...`、`go vet ./tools/...` 的实际输出。
