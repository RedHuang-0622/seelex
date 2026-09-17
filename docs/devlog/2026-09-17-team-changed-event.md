# 2026-09-17 启动团队后面板要动：会话团队事实改用 `team.changed` 通告

## 症状

`@goal-a2a` 召唤成功（回执"已召唤团队 goal-a2a：3 个席位在编"），但 status 子页的
Agent Team 区块还停在召唤前的样子（"当前会话未装配 AgentTeam"），要等切会话或做一次
面板动作才刷新。goal 上线自动装配 goal-a2a 时同样：面板永远不知道自己已经有一支团队。

## 根因

面板数据（成员表/工作顺序/发言调度）**不在**会话快照里：GUI 的 status 子页与 TUI 的
Alt+T 都按 RPC `AgentTeamView` 拉取，并按会话键缓存上一次结果。

- GUI `app.js`：`refreshAgentTeam({force=false})` 在"会话没变 + 有缓存"时直接早退，
  `scheduleAgentTeamRefresh`（每次 render 都调用）也带同一条早退。于是"事实变了"没有任何
  下行通道，只能靠**发起这次装配的那个前端动作**主动重取——而那是一个 composer 文本嗅探：
  `if (text.startsWith("@") && team-section.open) refreshAgentTeam({force:true})`。三个漏洞：
  面板收起时召唤（`team-section.open` 为假）、goal 自动装配（模型调 goal_begin 拉起团队，
  没有任何 composer 提交）、以及任何非本 composer 的来路。
- TUI `goalteam.go`：面板读面只在打开时取一次，用户得再按一次 Alt+T 才发现团队变了。

## 变更（后端）

- `application/event/hub.go`：新增会话级 `EventTeamChanged EventKind = "team.changed"`。
- `application/core/agentteam_service.go`：新增 `publishTeamChanged(mainSessionID)`，
  **revision=0**（沿用 `chat.changed` 的口径：载荷不在快照里，带 revision 会被协议层的
  "已由权威快照表示"陈旧判据丢掉，而面板缓存并不随快照翻转——召唤后调用方紧接一次快照
  刷新是常态，那条事件会被吞）。
- 发布点钉在**写路径**，不钉在调用方：
  - `MaterializeAgentTeam`（`@` 召唤 / goal 自动装配 / 面板 RPC「一键装配」的共同收口）；
  - `AgentTeamSetOrder`、`AgentTeamInstantiateRole`、`AgentTeamPutRole`、`AgentTeamDeleteRole`。
- `input_team.go` 里原先那次 `publishTeamSummonChanged`（发 `snapshot.changed`）删除：
  装配收口已经通告，召唤不再是特例。
- **读路径不发**（`AgentTeamView`）：读成员表时发通告会退化成"取成员表 → 通告 → 再取"的
  自激循环。这一条有专门用例钉住。
- 边界：团队库/员工库/默认顺序是**全局母本**，不在本事件范围内（母本 CRUD 的调用方按
  返回值刷新面板）。

## 变更（前端）

- GUI `protocol.js`：`team.changed` 进 `INCREMENTAL_KINDS`，reducer 里返回 true（不改快照，
  只把 kind 透给 `onIncremental`）。
- GUI `app.js`：新增 `agentTeamDirty` 脏位与 `invalidateAgentTeam()`；`refreshAgentTeam`
  与 `scheduleAgentTeamRefresh` 的早退条件加上"不脏"；取数完（无论成败）清脏位；
  `renderIncremental` 收到 `team.changed` 就作废缓存，面板可见时立即重取、否则等下次
  render/展开。composer 的文本嗅探删除。
- TUI `tui.go`：`applicationEventMsg` 分支里，团队面板打开时收到 `team.changed` 就地重取
  （`tea.Batch` 重取 + 继续等事件，保持"始终只有一个等待在飞"）。

## 验证

```text
gofmt -l application tui gui                       → 空
go vet ./application/... ./tui/...                 → 空
go build ./... / -tags "gui,desktop,production"    → ok

go test ./application/event/ -count=1
  TestKindWhitelistClassification                  → PASS（team.changed 会话类，必带 sid）
go test ./application/core/ -run 'TeamChanged|SubmitTeam|ReadPathDoesNot' -count=1 -v
  TestSubmitTeamPublishesTeamChanged               → PASS（`@goal-a2a` 后收到 team.changed，
                                                     sid=sess-summon、revision=0）
  TestMaterializeAgentTeamPublishesTeamChanged     → PASS（goal 自动装配走的同一收口也发）
  TestReadPathDoesNotPublishTeamChanged            → PASS（读成员表不发通告）
go test ./application/console/ -run EventLogger -count=1
  TestBackendEventLoggerLogsTeamChangedStage       → PASS（一次性探针能看见"装上了"）

go test ./tui/ -run TeamPanel -count=1 -v
  TestTeamPanelRefetchesOnTeamChanged              → PASS（面板打开→重取；收起→不发请求）

node --test gui/frontend/dist/protocol.test.mjs
  dispatches team.changed without a snapshot refresh, even above the revision floor → PASS
node --test gui/frontend/dist/agent-team-refresh.test.mjs → PASS（app.js 认 team.changed、
  用脏位作废缓存、不再按 composer 文本猜）
go test ./application/... ./tui/... ./gui/... -count=1 → 全 ok
node --test gui/frontend/dist/*.test.mjs           → 342 pass / 0 fail
```

真机端到端（`-frontend backend` 一次性探针）：

```text
go run . -frontend backend -backend-prompt "@goal-a2a" \
  -store tmp/e2e-teamwork/store-evt2-<stamp> -backend-log tmp/e2e-teamwork/log-evt2-<stamp>.log
[backend] +0s    stage=submit input_chars=9
[backend] +29ms  stage=chat.idle
[backend] +29ms  request= stage=team.changed          ← 新增：召唤成功在日志里可见
exit=0
落盘（注意会话树在 filepath.Dir(storePath)/sessions-json/ 下）：
  .../session-<id>/team/roles.json         → team_id=goal-a2a, order_policy=goal_loop
  .../session-<id>/metadata/lifecycle.json → order_roles=["user","main","tl"]
  .../session-<id>/goal_a384d51d11c6689e/… → tl 角色会话
没有 message.json：@goal-a2a 只装配、不产生输入（与上一版一致）。
```

顺带修的一处观测缺口：`application/console` 的事件日志此前只打 tool/interaction/error/
snapshot.changed/subagent.tool 这几类，召唤的"装上了"原本借 `snapshot.changed` 才能看见；
这次 `team.changed` 补进 `LogEvent` 的 switch，否则一次性探针里"召唤到底成没成"会消失。

## 未做

- 跨进程同步：`team.changed` 只对**同进程**订阅者可见（GUI/TUI 各自是进程）。另一个进程
  里的面板不会因此自动更新——那是"多客户端实时同步"的另一个题目（需要共享事件总线）。
- 母本（团队库/员工库/默认顺序）没有事件：面板里的"库"分区仍由调用方按 RPC 返回值刷新。
