# goal 残留整肃 + 工作台目标看板（阶段三 W3 收尾）

- 日期：2026-10-03（接 [`2026-10-03-seat-rotation-retired.md`](2026-10-03-seat-rotation-retired.md)）
- 范围：`application/{contract/core}`（注释与只读投影）、`seelebridge/*`（注释）、
  `gui/{README.md,headless_goal_test.go,frontend/dist/*}`、`gui/goal_board_wiring_test.go`（新增）、
  `tui/goalteam.go`（已在上一条退场）、根 `README.md`、`docs/arch/*`、`CHANGELOG.md`
- 上游权威：`docs/devlog/2026-10-01-goal-board-vmodel-brief.md`（W3）、
  `docs/devlog/2026-10-03-seat-rotation-retired.md`（退场边界）

## 1. 这次做两件事

1. **把「已退场的机制」当现存机制写的注释与文档清干净**（用户口径：删除一切的旧 goal
   席位轮换残余物，包括前端）。
2. **把 goal 看板做进工作台「目标」子页**（用户口径：上面大的 active seq、下面小字的
   "我发出的最近一次任务"、目标结束就没有了、点开出一份内容详情）。

## 2. 残留物清单与判据（为什么它们算残留物）

W3 删的是**代码**；本轮的判据是**读起来会骗人的陈述**。三类都清了：

| 类 | 例子（原文） | 为什么算残留 |
|---|---|---|
| 现存机制陈述 | `Order：座位存在性（newGovernor 据此决定 main/tl 座位要不要长出来）`、`真正驱动轮次的是 goal 治理的座位循环`、`order_roles 仍是座位存在性` | `newGovernor` / 座位派生已删；这两句会让下一个读者去找一个不存在的调用链 |
| 已删入口当可达 | `gov_break 可达`、`goal.gov_break 走同一个显式停止入口` | `goal_gov_break` 已拒（与未知方法同路），headless 用例自己也已断言被拒 |
| 座位词汇留在产品面 | 装配回执 `已召唤团队 %s：%d 个席位在编`、生态位 chip 的 title `决定装配后的座位（techlead = ADVISOR 评审座位）`、前端常量 `TEAM_SEAT_FIELDS` | "席位"是旧口径的命名；东西还在，名字不该继续指认一个退场的概念 |

**保留**的是显式退场注记（`X 已随席位轮转退场删除`）。它们不是残余物：删掉它们，
下一个人就会把 `SeatExecutor` 的缺席读成"还没写"。

前端死样式一并删除：`.goal-gov-broken` / `.goal-gov-error`（断环横幅与"本轮治理未完成"
横幅的样式，渲染点在 W3 已删）。`styles.css` 里那段注释同步改成事实。

## 3. 看板的设计口径（唯一口径）

- **active seq = 当前目标在本会话 goal 序列里的序号**（`g-<n>` 的 `n`，由 Controller 的
  会话级自增计数器分配）。它**不是**打点条数、**不是**对用户消息的编号、**不是**它在栈里
  的位置（id 解析不出来才回退栈位置）。前端 `goalActiveSeq` 是唯一落点。
- **上大下小**：上面是 active seq（大号数字 + `active seq` 标签 + 目标标题 + 状态/打点/更新
  一行 meta），下面是「我发出的最近一次任务」（最近一条非空用户输入，小字一行，超长截断并
  把全文留在 `title`）。
- **结束就没有了**：栈上没有 active 帧（目标收口 / 没有 goal）→ 看板返回空串，不留空壳。
- **点开 = 内容详情**：资源管理器「内容详情」口径的**属性表**——序号（+ id）/ 状态 / 标题 /
  目标正文 / 完成条件 / 非目标范围 / **完整打点流水**（时间 + 类型 + 内容逐条）/ 创建 / 更新；
  嵌套压栈（栈深 > 1）时前面多一块活动栈总览表（一行一帧），单帧不渲染那张只有一行的表。
- **只读**：前端没有任何写 goal 状态的入口（写回会让后端真值变成前端派生物）。

后端只读投影补齐了详情面缺的字段：`dto.GoalFrameView` 新增 `OutOfScope` / `CreatedAt` /
`ProgressAll`。`Progress` 保持"最近 3 条"（卡片摘要用，`goal_stack_view_test.go` 的既有断言
不动），`ProgressAll` 给详情面完整流水——上界是 goal 域自己的环形保留上限
（`goal.MaxProgressItems = 32`），所以不需要再截一刀。

## 4. 回归证据

- `go build ./...`：通过；`go vet ./application/... ./seelebridge/... ./gui/... ./tui/...`：通过；
- `go test ./application/core/... ./seelebridge/... ./tui/... ./gui/ -count=1`：全绿（含新增
  `TestGoalStackFramesCarriesDetailFields`、`TestEmbeddedGoalBoardWiring`）；
- `node --test "gui/frontend/dist/*.test.mjs"`：568 项全绿（含新增 `goal-board-view.test.mjs` 10 项）；
- `gofmt -l`（改动的文件）：干净。

## 5. 未决 / 边界

- **团队环的「发言调度」面板保留**：那是**环**（`TurnScheduler` + 逃生记账），不是 goal 的
  座位循环；goal 退场后它仍然如实投影环的运行态（`round_limit` / `no_progress` / `stopped`）。
  要连它一起收掉，属于"team plan 成为唯一顺序事实"那一步，不在本轮。
- **`order_policy` / `order_roles` 仍是历史字段**（写入面在、读面只展示），退场被
  `docs/devlog/2026-10-01-m4-deadcode-inventory.md` #5 标为 blocked：本轮只把描述改成事实，
  不改落盘取值（旧会话里的 `goal_loop` 必须继续可读）。
- **历史文档按历史阅读**：`docs/arch/agent-team-seat-vs-claim.md` 与各 `docs/2026-*` 目录
  保留原样（dated 记录 = 纸面轨迹），只在会误导"今天的实现"的那一篇加了退场横幅。
