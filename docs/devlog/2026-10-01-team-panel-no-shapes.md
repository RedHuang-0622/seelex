# GUI 团队面板：撤掉「团队形态」与人工编排（2026-10-01 · 提交二）

> **口径**：前一笔是 Go 侧（`7b0db19`「删除内置团队形态目录，团队只从数据来」）。本文记的
> 是 **GUI 侧**（面板 + headless 残留）的**修前事实**（可核对）、**改法**与**回归证据**。
> 结论分 **Confirmed**（有代码 / 用例 / 命令证据）与 **Hypothesis**（待验证，写明验证方式）。

---

## 1. 范围：为什么前端必须跟着改

上一笔删掉的是 Go 侧的一整层：`agentteam/presets.go`（三支内置形态）、`Preset`/`Presets`、
`Service.AgentTeamPresets` / `MaterializeAgentTeamPreset`、`Bridge.AgentTeamPresets`、
headless `team.presets`、`@` 的内置形态分支、goal 上线自动装配。前端没跟上，于是**同一份事实
在两处说法不同**：

- 面板还在调一个已经不存在的 RPC（`AgentTeamPresets`），并在团队库下渲染"内置形态 chip 行"；
- 面板还提供"人来编排团队"的手势（拖拽调序 / 位置列 / 顺序策略 chip）——而 leader-worker
  目标态里**顺序归 leader 的 team plan**（`stages[].depends_on`），面板要说的只是"谁在编"；
- headless 侧留了一个已删方法的接口残留，**真机 `team.*` 全废**（§2.4）。

本次只改前端与 headless 残留，不动 Go 侧行为、不动 `dto`/contract。

## 2. 修前事实（Confirmed）

| # | 位置 | 修前事实（`7b0db19` 可核对） |
|---|---|---|
| 1 | `gui/frontend/dist/app.js` | 面板加载即 `loadAgentTeamPresets()` → `invoke("AgentTeamPresets")`（**后端已无此方法**）；`let agentTeamPresets` 存形态列表；`[data-team-template]` 分支 → `fillAgentTeamFormFromPreset`（内置形态起手只填 团队名 / 形态 / 顺序策略 + 成员**名**）；`agentTeamDragRole` / `agentTeamDragSource` 状态 + 一整套拖拽区（dragstart / `agentTeamDropTarget` / dragover / dragleave / drop / `dropAgentTeamMember` / `dropAgentTeamOrder` / dragend / `clearAgentTeamDropMarkers`，约 118 行）；`writeTeamFormShape` / `writeTeamFormPolicy` |
| 2 | `gui/frontend/dist/agent-team-view.js` | `POLICY_LABEL` / `orderPolicyLabel` / `ORDER_POLICY_LEGACY_NOTE`；`renderAgentTeam(view, library, global, presets)` 带 presets 实参；团队库下的"内置形态退成一行 chip"；员工栏行首 `grip` 手柄 + 位置列 + 底部"顺序末尾"落区；`agentTeamOrderForDrag`（拖拽位置纯函数） |
| 3 | `gui/frontend/dist/styles.css` | `.team-preset*` / `.team-drag-handle` / `.team-drop-end` / `.team-policy-static` / `.team-field-static` / `.team-template-picks` / `.team-member-idx` / `.team-member-pos` + 拖拽宿主状态类（`.is-dragging` / `.is-drop-target` / `::after` / `data-team-drop-label`） |
| 4 | `gui/headless_team.go:17` | 接口 `teamRPCApplication` 里留着 `AgentTeamPresets() []dto.TeamSpec`，而 `*application.Service` 已无此方法 → `dispatchTeam` 的 `server.app.(teamRPCApplication)` 对**真实 Service 必然失败**，`team.materialize/view/put_role/delete_role/set_order/instantiate_role` 全体退化成"当前 Application 未装配 AgentTeam 管理扩展面"。`gui/headless_team_test.go` 的假实现同时留着 `AgentTeamPresets` 与 `MaterializeAgentTeamPreset`，**把这个缺口盖住了**：默认 `go test ./gui` 全绿 |

### 2.4 headless 残留的 RED / GREEN（Confirmed）

一次性核查程序 `tmp/teamiface/main.go` 用真实 `*application.Service` 对两种接口形状各断言一次：

```
$ go run ./tmp/teamiface
*application.Service 满足 修前接口（含 AgentTeamPresets）: false
*application.Service 满足 修后接口（去掉该法）      : true
```

为什么没被既有测试抓到：三个真实 API 探针都带环境门控（`SMOKE_TEAM_LIVE` /
`SMOKE_GOAL_TEAM_LIVE` / `SMOKE_TEAM_WORK_COMPUTER_LIVE`），默认不跑；跑的是假实现。
所以修法不只是一行删除，还要**把这条漂移变成编译错误**——按 `gui/bridge.go` 的同款做法加：

```go
var _ teamRPCApplication = (*application.Service)(nil)
```

（`bridge.go` 里那段注释记的是同一种事故：方法名漂移成 `AgentTeamMaterializePreset`，
只有假实现满足它，真机一调就断言失败。）

## 3. 改法

| 文件 | 动作 |
|---|---|
| `gui/frontend/dist/app.js` | 删 `loadAgentTeamPresets` / `agentTeamPresets` 实参 / `fillAgentTeamFormFromPreset` / `[data-team-template]` 分支 / 整套拖拽区 / `writeTeamFormShape` / `writeTeamFormPolicy` / `agentTeamDragRole` / `agentTeamDragSource`；import 收紧（去掉 `agentTeamOrderForDrag`、`orderPolicyLabel`）；**新增「入职」分支**（`data-team-employee-hire` → `agentTeamRolePayload` → `AgentTeamInstantiateRole`）；注释同步（次序 = 登记先后、写 `order_roles` 的唯一动作是「摘除」） |
| `gui/frontend/dist/agent-team-view.js` | 删 `POLICY_LABEL` / `orderPolicyLabel` / `ORDER_POLICY_LEGACY_NOTE` / `agentTeamOrderForDrag`；`renderAgentTeam(view, library, global)` 去 presets；团队库去"内置形态 chip 行"；员工栏去行首手柄 / 位置列 / 落区，动作只留 ✕ 摘除 + 删除 + 点名字看会话；员工库行加「入职」；`nextAgentTeamOrder` 注释改为"面板不提供任何调序通道"；头注重写（没有"团队形态"这个设定 / 顺序不是人编排的） |
| `gui/frontend/dist/styles.css` | 删 12 处规则 + 5 处列表摘项与 6 处拖拽宿主状态类；`.team-member-item` 列模板改 `minmax(0,1fr) auto auto 18px` 并去掉 `cursor: grab`（原 16px 序号列会把成员名压扁）；段落注释同步 |
| `gui/frontend/dist/agent-team-view.test.mjs` | 删 presets 夹具与全部 `, presets,`（20 处）/ `, presets)`（3 处）实参；按新口径改写 13 条断言；删 `orderPolicyLabel` / `agentTeamOrderForDrag` 两条用例；员工库拖拽用例改成「入职」 |
| `gui/headless_team.go` / `gui/headless_team_test.go` | 删接口方法 `AgentTeamPresets`、假实现里的 `AgentTeamPresets` / `MaterializeAgentTeamPreset`；加编译期断言（§2.4） |

## 4. 落进实现与文档的两条口径（用户要求）

1. **`order_policy` 只读、面板连展示都不给**：它是只回读、不驱动轮次的历史字段（上一轮
   还留过虚线只读框与只读 chip，本轮一并撤掉——摆在那里只会让人以为可配置）。它仍作为
   **隐藏字段** `data-team-form-policy` 原样回传；提交顺序时由 `agentTeamCurrentPolicy`
   把既有取值带上（`AgentTeamSetOrder` 的 `order_policy` 参数不变）。
2. **发言顺序不由人编排**：次序 = 员工**登记（入职 / 装配）的先后**，面板不留任何调序
   通道（拖拽、位置列、↑/↓ 都删）；目标态归 leader 的 team plan 的 `stages[].depends_on`。
   `team_kind` 只是团队名的展示别名，不是设定（面板既不给输入框，也不显示形态 chip）。

这两条同时写进 `gui/frontend/README.md`（模块表 + Agent Team 面板一节）、
`docs/gui/modules/right-sidebar.md`（四块表的"行内动作"与顺序一段）、
`docs/gui/modules/shell-and-interactions.md`（`@` 一节）、`docs/gui/decisions.md`（ADR-GUI-021 后果）、
`docs/arch/a2a-agent-team-factory.md`（前端交互口径）与
`docs/arch/teamwork-leader-worker-architecture.md`（M4 行）。

## 5. 回归证据（Confirmed）

| 命令 | 结果 |
|---|---|
| `node --test gui/frontend/dist/*.test.mjs` | **558 pass / 0 fail**（其中 `agent-team-view.test.mjs` 42 条） |
| `node --check gui/frontend/dist/agent-team-view.js` / `app.js` | 通过 |
| `go run ./tmp/teamiface` | 修前接口 `false` / 修后接口 `true`（§2.4） |
| `go test ./gui -run TestHeadlessTeamRPC -count=1 -v` | `--- PASS: TestHeadlessTeamRPC (0.02s)`；`ok github.com/RedHuang-0622/seelex/gui 0.577s` |
| `go build ./...` | exit 0（编译期断言成立） |
| `go build -tags "gui,desktop,production" ./...` | exit 0 |
| `go vet ./application/... ./gui/...` | exit 0 |
| `go test ./... -count=1` | exit 0（全部包 ok） |
| `git diff --check` / `gofmt -l` | 均无输出 |

用例改动清单（`agent-team-view.test.mjs`）：未装配态不出现形态按钮、团队库不出现 chip 行、
员工栏无拖拽与位置列、成员表无序号列、编辑器具隐藏字段（`data-team-form-policy` 不再有可见
只读框 / `-label`）、`teamMemberSpecMap` 改"条目优先、其次员工库"两条、`goalPreset` →
`teamEntryWithTL`、团队库行只说规模、顺序策略既不显示也不给入口、员工库拖拽 → 「入职」。

## 6. 门禁结果（Confirmed）

| 命令 | 结果 |
|---|---|
| `node --test gui/frontend/dist/*.test.mjs`（`_scratch/run_fe_tests.py`，输出落 `_scratch/fe_test.log`） | `ℹ tests 558` / `ℹ pass 558` / `ℹ fail 0`，exit 0 |
| `node --test gui/frontend/dist/agent-team-view.test.mjs` | `ℹ tests 42` / `ℹ pass 42` / `ℹ fail 0` |
| `node --check gui/frontend/dist/agent-team-view.js` / `app.js` | exit 0 |
| `go run ./tmp/teamiface` | 修前接口 `false` / 修后接口 `true` |
| `go test ./gui -run TestHeadlessTeamRPC -count=1 -v` | `--- PASS: TestHeadlessTeamRPC (0.02s)`；`ok .../gui 0.577s` |
| `go build ./...` | exit 0 |
| `go build -tags "gui,desktop,production" ./...` | exit 0 |
| `go vet ./application/... ./gui/...` | exit 0 |
| `go test ./... -count=1` | exit 0（全部包 ok，含 `sessionstore` 111.5s / `workspace` 85.4s / `seelebridge` 77.9s） |
| `go test ./e2e/ -run "TestRepositoryModulesHaveReadmes\|TestRepositoryModuleReadmeLinks\|TestRepositoryAgentDocumentationRules" -count=1` | PASS（文档规则与模块 README 链接） |
| `git diff --check` | exit 0（无空白错误） |
| `gofmt -l gui/headless_team.go gui/headless_team_test.go` | 无输出（无新增未格式化文件） |

提交二**不与提交一合并**：提交一是 Go 侧（`7b0db19`），提交二是本笔（GUI 面板侧 + headless 残留
+ 文档同步 + 生成物刷新）。

## 7. 未做 / 风险

- **`styles.css` 里另有一批存量零引用类名**（`.team-hire` / `.team-refresh` / `.team-toolbar` /
  `.team-cell-key` 等约 20 个）：不是本次改动造成的死类，不在本次范围（只清"本次改动造成的"）。
- **嵌入前端的 exe**：提交时仓库的 `post-commit` 钩子自动重建开发态二进制
  （`dist/dev/seelex.exe` + `dist/seelex-gui-dev/seelex-gui.exe`），所以**重启 GUI 就是新面板**。
  已核对重建后的产物（30.6 MB，`LastWriteTime` 11:52:00）里嵌的前端：旧符号
  `agentTeamOrderForDrag` / `AgentTeamPresets` **不存在**，
  `data-team-employee-hire` 与"次序 = 登记先后"**存在**。
  **但重启前看到的还是旧面板**：一个 `seelex-gui.exe`（PID 48936）在本笔提交前（10:12:55）
  就已启动，内存里仍是旧镜像——我没有去杀用户正在跑的进程，这一步留给用户。
- **`docs/design/qoder-skin/build/seelex-qoder-preview.html` 是整份重新生成**（`node
  docs/design/qoder-skin/build-preview.mjs`）：它内联**整份** `styles.css`，所以这次刷新也带上了
  `ea7037f`（上一次生成）之后其他提交的样式漂移，不只是团队面板那几条规则。这是生成物的固有
  性质（`replica.html` 里没有团队面板内容，团队面板规则全部来自 `styles.css`）。
- **M4 的 `order_policy/order_roles` 退场仍 blocked**：字段本体、落盘取值与 `order_roles` 一字未动；
  它仍被席位环与前端 `team.set_order`（员工栏「摘除」时提交整张顺序表）共同消费。
- **`@` 的补全面为空**（Hypothesis → 待用户实测）：候选只在团队库里，而 `Suggestions` 跑在 TUI 的
  `View()` 渲染路径与 GUI 每次输入事件上，不能为此读盘。若反馈"记不住团队名"，正确做法是给召唤面
  加会话上下文（`@` 空参的回执已列出库里的名字），而不是把形态目录加回来。
