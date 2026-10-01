# M4 死代码第 1 轮（2026-10-01）

> **口径**（阶段三开工简报 §W4）：**先产出引用事实，再动刀**；本轮只删能证明
> **无消费者**的干净条目 + 修正误导性陈旧注释/文档；`blocked` 条目不动；
> 一次提交；回归证据 = `go build` / `go vet` / `go test`（+ 文档门禁）。
>
> **引用事实**统一用 `git grep`（覆盖全部 tracked 文件）；"生产调用点"与"用例调用点"
> 分开看——**只有用例引用**的条目（test-only）删它等于**改规格**，本轮一律不动，留进
> 下一轮清单。所有断言给 `文件:符号`。

---

## 1. 本轮方法：为什么不能直接照 deadcode 的清单删

- 先用 `go run golang.org/x/tools/cmd/deadcode@latest -test=false ./...` 扫全仓得候选
  （报告 46 KB）。
- **报告噪声极大**，不能当删除清单用（Confirmed，逐类核对）：

| 噪声类 | 例子 | 为什么误报 |
|---|---|---|
| Wails 绑定面 | `gui/bridge.go` 的 `Bridge.*` 几十条 | 前端经 Wails 反射/绑定调用，静态不可达分析看不到 |
| 测试夹具 | `e2e/scenario/*`、`internal/testutil/*` | `-test=false` 把它们从根集合里排除，整包变成"不可达" |
| 接口方法实现 | `e2e/scenario/harness*.*`、`gui/terminal/ptyProcess.*` | 满足接口即被调用，与直接调用点无关 |
| 闭包/接口注入 | `sessionstore/*` 的 `storeEngine.*` | 经接口值或闭包调用 |

- 因此本轮**逐条**用 `git grep <符号>` 核实：**生产 + 用例都零引用**的才删；只有用例引用的
  记进 §5.2。

---

## 2. 本轮删除（逐条引用事实）

| # | 位置 | 符号 | 引用事实（`git grep`，全仓含用例） | 判定 |
|---|---|---|---|---|
| 1 | `seelebridge/security/pathgate.go` | **整文件**：`PathGate` / `LoadPathGate` / `parseRule` / `normalizePath` / `pathZoneRule` | 只有该文件自身；外部仅 README/文档以路径或符号名提及，**无任何 `.go` 调用点**。2026-09-13 调研 `docs/2026-09-13-seele-multimodal-interaction/plan.md` §S9 已记为「**无调用点（死代码）**」 | 零消费者 |
| 2 | `application/core/goal/record.go` | `ParseStatus` / `validStatuses` / `ErrInvalidStatus` | 仅本文件（`ParseStatus` 用 `validStatuses`、`ErrInvalidStatus`，后两者只服务它） | 零消费者 |
| 3 | `application/core/govern/governance.go` | `ShouldBreak` | 仅本文件 + `govern/README.md` | 零消费者 |
| 4 | `application/core/agentteam/global.go` | `NormalizeEmployeeLibrary` | 仅本文件 + `agentteam/README.md` + devlog | 零消费者 |
| 5 | `application/core/session_runtime/fork.go` | `forkContextPlanFrames` / `forkContextTaskFrames` | 仅本文件 + `session_runtime/README.md`；同族 `forkContextSkillFrames` 仍被 `forkContextRecord` 消费（Plan/Task 栈在 fork 时直接置 `nil`，故这两个过滤函数从未接线） | 零消费者 |
| 6 | `seelebridge/task/tools.go` | `validTodoStatus` | 仅本文件（`validateTaskTransition` 是另一条链，不经它） | 零消费者 |

**为什么这些是"安全删除"**：每条的 import 面都不收缩（`fmt`/`errors`/`dto`/`sessionstore`
在同文件其它函数里仍被用），删完 `go build ./...` 与 `go vet`（含测试编译）全绿——
没有一处的删除是靠"改别人才能编过"。

---

## 3. 连带文档修正（"有误导性的陈旧代码"的另一半）

`pathgate.go` 退场不只是删一个文件，还消掉一处**误导**：多个文档把它写成一层**在跑**
的安全边界（`ProjectScope + PathGate`、mermaid 里的 GATE 子图）。修正清单：

| 文件 | 修正 |
|---|---|
| `seelebridge/security/README.md` | 删 `pathgate.go` 条目；mermaid 删 `GATE` 子图与相关边（`SCOPE → GATE → CMD` 直连 `SCOPE → CMD`）；"两层边界"段改写为「ProjectScope 物理边界 + CommandSandbox 执行边界；策略审批由 `seele.yaml` 权限 gate 判定」，并注明 PathGate 已退场 |
| `README.md`（根） | 能力表「项目安全」行、架构 mermaid 的 `ProjectScope + PathGate`、分层字符画的 `Account · MCP · PathGate`、§5「Workspace Sandbox 与 Permission Policy」、目录表 `seelebridge/` 职责 |
| `AGENTS.md` | Review 清单里的 `ProjectScope/PathGate` 引用 |
| `seelebridge/README.md` | 子包表 `security/` 行 + 「ProjectScope 与 PathGate」小节 |
| `plugins/default/README.md` | default 不得绕过的清单（去 PathGate） |
| `seelebridge/security/command_class.go` | 注释里的 `pathgate.go` 改指 permission gate |
| `application/core/{agentteam,goal,govern,session_runtime}/README.md` | 索引段由 `scripts/gen_core_readme_index.py` 从源码 doc 注释重生成（刷新即去掉已删函数条目） |

---

## 4. 回归证据

```text
go build ./...                                        → exit 0
gofmt -l <本次改动的 6 个 .go>                         → 空
go vet ./application/... ./seelebridge/...            → exit 0（含测试编译）
go test ./application/core/{agentteam,goal,govern,session_runtime}/... \
        ./seelebridge/{security,task}/... -count=1    → 全绿（1.0–3.1s/包）
python scripts/gen_core_readme_index.py               → 刷新 4 个 README
python scripts/check_mermaid.py                        → 201 代码块 / 0 问题
python scripts/check_readme_refs.py --strict           → 除 5 处既有历史漂移外无新增
                                                         （本文件创建后本文引用解析）
```

> `check_readme_refs --strict` 报的 5 处未解析引用（`README-session.md`、
> `gui/frontend/README.md`、`internal/bootseed/README.md`、`seelebridge/plan/README.md`、
> `sessionstore/README.md`）**全部与本次改动无关**（引用的是更早退场的文件），不在本
> 轮范围内。

---

## 5. 未删：下一轮清单

### 5.1 `blocked`（等新面接管，按 §9 纪律不动）

`newGovernor` / `seatPlan.seats` / `roleTurnSeat`、`NewAdvisorSeat`、`teamRoleSeatsFor`、
`RoleTurnRunner` 适配层、`lifecycle.order_policy/order_roles`——引用事实与退场条件见
[`2026-10-01-m4-deadcode-inventory.md`](2026-10-01-m4-deadcode-inventory.md)。本轮**未动**。

### 5.2 `test-only`（删 = **改规格**，须连带改用例）

| 位置 | 符号 | 只有谁在调 |
|---|---|---|
| `application/core/goal/headless.go` | `NewServer`/`NewClient`/`Server`/`Client` 整面 | `headless_*_test.go` / `dsa2a_test.go`（**无生产调用点**——整面疑似陈旧，值得下一轮定夺） |
| `application/core/goal/store.go` | `MemoryStore`/`JSONFileStore` | `controller_test.go`（生产 Store 是 `sessionstore_store.go`） |
| `seelebridge/events_unified.go` | `unifiedEventTopic` | `events_range_test.go` |
| `seelebridge/runtime_plan.go` | `branchTraceID` / `roleForPlanBranch` / `resolvePlanBranchAccount` | `runtime_account_test.go` |
| `seelebridge/tools/async_exec.go` | `renderAccepted` / `renderPolled` | `async_exec_test.go` |
| `internal/bootseed/bootseed.go` | `EncodeBase64` | `bootseed_test.go` |
| `seelebridge/worktree/worktree_manager.go` | `ConflictFilesIn` | `worktree_test.go` |
| `seelebridge/internal/telemetry/summary.go` | `WithNow` | `summary_test.go` |

### 5.3 待核实（疑似真死，但需先证）

- `seelebridge/tools/async_run.go` 的 `dispatchAsync`：**生产与用例都零调用点**，注释自称
  「留给直接调用契约的路径与用例」——疑似随 `bash(background=true)` 面退场遗留。同文件
  `startAsync`/`awaitAsync`/`startInlineJob` 仍是活路径，故**不能整文件删**；下一轮单独核实
  `dispatchAsync` 是否确无调用者后删除。

### 5.4 deadcode 报告的"不可直接采信"面（留档）

`gui/*`（Wails 绑定）、`e2e/scenario/*`（夹具）、接口方法实现、`sessionstore` 的
`storeEngine.*`——见 §1 噪声表。下一轮若要动 gui/e2e，须另找手段（绑定清单 / 夹具清单），
**不得**拿 deadcode 输出当依据。

---

## 6. 纪律

- 未动 `_tmp` / `_scratch` / `vendor` / `dist`；未动 `go.mod` 的 `replace`；未改任何用例文件。
- 一次提交（`refactor(...)`），提交信息写清「删了什么、为什么现在能删、回归证据」。
- 下一轮删除前先跑 `docs/test/2026-10-01-computer-use-smoke-checklist.md`（删一波 → 提交一次
  → 冒烟 → 没问题再删下一波）。
