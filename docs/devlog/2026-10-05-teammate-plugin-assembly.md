# teammate 插件装配：权限 × 能力插件两轴专业化（交付）

> goal：`g-5`｜team_id：`teamwork-plugin-assembly`｜6 个 teammate / 4 个里程碑
> （`m-seam` 接缝与契约 → `m-build` 实现与测试 → `m-proof` 对抗复核 → `m-fix` 复核发现项的修复与再证）
> 外部一手调研（我亲自用 Playwright 实读）：`docs/research/2026-10-05-teammate-plugin-assembly-external-research.md`
> 本文件是**交付文档**：把「做成了什么、凭什么这么说、还剩什么」写在一处。所有 `file:line` 以本文件落盘时的 HEAD 为准。

---

## 0. 一句话

**teammate 的装配现在有两个正交轴**：**权限轴**（`tools_policy` / `permission_groups`，只能由 leader/宿主给）
× **能力轴**（`plugins`，可以随插件分发）。于是同一个 coding harness 上可以并存多个不同专业方向的 Agent，
且「挂了哪些能力包」是**每个 teammate 各自的**，不是全局开关。

---

## 1. 为什么两轴必须分开（不是设计偏好，是有外部硬规则先例的）

我实读 Claude Code 官方文档（2026-10-05，`https://code.claude.com/docs/en/sub-agents`）拿到一条被产品写死的边界：

> "For security reasons, **plugin subagents don't support the `hooks`, `mcpServers`, or `permissionMode`
> frontmatter fields. These fields are ignored when loading agents from a plugin.**"

即：**随"别人给的包"进来的东西不许携带权限面**。这条被我们采纳为设计裁决并落进代码：

- 工具面 = **权限面 ∩ 插件面**，插件**只做减法**、永不放宽（`seelebridge/tools/policy.go:84-96`，先用例钉住"插件全接纳也放不回被权限面挡掉的工具"）；
- **口径写准的地方**（复核者指出、已改正）："只收窄"只相对**权限面**成立，**不相对宿主装配**——
  `plugins/default/plugin.md` 的 include/exclude 皆空，所以一个 inherit-host 的 teammate 声明 `plugins:["default"]`
  就能拿到全工具面。这段话已写进 `runtime_role_plugins.go` 与 `runtime_teamwork_schema.go:36` 的注释。

---

## 2. 装配链路（全部可核对）

| 环节 | 落点 |
|---|---|
| 声明（编排入口） | `team_plan` 的 `members[].plugins`（schema：`seelebridge/runtime_teamwork_schema.go:36`） |
| 声明（召唤入口） | `dto.RoleSpec.Plugins`（`application/contract/dto/agentteam.go`，与 `ToolsPolicy`/`PermissionGroups` 并列） |
| 落盘 | `sessionstore.TeamworkMember.Plugins`（`sessionstore/teamwork.go`），回读 `application/core/agentteam/factory.go` |
| 透传三跳 | `seelebridge/teamwork/coordinator.go:160`（`Dispatch`）→ `items.go:374`（`DispatchItem`）→ `runtime_teamwork.go:697`（当前 HEAD 行号；复核时是 629） |
| 生效（工具面） | 按 ctx 解析：集合按构造进本轮 ctx（`runtime_role_turn.go:285` `WithRolePlugins`），收口在 `runtime.go` 的 `PluginFace`，**每轮现算、不缓存** |
| 生效（能力目录） | `seelebridge/runtime_role_plugins.go`：每轮把该会话插件集合的技能目录（name+description）注入 system prompt |
| 拒绝面 | 未知名 / **重复声明** / 超上限（`limits.plugins.per_teammate`，出厂 3）/ pending 未落盘 / 精选目录读不到 —— 一律**显式拒绝，不静默** |
| 读数 | `team_plan` 回执带 `assemblies`（逐成员 runes/token 估算/`plugin_face_tools`/`total_tools`/yellow）与 `plugin_limit_per_teammate`；**失灵**单独一形态（`faulted` + `note`） |

**空集语义（刻意的不对称，已声明）**：`plugins` 缺失/为空 = **不覆盖** —— 工具面继承宿主当前装配、技能目录不注入。
证法是"与旧收口逐元素/逐字节相等"，不是"跑绿了"。

---

## 3. 「精选」与「跨构建可见」

### 3.1 精选目录 `plugins/curated.yaml`

三条铁律（机检，不靠人眼）：
1. **只列已落盘插件**（entries ↔ 插件根双向一一对应，多一条少一条都红）；没装的东西只能出现在 `presets[].pending`，
   而 `pending` 是**路线图不是可用集合**——装配到它必须**显式拒绝**，文案点名 `pending` + 上游 + 实读页。
2. **禁止重复 include/exclude**（那是 `plugin.md` 的事实源）；**权限档只出现在 preset**。
3. 每条 entry 必有 `source{kind,url,license,pinned,read_at}`：一眼看出哪个插件是谁给的。

**转正四问**（机器可引用版本：`plugin/curated.go` 的 `CuratedPromoteQuestions`）：
① 可装载性 ② 不带权限面 ③ 许可证 ④ 成本（每轮进上下文的 name+description 体积）。

### 3.2 跨构建可见（"观察者式广播"）

- **根责任链**：`-plugins` > `$SEELEX_PLUGINS` > `<exe>/plugins` > `<exe>/../plugins` > `plugins`(CWD)，多根去空去重保序、first-wins（`main.go`）。
  这解决的是原失败链：root 缺失 → 加载器静默 continue → 空表不报错 → 找不到 default 就 `return nil`
  ⇒ **零插件零技能启动且无提示**。现在零插件**显式拒绝启动**，并把试过的每个根与两条救法都列出来。
- **一份字节两处引用**：`pluginRootReport` 生成的同一条启动读数同时进终端日志与 `app.AddNotice`（UI 面）。

### 3.3 我实读时发现的两条事实（会影响"从哪收"）

- `https://github.com/anthropics/frontend-design` → **HTTP 404**；`https://github.com/anthropics/skills` → **存在**
  （"Public repository for Agent Skills"，顶层含 `.claude-plugin`/`skills`/`spec`/`template`）。
  ⇒ **"在榜单上看见"与"能取到正文"之间隔着一整步**：候选转正前必须先做**可达性核验**，再做四问。
- 开源侧**不是"还没有"**：`topic/claude-code-skills` 实读页面原话 "2,031 public repositories matching this topic"；
  `VoltAgent/awesome-agent-skills`（1000+ skills，自述 "Hand-picked, not AI-slop generated."）里直接列着
  `mblode/agent-skills`（"Nobody ships AI slop on purpose. These skills make sure you don't. UI audits, typography…"）
  ——**用户设想的"去 AI 味 / 前端 UI / 渲染性能"这一类能力，开源侧已经有人在供给**。
  稀缺的不是 skill 数量，而是**装配到"某一个 teammate 会话"上**、**一条可核的装载门禁**、**同一台机多个构建读同一份精选**。

---

## 4. 对抗复核：找出什么、修了什么、还剩什么

复核（`wi-proof`）把三条验收从"通过"改成"有条件通过"，并逐条给 `file:line`。四条缺陷**全部已修**且有修前/修后实测：

| # | 缺陷（复核原文） | 修复后的证据 |
|---|---|---|
| **A** | 运行期**无人读** `curated.yaml`（`LoadCuratedFromRoot`/`ActivateFromCatalog` 只在测试里）⇒ 精选只是声明面 | `resolveCuratedRead` 从已解析的插件根读一次并注入判决；判决四路（目录没读到 / entries 有而进程没有 / pending / 谁都不认识）；**被拒计划不落盘**（校验早于 SetPlan）；"已装插件不进判决函数"有零调用断言 |
| **C** | 读数与运行事实**两套判据**：撤销插件后**回执报满面（5/5）、运行面为空（0）**；inherit-host 报 0/0 | 修前探针逐字复现，修后 `① 1==1 ② 0==0`；判据收敛为 `pluginFaceJudgement` 一处，失灵独立成 `Faulted` 形态并在**派发回执**可观察 |
| **D** | 重复声明**静默去重**，`sessionstore/teamwork.go` 那句"显式拒绝"**不可达**（旁有死代码 `_ = pluginNames`） | 修后原文：`插件 "docs" 重复声明（显式拒绝，不静默去重）` 且计划未落盘；死代码已删；`RoleSpec` 与编排侧共用同一份判据 |
| **G** | 派发**三跳零用例**——而"派发时带了、执行时丢了"正是本特性自定的失败模式（权限在相同位置有对应测试） | 补齐三跳断言并做**变异检验**：分别删掉那一跳后报 `Dispatch 的载荷丢了装配声明` / `DispatchItem 的载荷丢了装配声明` / `这一轮的 ctx 装配 = []，want [docs]` |

**口径更正（改声明不改行为）**：F（"只收窄"不相对宿主装配，见 §1）；I（`SourceSummary` 目前是**门禁面**，生产 0 调用）。

**仍未实证 / 未覆盖（如实保留）**：
1. **B 是"源码守卫"而非实证**——"根读数进 UI 面"目前由 `strings.Contains(main.go, …)` 断言，**没有执行路径真到过那行 `AddNotice`**；
   彻底闭环需要 GUI 级 e2e（超出本轮边界）。这是复核者独立给出的"最短的板"，我采纳为本轮第一项遗留。
2. 变异阴性对照**未由复核者复跑**（其工具面只读，写类命令被拒）；G 的硬证据只有作者的留痕 + 复核者的成对断言判别力论证。
3. 真进程端到端（起真二进制看精选目录被读、pending 被拒）未做。
4. 会合点测试只覆盖"同进程两会话并发"，**不覆盖**：同 RoleSessionID 换装配、计划整份替换后已派发未完成作业、召唤路径闭包、跨进程。
5. 失灵没有独立日志面（本包无 logger 端口）；宿主面读数是"那一刻"的事实，全局 switch 后旧读数会过时。

**与本次交付无关、但复核者与我各自跑出的先存缺陷（登记不修）**：
`seelexctx TestPrefixChainLockContentionGate` 与 `-count≥2` 不兼容（mutex profile 进程级累积，A 臂读到上一轮 B 臂样本）
⇒ **"全仓 `-count=3` 全绿"这条证据口径不成立**，本轮按 `-count=1` 收。它挡的正是更强的证据。

---

## 5. 本轮明确不做

- 去开源平台**实际收集/编写**前端类 skill（用户明说后期收集与补充）——本轮只定形状 + 门禁 + 候选池。
- marketplace 的网络拉取 / 签名 / 版本解析（只做本地精选清单）。
- 改权限模型本身（`permission_groups` / `tools_policy` 语义未动）。
- GUI 装配面板；`plugin_face` 读数挂到 `team_join` 收口回执；给 seelebridge 加 logger 端口。

## 6. 流程教训（写给下一轮）

1. **并发工作项会把后来者钉在移动靶上**：`wi-assemble-test` 跑证据时另一个工作项正在改同一文件
   （`CuratedPreset.Pending` 由 `[]string` 变 `[]CuratedPending`，首次编译即错）。→ 本轮证据一律附
   `git hash-object` / `git rev-parse HEAD` 留档，说明"跑在哪一版"。
2. **行号会漂**：复核时 `runtime_teamwork.go:629` → 修完已到 `:697`。→ 引用行号必须与被引用的 HEAD 绑定。
3. **设计稿会落后于落地**：设计稿 §5 写了"精选目录本轮不做"，但两条装配闸实际已落地——本文件已更正。
   → 设计与实现跨里程碑时，落地文档要显式覆盖设计稿的"不做"清单。

---

## 7. 交付清单与证据索引

**代码/配置（未提交，待裁决）**：见 `git status`。新增：`application/contract/dto/plugin_assembly.go`、
`seelebridge/runtime_role_plugins.go`、`seelebridge/tools/role_plugins.go`、`seelebridge/runtime_teamwork_curated.go`、
`plugin/curated.go`、`plugins/curated.yaml`、`seelebridge/assembly_chain_test.go`、`seelebridge/runtime_role_prompt_matrix_test.go`、
`seelebridge/teamwork/items_plugins_test.go`、`seelebridge/runtime_role_plugins_fault_test.go`、若干 `*_test.go`。
**leader 亲自改的**：`config/seelex.yaml` 与 `internal/bootseed/assets/config/seelex.yaml` 登记 `limits.plugins.per_teammate: 3`。

**我亲自跑过的验证（不是转述 teammate 的）**：
- `go build ./...` + `go vet ./seelebridge/... ./plugin/...` → 干净（exit 0）
- `go test ./seelebridge/ -run AssemblyChain -race -count=3` → `ok 1.301s`（无 DATA RACE）
- 全量相关包 `go test ./seelebridge/... ./sessionstore/... ./skill/... ./seelexctx/... ./application/... ./plugin/... ./e2e/ . -count=1` → **全 `ok`，exit=0**

**分项文档**：设计 `2026-10-05-teammate-plugin-assembly-design.md`｜实现 `…-impl.md`｜测试证据 `2026-10-05-assembly-chain-test-evidence.md`、
`2026-10-05-prompt-reset-nonempty-pin.md`｜精选接入 `2026-10-05-curated-into-runtime-and-root-reading.md`｜
复核发现项修复 `2026-10-05-anti-review-cdg-fixes.md`｜外部调研 `docs/research/2026-10-05-teammate-plugin-assembly-external-research.md`
