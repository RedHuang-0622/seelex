# backend/limit/policy

## 生态位

`policy` 是权限**判据**的**唯一落点**：谁在调用（主体类）、工具属于哪一族（路由组表）、
每一族要哪些位与默认动作（规则表）、某档位要不要放开某一族（档位覆盖）、某个工具该声明
什么簇属（声明路径）、以及"执行壳是什么类别"这份词表——这几件事上一共只有**一份**实现。

它是 limit 板块里"表最多、状态最少"的一层：只有词表与纯函数，没有注册表、没有审批、
没有 IO。

主要调用方：

- `backend/tool/registry`（执行门 `PermissionGate` 的类解析、可见性、工具面与注册表；
  判据搬走后它只调用 `policy.*`）；
- `seelebridge` 根包与 `main.go`（装配期的配置表、壳类别、子代理/员工权限派生）；
- `wiring/adapters`、`application/*`（员工权限装配、工具声明的读面）。

## 职责与非职责

- 做：登记权责模型表（主体 × 路由组 × 位 × 动作）、按壳给规则表、按档位剪 ask 规则、
  把节点作用域/工具策略映射成主体类、把工具名路由到路由组、派生工具簇属。
- 不做：不持有任何运行期状态（角色会话的策略缓存、注册表、审批 broker、当前档位与
  配置快照都在 `backend/tool/registry` 与消费方）；不做权限**判定**（判定委托给上游
  Seele `permission.Gate`）；不做路径闸（`backend/limit/gate` 的 ProjectScope）；
  不做 UI 词表（档位目录的唯一事实在 `contract/dto`，这里只转发）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| [`policy.go`](policy.go) | 主体类词表与位常量、路由组表、主体授权表、规则表（按壳折叠）、主体类映射、按名字路由、glob 匹配 |
| [`tiers.go`](tiers.go) | 档位目录转发与档位覆盖实现（只剪 `ask`，`deny` 永不碰） |
| [`tool_meta.go`](tool_meta.go) | 工具簇属的声明路径（路由组表 → `Kind`/`Groups`/`Resource`），未命中不猜 |
| [`shellkind.go`](shellkind.go) | 执行壳类别词表（`ShellKind` / `ShellBash` / `ShellPowerShell` / `ShellCmd`） |
| [`policy_test.go`](policy_test.go) | 把表本体钉住：组名/位/默认动作/顺序、档位目录单一来源 |

## 核心实现

```text
DefaultPermissionGroupList()      → 6 个路由组（LMRW：最后匹配胜出）
DefaultPermissionSubjects()       → 主体 → 逐组位与 resource 限定
DefaultPermissionRules()          → 规则表（写类 ask、bash 白名单 allow、危险命令 deny）
PermissionRulesForShell(kind)     → 同一张表按**实际壳**折叠 bash 白名单那一层
（内部）bashAllowListForShell     → 该壳的免打断模式（共享槽 + 方言槽；只有
                                    PermissionRulesForShell 的 bash allow 规则用它，
                                    外部经那张规则表读同一份值）
SubjectForClass(class)            → 主体类 → 框架主体 id
ClassForToolsPolicy(toolsPolicy)  → ToolsPolicy 档位 → 员工主体类（未识别 = root）
ClassForNodeScope(scope)          → 节点作用域 → 主体类（子代理 vs root）
ClassForEmployeeGrant(...)        → 逐格授权 → 主体类（员工）
RoutePermissionGroup(groups, name)→ 工具名 → 路由组（未命中 = 未分封）
DeclaredToolMeta(name)            → 工具名 → 簇属声明（从路由组表派生，不新增第二张表）
ToolKindForGroup(group)           → 组名 → 框架簇属类别
DefaultPermissionTiers()          → 档位目录（唯一事实在 contract/dto）
ApplyTier(base, tier)             → 档位覆盖：只删该档要放开的 ask 规则
```

搬迁口径（2026-10-16 M2-C4，来自 `seelebridge/tools`）：**判据正文逐字节未改**，
只有三处名字变了——包名 `tools` ⇒ `policy`、四个位常量 `bitRead/bitWrite/bitExecute/
bitAll` ⇒ `BitRead/BitWrite/BitExecute/BitAll`（它们要跨新边界），以及
`ClassForToolsPolicy` 的形参 `policy` ⇒ `toolsPolicy`（同名会遮蔽包名）。表内容、
顺序、模式字符串、错误文案一律照旧。

## 数据流

```text
config/seelex.yaml 的 permission 段 ──┐
                                      ├─→ DefaultPermission* / PermissionRulesForShell
执行壳解析结果（tools.ResolveShellFact）┘        │
                                                ├─→ backend/tool/registry 的 PermissionGate
档位（dto.PermissionTier*）──→ ApplyTier ────────┤   （判定仍委托框架 permission.Gate）
                                                │
工具名 ──→ RoutePermissionGroup ──→ DeclaredToolMeta ──→ 注册表条目的簇属
```

## 依赖方向

允许：`stdlib`、`github.com/RedHuang-0622/Seele/tools`、
`Seele/tools/permission`、`contract/dto`。

禁止：任何 `seelebridge/*`、`application/*`、`seelexctx*`、`backend/tool*` 依赖。
`ClassForNodeScope` 需要节点作用域形状，因此 `NodeScope` 的类型本体在 `contract/dto`
（2026-10-16 下沉，原 `seelebridge/internal/model` 留 `type X = Y` 别名）——
这条边是**契约层**，不是反向依赖；`backend/limit` 依赖 backend 内部包会破坏 plan §1.2
的分层（而且 `seelebridge/internal/*` 本来就不允许被外部 import）。

## 并发、存储、安全或错误语义

- **无状态**：本包没有可变包级变量；所有函数可并发调用（表是只读字面量）。
- **安全语义**：本包持有的是"谁能做什么"的**表**。改表就是改安全边界，因此
  `policy_test.go` 把组名/位/默认动作/顺序钉住；任何改动都应伴随用例改动。
- **未知不猜**：`RoutePermissionGroup` 未命中 ⇒ 调用方按"未分封"处理；
  `DeclaredToolMeta` 未命中 ⇒ 返回零值簇属（框架退回名字路由，与 K-0 之前一致），
  不猜、不自造分组。未识别档位按 `manual` 处理（写入侧的规范化在 `dto` 已挡）。

## 扩展方式

- 新增工具：改**路由组表**（`DefaultPermissionGroupList` 的 `Match`）即可——簇属声明
  与路由是同一张表派生的，不要另加"工具 → 组"的第二张表。
- 新增档位：先在 `contract/dto` 登记目录（前端按它渲染），再在 `tierStopAskingTools`
  决定剪哪些 ask；`deny` 一律不剪。
- 新增执行壳：在 `shellkind.go` 加常量，并在 `bashAllowPatterns` 补方言槽。

## Review 指南

- 最容易错的是"顺手多写一份表"：任何第二张"工具 → 组"或第二份档位目录都是缺陷。
- `ApplyTier` 只允许**删 ask**；出现新增 allow 或触碰 deny 的改动一律退回。
- `ClassForNodeScope` 只按 `NodeID != "" && Role == dto.RoleSubAgent` 收窄，
  与工具可见性判定同源；改动它等于改子代理的授权面。
- 位的 resource 限定（project/session/desktop）与组 `Mode` 必须一致：
  `DefaultPermissionSubjects` 与 `DefaultPermissionGroupList` 对同一组给同一个 resource。
- 表里的中文注释是判据的一部分（解释"为什么这条在这里"），改判据时同步改注释。

## 测试与验证

```text
gofmt -l backend/limit/policy
go build ./...
go vet ./...
go test ./backend/limit/policy/ ./backend/tool/registry/ ./backend/limit/... -count=1
go test ./e2e/ -count=1          # 模块清单与门面闭包 ratchet
```

判据层的消费侧用例在 `backend/tool/registry`（同包测试：权限主体、员工授权、档位覆盖、
工具簇属、壳方言白名单），它们经 `policy.*` 的导出面覆盖同一份判据。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### policy.go

- `func DefaultPermissionGroupList() []toolspermission.PermissionGroup` — DefaultPermissionGroupList 是默认路由组表：按工具名 glob 路由，组带对象 mode 与默认动作。
- `func DefaultPermissionSubjects() map[toolspermission.Subject]toolspermission.SubjectGrant` — DefaultPermissionSubjects 是默认授权表：主体 → 各组位与 resource 限定。
- `func DefaultPermissionRules() []toolspermission.PermissionRule` — DefaultPermissionRules 是默认的最细粒度覆盖层（args 级）：把"必须问人"和
- `func bashAllowListForShell(kind ShellKind) []string` — bashAllowListForShell 折出这一档壳的免打断模式（共享槽 + 方言槽）。
- `func PermissionRulesForShell(kind ShellKind) []toolspermission.PermissionRule` — PermissionRulesForShell 按**实际壳**给出同一张规则表。
- `func DefaultPermissionConfig() toolspermission.PermissionConfig` — DefaultPermissionConfig 是 manual 模式的默认权责配置：分组 + 主体 + 缺位口径
- `func PermissionConfigForShell(kind ShellKind) toolspermission.PermissionConfig` — PermissionConfigForShell 按**装配期解析出的实际壳**装配默认权责配置（组合根用它）。
- `func SubjectForClass(class SubjectClass) toolspermission.Subject` — SubjectForClass 把主体类映射成框架授权主体 id（= 写入 ctx 的 engine id）。
- `func ClassForToolsPolicy(policy string) SubjectClass` — ClassForToolsPolicy 把员工的 ToolsPolicy 口径映射成主体类：
- `func ClassForNodeScope(scope dto.NodeScope) SubjectClass` — ClassForNodeScope 把工具调度 ctx 的节点作用域映射成主体类：带 NodeID 的
- `func RoutePermissionGroup(groups []toolspermission.PermissionGroup, toolName string) (toolspermission.PermissionGroup, bool)` — RoutePermissionGroup 按名字 glob 路由到路由组（LMRW：最后匹配胜出）。
- `func matchToolGlob(pattern, name string) bool` — matchToolGlob 与框架 permission.matchGlob 同语义：只有 `*` 通配，整串匹配。

### policy_test.go

- `func TestGroupTableShape(t *testing.T)` — TestGroupTableShape 把**路由组表本体**钉住：组名、位、默认动作、顺序。
- `func TestDenyRulesComeLast(t *testing.T)` — TestDenyRulesFormTrailingBlock 钉住"危险命令 deny 在最末"这条顺序判据：
- `func TestTierCatalogHasSingleSource(t *testing.T)` — TestTierCatalogHasSingleSource 钉住"档位目录的唯一事实在 contract/dto"：
- `func TestDeclaredToolMetaNeverGuesses(t *testing.T)` — TestDeclaredToolMetaNeverGuesses 钉住"派生不出来就不猜"：

### tiers.go

- `func DefaultPermissionTiers() []dto.PermissionTierInfo` — DefaultPermissionTiers 返回档位目录（跨层词表的唯一事实在 dto）。
- `func ApplyTier(base toolspermission.PermissionConfig, tier string) toolspermission.PermissionConfig` — ApplyTier 把档位声明式覆盖到 base 权责配置上，返回应用后的配置。
- `func tierStopAskingTools(tier string) map[string]bool` — tierStopAskingTools 返回该档位下"不再问人"的工具名集合（空 = 不覆盖）。

### tool_meta.go

- `func DeclaredToolMeta(name string) frameworktools.ToolMeta` — DeclaredToolMeta 返回某工具**声明的**簇属：按名字在默认路由组表里路由，
- `func ToolKindForGroup(name string) frameworktools.ToolKind` — ToolKindForGroup 把路由组翻译成框架的簇属类别。

