# teammate 的「按 plugin 装配」：实现落地（exec / work item `wi-impl`）

> 日期：2026-10-05 · 角色：exec（milestone `m-build`）
> 契约来源：`docs/devlog/2026-10-05-teammate-plugin-assembly-design.md`（arch）
> 一手外部调研：`docs/research/2026-10-05-teammate-plugin-assembly-external-research.md`
> 定位：**实现与证据**。逐条对照 leader 的六条裁决 + 设计稿 C1–C13；列出改动的 file:line、
> 自测用例、以及**没做/做不到的部分**（诚实标注）。

---

## 0. 一句话结论

装配的接缝落在**一处**：`seelebridge/tools/policy.go` 的插件收口从"无 ctx 的全局闭包"
换成"按 ctx 解析会话装配的闭包"。集合经**构造**进本轮 ctx（`tools.WithRolePlugins`，
与 `WithEmployeeGrant` 同姿势），每轮现算、任何句柄上都不缓存。工具面 = 权限面 ∩ 插件面
（插件只做减法），技能目录按同一集合渲染并附读数进回执与审计行。空集 = 不覆盖：**早退到
宿主那条老闭包**（同一条路，不是等价实现），system 字节逐字不变。

---

## 1. 六条裁决逐条对照

| # | 裁决 | 落地 | 位置 |
|---|---|---|---|
| 1 | 每会话插件上限 **3**，做成配置 `limits.plugins.per_teammate`，超出**显式拒绝** | `PluginLimits.PerTeammate`（yaml `plugins.per_teammate`，默认 3）+ `dto.NormalizePlugins(names, limit)` 超限报错（不截断） | `seelexctx/limits.go:164,257-276,330-331,477-478,536`；`application/contract/dto/plugin_assembly.go:24,34`；`application/core/agentteam/spec.go:117-121`；`seelebridge/runtime_role_plugins.go:135`（`validateMemberPlugins`） |
| 2 | 目录字节闸门：>6k token 或 > 窗口 2% → **回执给黄牌读数，不拒**，但装配回执**必须带读数** | `pluginsCatalogTokenWarn=6000` / `pluginsCatalogWindowPercent=2` + `assemblyViews` 逐成员读数（`skill_catalog_runes`/`skill_catalog_tokens_est`/`yellow`/`yellow_reason`）；`team_plan` 回执新增 `plugin_limit_per_teammate` 与 `assemblies` | `seelebridge/runtime_role_plugins.go:40-50,230-296`；`seelebridge/runtime_teamwork.go:298-302` |
| 3 | 空集语义：**接受不对称**（工具面继承宿主 + 目录不注入），**回执与日志显式写明**；不用指针字段区分 `plugins:[]` | 工具面：ctx 解析不出集合即早退 `r.plugins.Filter`；目录：`roleSkillCatalog` 空集返回 `""`；回执 `mode: inherit-host`；审计行 `plugins=<role>=inherit-host`/`none` | `seelebridge/runtime.go:418-434`；`seelebridge/runtime_role_plugins.go:185-196,230-262`；`seelebridge/teamwork/coordinator.go:68,72-100` |
| 4 | **权限不随插件走**；工具面 = 权限面 ∩ 插件面（只收窄） | 收口顺序写死：先 `ToolFace`（权限/主体）后 `PluginFace`（插件）；插件带 MCP/Prompt/权限一律不接 | `seelebridge/tools/policy.go:84-95`；用例 `seelebridge/tools/role_plugins_test.go:80-110` |
| 5 | 优先序写死：**集合**由 leader 显式声明**替换**；**定义**优先序 `角色自带 > 精选目录 > 宿主内建` | `rolePluginAssembly`：显式声明（plan/WorkerRequest）→ 角色自带（`SetRolePluginsProvider`）→ 空集；`skill.PluginSkillsFor` 同名先到先得（本轮只有两层真实存在） | `seelebridge/runtime_role_plugins.go:95-104`；`skill/skill.go:195-216` |
| 6 | 未知插件名**显式拒绝**（不静默忽略） | `validateMemberPlugins` 在 `team_plan` 里比对 `r.plugins.Defined`，报错文案写明"显式拒绝，不静默忽略" | `seelebridge/runtime_role_plugins.go:113-139`；`seelebridge/runtime_teamwork.go:283` |

### 设计稿 C1–C13 对照

- **C1 声明面**：`dto.RoleSpec.Plugins`（`agentteam.go:106`）+ `dto.TeamMember.Plugins`（`:166`，回读面在 `factory.go:399-401`）+ `sessionstore.TeamworkMember.Plugins`（`teamwork.go:116`）+ schema（`runtime_teamwork_schema.go:34-37`）。✔
- **C2 语法层**：`NormalizeRole` 调 `dto.NormalizePlugins`（`spec.go:117`）。✔（上限用出厂值；配置值在编排入口拦，见 §4 偏差 2）
- **C3 存储关口**：结构校验（空白项/重复显式拒绝），**不比对插件目录、不重复声明上限**（`teamwork.go:278-291`）。✔（与设计稿写法有偏差，理由见 §4）
- **C4 schema**：`members[].plugins` + 描述写明"未知名显式拒绝 / 对已开会话自下一轮生效"。✔
- **C5 语义校验**：`teamPlanHandler` → `validateMemberPlugins`。✔
- **C6 透传链**：`WorkerRequest.Plugins`（`teamwork/teamwork.go:88`）→ 派发两处（`coordinator.go:160`、`items.go:374`）→ `roleRoundSpec.Plugins`（`runtime_role_turn.go:149`）→ `workerRoleRoundSpec`（`runtime_teamwork.go:622-629`）。✔
- **C7/C8 接缝**：`PolicyDeps.PluginFace`（带 ctx；`PluginFilter` 保留给未接装配的宿主/测试桩，nil 语义不变）+ `runtime.go` 注入闭包。✔
- **C9 纯函数**：`plugin.VisibleName` / `plugin.Face`（并集/硬拆），`Filter` 与 `active` 一字未动。✔
- **C10 员工读面**：**未采用**（不做 `EmployeeFromContext` 反查）——集合直接按构造进 ctx，比反查更可靠，也少一个权限门读面。见 §4 偏差 3。
- **C11/C12 生效面**：`turnCtx = WithRolePlugins(...)`（`runtime_role_turn.go:285`）+ 目录段追加进 system prompt（`:261-265`），并在每轮起手重设 prompt（`:303`），因此**目录与工具面都自下一轮生效**（比契约 3 的实现建议更强，见 §4 偏差 4）。✔
- **C13 用例与文档**：4 组用例（见 §3）；`seelebridge/plugin` 包注释改口径（`plugin.go:5-14`）；`05-plugin-dual-track-decision.md` 追加 §7（不推翻单选裁决）。✔

---

## 2. 改动清单（file:line）

**新增**

| 文件 | 内容 |
|---|---|
| `seelebridge/tools/role_plugins.go` | ctx 载体：`WithRolePlugins`(:25)、`RolePluginsFromContext`(:50) |
| `seelebridge/runtime_role_plugins.go` | 解析（`SetRolePluginsProvider`:70、`rolePluginAssembly`:95、`normalizePluginNames`:104）、上限（`maxPluginsPerTeammate`:161）、目录（`roleSkillCatalog`:185）、**两道显式拒绝**（`validateMemberPlugins`:145）、读数（`assemblyViews`:230、`pluginDefs`:274、`pluginsCatalogYellow`:283、`appendSkillCatalog`） |
| `application/contract/dto/plugin_assembly.go` | `MaxPluginsPerRole`(:24)、`NormalizePlugins`(:34) |
| `seelebridge/runtime_role_plugins_test.go`、`seelebridge/tools/role_plugins_test.go`、`seelebridge/plugin/plugin_face_test.go`、`application/core/agentteam/spec_plugins_test.go`、`sessionstore/teamwork_plugins_test.go` | 自测用例（§3） |

**修改**

| 文件:行 | 改动 |
|---|---|
| `seelebridge/plugin/plugin.go:92,105,159,181` | `Defined` / `DefsFor` / `VisibleName` / `Face`（只读投影；`active`、`Filter` 不动）；包注释:`5-14` 补"按会话集合"口径 |
| `seelebridge/tools/policy.go:17-25,88-96` | `PolicyDeps.PluginFace`（带 ctx）+ 收口顺序（权限面 → 插件面） |
| `seelebridge/runtime.go:119-134,418-434` | `skills` 改 `atomic.Pointer`（两个读面并发）+ `rolePluginsMu/rolePlugins` 读面 + `PluginFace` 闭包（空集早退、未知名失灵） |
| `seelebridge/ports.go:425-437` | `SetSkillRegistry` 同时存 `r.skills`（员工目录读面），再转 `node.SetSkills` |
| `seelebridge/runtime_role_turn.go:143-149,261-265,285,303` | `roleRoundSpec.Plugins`；解析集合 + 追加目录；集合进 ctx；每轮重设 system prompt |
| `seelebridge/runtime_teamwork.go:281-285,298-302,622-629` | `team_plan` 两道显式拒绝；回执带读数；`workerRoleRoundSpec` 透传 `Plugins` |
| `seelebridge/runtime_teamwork_schema.go:34-37` | `members[].plugins` + 描述 |
| `seelebridge/teamwork/teamwork.go:82-88`、`coordinator.go:68,72-100,160`、`items.go:374` | `WorkerRequest.Plugins` + 两处透传 + 计划审计行带 `plugins=` 摘要 |
| `sessionstore/teamwork.go:113-116,278-291` | 成员字段 + 结构校验 |
| `application/contract/dto/agentteam.go:97-106,140-166` | `RoleSpec.Plugins`（声明面）+ `TeamMember.Plugins`（回读面） |
| `application/core/agentteam/spec.go:117-121`、`factory.go:399-401`、`registry.go:102-118`、`agentteam_service.go:667-678` | 写入侧规整 + 回读 + `PluginsFor` + `AgentTeamRolePlugins`（召唤路径读面） |
| `main.go:418-429` | `runtime.SetRolePluginsProvider(...)`（与 `SetRolePromptProvider` 并列） |
| `seelexctx/limits.go:163-164,257-276,330-331,477-478,536` | `limits.plugins.per_teammate` 块 + 默认 3 + 负值报错 |
| `skill/skill.go:195-216` | `PluginSkillsFor`（只读按集合取技能；`Get/All` 掩蔽语义不变） |
| `docs/2026-08-14-decoupling/05-plugin-dual-track-decision.md` §7 | 追加"按会话集合已落地，不推翻全局单选" |

`plugins/**`、`main.go` 的插件根解析、`e2e/layout_test.go` **未碰**（shipper 领地）。

---

## 3. 自测用例（都在所改包的 `_test.go` 里）

| 用例 | 钉什么 |
|---|---|
| `plugin.TestFaceSingleDefMatchesFilter` | 单元素集合与 `Filter` 同解（不是第二套语义） |
| `plugin.TestDefsForReportsMissingNames` | 未知名可被显式拒绝（含 nil Manager 防护） |
| `plugin.TestFaceUnionOfIncludesAndExcludeVeto` | include 并集 + exclude 硬拆；空集合原样返回 |
| `tools.TestRolePluginsContextCarrier` | 空集 = 不进 ctx（"没装配"与"装配零个"同一形态） |
| `tools.TestPolicyPluginFaceTakesPrecedenceOverHostFilter` | 按会话收口优先；只接老闭包时行为不变 |
| `tools.TestPolicyPluginFaceCannotWidenPermissionFace` | **权限不随插件走**（插件全接纳也不能放回被权限面挡掉的工具） |
| `seelebridge.TestPluginAssemblyEmptySetMatchesHostFace` | 空集与旧收口**同一条路**（逐元素比较）+ 目录不注入 |
| `seelebridge.TestPluginAssemblyIsolatedPerTurnContext` | 两个 teammate 不同集合不串味；重复求值稳定；全局 active 不被改 |
| `seelebridge.TestPluginAssemblyUndefinedNameFailsClosed` | 声明后被撤销 ⇒ 失灵（空面），不静默放宽 |
| `seelebridge.TestRoleSkillCatalogAndAssemblyReadings` | 目录段形状 + 字节/token 读数 + `replace`/`inherit-host` |
| `seelebridge.TestRolePluginAssemblyPrefersExplicitOverRoleDefault` | 优先序：显式替换 → 角色自带 → 空集 |
| `seelebridge.TestValidateMemberPluginsRejectsUnknownAndOverCap` | 未知名/超上限显式拒绝 + 规整写回 + 空集写回 nil |
| `seelebridge.TestRunRoleRoundAssemblesPlugins` | 装配真的落到回合：ctx 有集合、prompt 有目录；**空集两次 prompt 都是原始字节** |
| `agentteam.TestNormalizeRoleNormalizesPlugins` | 写入侧语法口径 + 超限拒绝 + 空/缺失保持 nil |
| `sessionstore.TestValidateTeamworkPlanPluginStructure` | 存储关口：重复/空项拒绝；空/缺失合法 |

---

## 4. 与设计稿的偏差（都写在这里，不藏在代码里）

1. **C10 未采用**：不做 `EmployeeFromContext` 之类的反查读面。集合**按构造**进本轮 ctx
   （与 `WithEmployeeGrant` 同一姿势，`runtime_role_turn.go` 文件头那条"按构造授权永远比
   按反查授权可靠"的理由同样适用），少一个跨域读面。
2. **上限只在一处判死**：写入侧（`NormalizeRole`）用**出厂值 3**，配置值
   （`limits.plugins.per_teammate`）在编排入口（`team_plan`）判。存储层不重复声明数字，
   只做结构校验——两处数字迟早在配置改大时打架。
3. **未知名在“执行面”的处置是我新加的一条裁决**（设计稿没写）：`Filter` 签名不能返回
   错误，所以名字在声明后被 `Undefine` 掉时，`PluginFace` **失灵**（空工具面）而不是
   静默回退宿全面——回退等于丢掉收窄（= 放宽），且会让"装了什么"变成不可审计的事实。
   声明期的正常路径仍是显式拒绝（C5）。
4. **目录也"自下一轮生效"**：设计稿只承诺工具面每轮重算、目录在建会话时定死。我在每轮
   起手加了 `engine.SetSystemPrompt(spec.SystemPrompt)`（同值幂等），于是装配集合变更对
   **目录**也自下一轮生效；代价是每轮一次字符串赋值，收益是"工具面变了、目录还是旧的"
   这种半生效状态不存在。空集时两次设置的都是原始字节（用例钉住）。
5. **目录段尾部补了一行纠正**：`RenderSkillCatalog` 的尾句写死"属于 active plugin、切插件
   就变"——那是宿主口径，员工面既没有 `switch_plugin` 位，集合也来自装配声明。复用同一
   函数（字节口径一份）+ 追加一行说明；读数按**追加后的实际字节**报。

---

## 5. 证据（命令与输出）

```
$ go build ./...
（无输出 = 通过）

$ go vet ./...
（无输出 = 通过）

$ go test ./seelebridge/... ./sessionstore/ ./skill/ ./seelexctx/ ./application/core/agentteam/ -count=1
ok  github.com/RedHuang-0622/seelex/seelebridge           27.254s
ok  github.com/RedHuang-0622/seelex/seelebridge/plugin     1.408s
ok  github.com/RedHuang-0622/seelex/seelebridge/tools     18.949s
ok  github.com/RedHuang-0622/seelex/seelebridge/teamwork   1.968s
ok  github.com/RedHuang-0622/seelex/sessionstore          39.747s
ok  github.com/RedHuang-0622/seelex/skill                  1.235s
ok  github.com/RedHuang-0622/seelex/seelexctx              0.928s
ok  github.com/RedHuang-0622/seelex/application/core/agentteam 1.165s
（其余 seelebridge/* 子包同批全 ok）

$ go test ./e2e/ -count=1
ok  github.com/RedHuang-0622/seelex/e2e  0.331s

$ gofmt -l <本次触碰的 LF 文件>
（无输出；runtime_role_turn.go 等本就 CRLF 的文件按仓库现状不在 gofmt 口径内）
```

---

## 6. 没做 / 做不到（明确列出）

1. **MCP 轴、插件的 Prompt、teammate 的 `switch_plugin`/`skill_activate`**：设计稿 §5 已定
   本轮不做，本实现也没接（ADM 位仍是 0）。
2. **`config/seelex.yaml` 未写显式键**：`limits.plugins.per_teammate` 已可用（缺省 0 →
   出厂 3），但出厂档里没有这一行字面量——`config/**`/`e2e/layout_test.go` 是 shipper 的
   面，登记这一行需要与它协调（否则两边同时改同一份配置说明）。
3. **同名插件的定义覆盖层（契约 6 第二层）**：只把优先序写进口径（`PluginSkillsFor` 先到
   先得），没有新的解析层。
4. **GUI 装配界面**：`TeamMember.Plugins` 已随成员表下发（前端可回填），但面板渲染/编辑
   未动（非本工作项）。
5. **端到端未跑**：隔离性在 seelebridge 层用 ctx 直接验证（含两个 ctx 的面不同、重复求值
   稳定、全局 active 不被改）；**没有**用真实进程 + 两个 teammate 并发回合端到端复现一遍
   （需要真模型与 GUI，属验收面）。
6. **根包 `go test .`**（`teamwork_headless_smoke_test.go` 那批）未在本轮跑：跨包成本高，
   留给 leader 的收口验证。
