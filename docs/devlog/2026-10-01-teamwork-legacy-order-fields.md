# 团队顺序字段（`order_policy`）退化为历史字段：撤掉「循环」说法 + 面板只读化（2026-10-01）

> **口径**：本文记的是「teamwork 的说法与顺序字段」这件事的**修前事实**（可核对）、
> **改法**与**回归证据**，并明确标出**没做什么**（阻塞项）。
> 触发是用户口径：**「一些说什么循环的我估摸着这种说法对于 teamwork 来讲过时了，
> 你继续修改一些废弃的设定」**。
> 结论分 **Confirmed**（有代码/文档/用例证据）与 **Hypothesis**（待验证，写明验证方式）。

---

## 1. 事实（Confirmed）

### 1.1 「循环」是旧环序模型的说法，不是 teamwork 的模型

`docs/arch/teamwork-leader-worker-architecture.md` 自己的决策表就把这套字段判为旧面：

- **D4**（§开头决策表）：**「废弃旧顺序字段；顺序由 mainagent 掌控」**；
- **§4.6**：`stages.depends_on` 是**顺序/依赖的唯一事实**（取代 `order_policy/order_roles`）；
  「旧字段退场：`lifecycle.order_policy/order_roles` 标注历史/只读，读面切换到计划」；
- **M4**（§8）：目标之一就是 `order_policy/order_roles` 退场。

也就是说：把 teamwork 描述成「固定循环 user → main ↔ TL」既**不承诺班底**（成员表
早就能加工人），也**用错了模型**（leader-worker 里顺序由 leader 编排，不是环转）。

### 1.2 `order_policy` 现在**只被回读展示**（这是本轮动手的依据）

逐条引用事实（生产调用点）：

| 面 | 位置 | 事实 |
|---|---|---|
| 校验 | `application/core/agentteam/{spec,library,global}.go` | 只做枚举校验（`goal_loop` / `user_main_decided` / `scheduled_only`），没有按取值分支 |
| 写入 | `agentteam/factory.go`（`SetLifecycleOrder`）→ `sessionstore/lifecycle.go:311` | 写进 lifecycle head 的 `order_policy` 字段 |
| 运行时 | `agentteam/runtime.go:94/109`、`runtime.go:335` | `Runtime` 只把策略**存下来**（`r.policy`）并在 `Snapshot()` 里投影；`scheduler.go` 完全不读取值 |
| 展示 | `agentteam/README.md` 的「工作顺序」一行 | 「用于角色 draft 同步排序与成员表展示；**不驱动运行时轮次**」 |
| GUI | `gui/frontend/dist/agent-team-view.js`（旧 `metaRow`） | 下拉改值 → `AgentTeamSetOrder("", policy, orderRoles)`：**顺序一字不变**，只改一个回读展示的字符串 |

对照：真正影响行为的是**同一字段组的 `order_roles`**——`goal_coordinator.go` 的
`newGovernor`/`seatPlan` 按它决定座位长不长出来（`teamRoleSeatsFor` ← `Runtime.Order()`），
前端拖拽调序也写它。

结论：**`order_policy` 是一个"改了不驱动任何行为"的旋钮**，而面板把它摆成"谁决定发言顺序"
的设定——这正是用户说的「过时的说法」。

### 1.3 退场仍然 blocked（所以本轮只标注）

`docs/devlog/2026-10-01-m4-deadcode-inventory.md` #5：`order_policy/order_roles` 既被席位环
消费，也是前端拖拽调序的唯一事实；只读化 = 退掉写入面 + 读面切到计划，属**产品面决定**，
必须在 team plan 成为读面唯一来源之后。故本轮**不删字段、不改落盘取值**（旧会话里的
`"goal_loop"` 必须继续可读）。

---

## 2. 改法（本轮）

### 2.1 文案：不再用「循环」说 teamwork

| 位置 | 修前 | 修后 |
|---|---|---|
| `POLICY_LABEL.goal_loop` | `固定循环` | **`固定座次`** |
| `STOP_REASON_LABEL.no_executor` / `.empty_ring` | `环内无执行者` / `空环` | **`顺序里没有执行者` / `发言顺序为空`** |
| 发言调度串珠条（tips / 注释） | 「发言**环**」「环内没有执行者」 | 「发言顺序（旧环序的运行态投影）」「顺序里没有执行者」 |
| `agentteam/presets.go`（`goalA2APreset` 首行 → README 索引） | 「goal 的 user→main↔tl **固定循环**」 | 「goal 的**固定座次**（user → main → tl，其中 tl 是 ADVISOR 评审座）」 |
| `application/core/agentteam/README.md` | 「`goal-a2a`（**TL 循环**）」 | 「`goal-a2a`（固定座次 user → main → tl）」 |

新增纯函数 `orderPolicyLabel(policy)`：未知取值**原样显示**、空值明说「未登记」，
不把「没见过的取值」折成已知策略。

### 2.2 顺序策略从「可编辑设定」降为「只读历史字段」

- **团队编辑器**（`teamEditorPanel` 字段 4）：下拉 → **虚线只读框 + 隐藏字段**
  （`data-team-form-policy-label` / `data-team-form-policy`）。取值仍被表单读回
  （`agentTeamEntryFromForm`），保存时**原样送回后端**，不会丢事实。
- **Team 栏**（`metaRow`）：下拉 → **只读 chip**（`class="chip team-policy-static"`，
  仍带 `data-team-policy="<取值>"`）。`app.js` 删掉它的 `change` 分支（那是一个
  "改了不驱动任何行为"的提交口），`agentTeamCurrentPolicy()` 改从 chip 读取值——
  拖拽调序仍走 `AgentTeamSetOrder`，取值原样带上。
- **形态起手**：新增 `writeTeamFormPolicy(slot, policy)`（`app.js`）——「用内置形态起手」
  时同时刷隐藏字段与只读标签；旧代码只写 `.value`，标签会留在旧取值上。
- **样式**：`.team-policy-select` → `.team-policy-static`（虚线边 = 不是输入控件），
  新增 `.team-field-static`。

### 2.3 后端只标注，不动接口

- `application/contract/dto/agentteam.go`：三个策略常量加 GoDoc `Deprecated:`（写明替代
  事实 = team plan 的 `stages[].depends_on` 与"不要按它分支"）；`TeamSpec` /
  `TeamSchedule` 的类型注释写明这两个字段是**历史/只读**，退场前不得删。
  **不把 `Deprecated:` 打在 struct 字段上**：写旧字段仍是装配/保存的合法动作，打了只会
  让"必须保留的写入面"看起来像错误用法。
- 落盘取值、wire 契约、`NormalizeLibraryEntry` 的枚举校验**一字未改**。

---

## 3. 回归证据（Confirmed）

| 命令 | 结果 |
|---|---|
| `node --test gui/frontend/dist/*.test.mjs` | **560 pass / 0 fail**（`agent-team-view` 新增的 3 条全绿；本文件 41 → 44 条） |
| `node --check gui/frontend/dist/{agent-team-view,app}.js` | 通过 |
| `gofmt -l <本仓库目录>` | 本次改的两个 Go 文件**不在**输出里（输出里的 5 个文件是本仓库既有的未格式化文件，与本次无关） |
| `go vet ./application/... ./gui/...` | exit 0 |
| `go build ./...` / `go build -tags "gui,desktop,production" ./...` | exit 0 / exit 0 |
| `go test ./... -count=1 -timeout=300s` | 第 1 次：仅 `seelebridge` 的 `TestForkSubagentsBestEffortKeepsSiblingAlive` 失败（`fork_besteffort_test.go:57`，上游流 `context deadline exceeded`）；`-run` 精确重跑 **PASS**（0.33s），第 2 次全量 **exit 0（69 个包 ok）**。本次 Go 改动只有注释，与该用例无因果，判为并发满载下的偶发超时。 |

新增/改写的用例（`gui/frontend/dist/agent-team-view.test.mjs`）：

1. **顺序策略是只读历史字段**：面板给 chip 不给下拉（`assert.doesNotMatch(/<select[^>]*data-team-policy/)`），
   chip 仍带取值 `data-team-policy="goal_loop"`，标签是「固定座次」；
2. `orderPolicyLabel` 只翻已知取值：未知原样、空值「未登记」；
3. 发言调度的收束文案不再说「环」（`顺序里没有执行者`）；
4. 团队库行用例加断言：不再出现「固定循环」，title 里点明「历史字段：顺序由 leader 掌控
   （team plan 的 stages[].depends_on）」；
5. 团队编辑器用例加断言：字段 4 是只读标签 + 隐藏字段（无下拉）。

---

## 4. 没做 / 待定

- **字段本体与落盘取值未动**（`lifecycle.order_policy/order_roles` 仍在写、仍在读）：
  退场条件见 M4 清单 #5（**blocked**，要先让 team plan 成为读面唯一事实）。要做到
  「只读化」还需后端不再接受 policy 写入（`AgentTeamSetOrder` 的 policy 参数）——
  本轮只撤掉面板上的编辑入口，接口保持兼容。
- **`teamGlobalDrift` 仍只比名单**（母本改权限不算漂移）：属产品口径，未改。
- **README「文件与函数索引」在别处有旧漂移**：跑 `python scripts/gen_core_readme_index.py`
  时同时刷新出 `application/core/{README-goal,README-input,README-session,goal/README}.md`
  的存量差异（与本次改动无关）。为不让一个 commit 混两个主题，本轮只保留
  `application/core/agentteam/README.md` 的刷新，其余四份**已还原**，留待单独一次
  「README 索引刷新」提交。
- **`order_roles` 的座位语义仍是环的语义**（`newGovernor` 按它长座位、`Runtime` 把它投影成
  「发言顺序」）：这是 M4 §9「team 环与逃生」那条退场项的边界，本轮不动。
