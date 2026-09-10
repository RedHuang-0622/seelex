# A2A AgentTeam 与 subagent 断点恢复真实 API 冒烟报告（2026-09-10）

> 日期：2026-09-10 · commit：`589c6e1`（工作区干净）
> 平台：Windows amd64 · `go1.25.8 windows/amd64` · `CGO_ENABLED=1`（`-race` 可用）
> 被测：`application/core/agentteam`、`application/core/resume`、`seelebridge/session`（subagent）、
> headless `team.*` / `role.*` / `subagent.*`
> 账号：本地 `config/accounts.yaml`（deepseek 兼容端点，凭据不外泄）

本报告回答三件事：A2A AgentTeam 的**装配与运维过程**真跑通了没有、subagent 在
**进程被硬杀之后的断点恢复**真跑通了没有、以及这两条链路在真实 API 下**有没有
死锁 / 数据竞争 / 性能热点**。

## 1. 命令与现场

```powershell
# 目标二进制（当前 HEAD 重建，避免用旧产物糊结论）
go build        -tags pprof -o tmp/headless-smoke/seelex-pprof-team.exe .
go build -race  -tags pprof -o tmp/headless-smoke/seelex-pprof-subagent-race.exe .

# A2A AgentTeam：真实 API + pprof
$env:SMOKE_TEAM_LIVE='1'; $env:SMOKE_TEAM_LIVE_PPROF='1'
go test ./gui -run TestRealAPIAgentTeamLiveProbe -v -count=1 -timeout 20m
# → PASS 1.72s

# subagent 中断恢复：真实 API + pprof + race 目标
$env:SMOKE_SUBAGENT_LIVE='1'; $env:SMOKE_SUBAGENT_LIVE_PPROF='1'; $env:GORACE='halt_on_error=1'
$env:SMOKE_SUBAGENT_LIVE_TARGET='tmp\headless-smoke\seelex-pprof-subagent-race.exe'
go test ./gui -run TestRealAPISubagentResumeLiveProbe -v -count=1 -timeout 25m
# → PASS 52.14s
```

说明两点，避免结论被误读：

- 真实 API 需要外网，沙箱内被拦截（`Invoke-WebRequest` 直接 `无法连接到远程服务器`），
  两条探针均在**非沙箱**下执行；未执行的项目不在本报告里写成通过。
- 第二条探针的被测 headless **进程**用 `-race` 构建并设 `GORACE=halt_on_error=1`；
  `go test` 自身未加 `-race`。数据竞争结论只覆盖被测进程。

## 2. A2A（AgentTeam）运行过程

一条真实链路从头走到尾：真实 API 物化主会话 → 同一工厂装配两个团队 → 角色 CRUD 与
工作顺序设置 → 设计稿不变量断言 → 抓 pprof。

| # | 步骤（headless RPC） | 断言 | 实测 |
|---|---|---|---|
| 1 | 启动 + `/healthz` | 进程健康 | 200 |
| 2 | `Submit` + `WaitIdle`（真实 API） | 主会话物化 | `draft_1789049254608175600_1` |
| 3 | `team.presets` | goal 只是内置实例之一 | `goal-a2a` / `review-team` / `research-team` |
| 4 | `team.materialize(goal-a2a)` | TL 上线 + 顺序策略落 lifecycle head | 角色会话 `tl`；`order_roles=user,main,tl`；`order_policy=goal_loop`；`design_notice=[]` |
| 5 | 主文档真实轮次核对 | 不能“没跑模型也算过” | `user_rows=1, assistant_rows=1` |
| 6 | role draft sync（`tl`） | 顺序装配后 floor 随 message head 发布 | `floor.RoleName=tl`，`floor.RoleSessionID` 非空 |
| 7 | 重复 `team.materialize(goal-a2a)` | 幂等（AT6） | 第二次 `created=false`，无第二个 tl 会话 |
| 8 | `team.materialize(review-team)` | 同一工厂 + 同一 RPC 面，只换 TeamSpec（AT8） | 角色会话 `reviewer`；`order_policy=user_main_decided` |
| 9 | `team.put_role(auditor)` + `team.set_order` | 前端拖拽只提交 `order_roles` | `order_roles=user,main,reviewer,auditor` |
| 10 | `team.put_role(digest,timer,scheduled)` + 把它塞进 `order_roles` | 定时 agent 不得进工作顺序（设计 §7.1） | 调用被**拒绝**；`team.view.scheduled=[digest]` 且 `in_order=false` |
| 11 | `team.delete_role(auditor)` | 从注册表与工作顺序一起摘除 | `order_roles` 中不再有 `auditor` |
| 12 | `team.view` + stderr + `/healthz` | subagent 不入成员表、无竞争、进程仍活 | 无 subagent 角色行；无 `DATA RACE`；200 |

落盘证据：`tmp/headless-smoke/reports/team-live-20260910-220736.json`
（`race_clean=true`、`real_turn={user_rows:1,assistant_rows:1}`、`design_notice=[]`）。

一句话结论：`goal` 的 TL 不再是特例——同一 `Factory` + 同一 `team.*` 面装配出了
`review-team`，角色管理（增/删/排序/定时分区）与顺序策略都按设计稿落地；
subagent 在这条链路里**完全不出现**在成员表里。

## 3. subagent 运行过程与断点恢复

subagent 是 tool calling 能力（劳务派遣），不是 AgentTeam 成员，所以恢复语义是
“工具调用跑到一半被终止 → 续跑”，不是“团队成员重开会话”。

### 3.1 时间线

```
阶段 1（~10s）
  healthz 200 → Submit/WaitIdle 暖场（真实 API）
  → subagent.fork(smoke-sub-1)  ← 与模型 fork_subagents 同一条执行链
  → 轮询 subagent.list 抓到运行态残留：
       node=smoke-sub-1  status=running  active=true  session=node-3e37c998
       conclusion_found=false  resumable=true
  → 直接 kill 进程（模拟“结果写盘前终止”）
  → 清理硬杀遗留的 lock.owner（诊断用）

阶段 2（~41.7s，全程 -race）
  同一数据根冷启动 → healthz 200 → ResumeSession
  → subagent.list 仍能看到可续跑残留（active 只在表格，不写进历史）
  → subagent.recover 走六步模板：
       decide → repair_parent → restore_scene → inject_note → reexecute → converge
  → 恢复说明 provider role = system；unit.NoteRole = system
  → 41.7s 后收敛：converged=true，resumed=[smoke-sub-1]，failed=[]
  → healthz 200；stderr 无 DATA RACE
```

### 3.2 断言与证据

| 断言 | 期望 | 实测 |
|---|---|---|
| 冷启动可见中断残留 | 表格里有未终结记录 | 1 条，`resumable=true` |
| 恢复模板编排 | 六步 | `decide, repair_parent, restore_scene, inject_note, reexecute, converge` |
| 恢复说明 provider role | `system` | `recovery_note_role=system`，且单元级 `note_role=system` |
| 续跑是否真的重启了节点 | 重启并注入上下文后重跑 | `resumed=[smoke-sub-1]`，`skipped=false` |
| 收敛性 | 无残留 running/active | `converged=true`，主会话回到收敛态 |
| 真实 API 是否真的调用过 | 有 provider 请求 | `requests.jsonl` 2 行（role + 内容指纹，不含正文） |

落盘证据：`tmp/headless-smoke/reports/subagent-live-20260910-220934.json`
（`phase2_report.started_at=22:08:52.547` → `finished_at=22:09:34.199`，约 41.7s）。

注意一个**探针与生产的差异**：探针通过 `SEELEX_SUBAGENT_HOLD_MS`（仅冒烟设置，默认 0）
把节点注册态多留一会儿，保证外部驱动能稳定观测到 active 再动手；生产路径不引入这段等待。

## 4. 性能热点（pprof）

采样来自**阶段 2 那个冷恢复进程**（`pprof_addr=127.0.0.1:57656`），即恢复 + 重跑的真实现场。

### 4.1 死锁与数据竞争：干净

- 冷恢复全程 `/healthz` 200，恢复后仍 200 → 没有卡死。
- goroutine dump 共 21 个：`select` 13、`IO wait` 5、`chan receive` 1、`syscall` 1、
  `running` 1 —— 没有 goroutine 停在 `semacquire`（互斥等待）上，也没有互相等待的环。
- `-race` 目标 + `GORACE=halt_on_error=1`：stderr 无 `DATA RACE`。
- A2A 冒烟同结论（`race_clean=true`）。

### 4.2 mutex profile：唯一的真实串行点

冷恢复进程（`role-mutex-1789049374251234100.txt`）：

| 争用时间 | 栈（Unlock 侧） | 解读 |
|---|---|---|
| 42.29s | `Seele/session.(*Session).ChatStream.deferwrap1` ← `seelebridge/node.(*AgentNode).Run` ← `Seele/workplan/runtime/executor.(*Executor).runNode` ← `Scheduler.run.func2` ← `ForkCoordinator.RunWithContextManager` | 子代理整段流式生成期间，会话互斥被持有 ~42s，有别的 goroutine 一直在等 |
| 23.8ms | `log.Printf`（`main.startPprofHook.func1` → `time.(*Location).get`） | 启动期日志，可忽略 |

对照 A2A 冒烟（`role-mutex-1789049256184789200.txt`）：**总量仅 478ms**，最大条目就是
上面那条 44.8ms 的启动期日志。也就是说，团队装配 / 角色 CRUD / 顺序设置链路本身
没有锁热点，热点只出现在“子代理长流式”这条路上。

### 4.3 block profile：两条 ~28s 的锁等待，其余都是空闲等待

冷恢复进程（`role-block-1789049374301418100.txt`）：

| 阻塞时间 | 阻塞点 | 性质 |
|---|---|---|
| 28.19s | `sync.(*Mutex).Lock` ← `Seele/session.(*Session).History` ← `seelebridge/session.(*SubagentSessions).buildRecordLocked` ← `persistLocked` ← `handle`（actor） | **需要关注**：落账路径读 History 被同一把会话锁挡住 |
| 28.19s | `sync.(*Mutex).Lock` ← `Seele/session.(*Session).History` ← `seelexctx.ExportSnapshot` ← `seelebridge/session.(*SubagentTree).projectNode` ← `Projection` | **需要关注**：表格投影同样被挡住 |
| 24ms | `sync.(*Mutex).Lock` ← `log.Printf`（配置加载期） | 忽略 |
| 3.2ms | `sync.(*Mutex).Lock` ← `sessionstore.(*storeEngine).readRows` ← `DerivedRecordWorkspace` | 忽略 |
| 1.4ms | `sync.(*RWMutex).RLock` ← `application/core.(*Service).consumeTaskChanges` | 忽略 |

其余 block 条目按“帧累计值 ÷ 52.1s 墙钟”折算，全部是**整场空闲等待**，不是自旋或堆积：

| 累计时间 | 符号 | 折算 |
|---|---|---|
| 1089.7s | `runtime.selectgo` | ≈21 个 goroutine 全场 select（actor/ticker/consumer） |
| 312.3s | `seelebridge/internal/actor.New[...].func1` | ≈6 个 actor 事件循环 |
| 310.4s | `Seele/tools.HandlerFunc.Execute` | ≈6 个工具执行位空闲等待 |
| 206.9s / 205.4s | `Scheduler.run` / `Executor.runNode` | ≈4 个 fork 执行位全程等待 |
| 各 104.1s | `WorkTablePublisher.loop`、`Service.consumeTaskChanges`、`consumeSubagentLifecycle`、`consumePlanNodeEvents`、`Runtime.startLiveDispatcher.func2` | 各 ≈2 个消费者全场等待 |
| 106.6s | `Seele/event.(*HeartbeatManager).run` | 心跳 ticker |

### 4.4 对热点的归因与处理方向

归因是清楚的、也不是我们这条链路的逻辑缺陷：`Seele/session.Session` 的
`ChatStream` 实现是 **整段持锁**（`e.mu.Lock(); defer e.mu.Unlock()` 包住整个 ReAct 循环），
而 `History()` 用的是**同一把锁**。子代理这次重跑了 1200 字长文（~42s 流式），
于是两个观测者——**子代理表格投影**（`SubagentTree.projection`）和
**子代理记录落账**（`SubagentSessions.buildRecordLocked`）——各自被挡了约 28s。

它不会死锁（观测者是可重入的短请求、上游最终会放锁），但它是**观测路径与上游会话锁耦合**：
子代理跑长任务是常态，观测面被拖 28s 会直接表现为“表格卡住不动”。

建议方向（未在本轮实施）：

1. 表格投影不要读上游 `Session.History`，改读我们已有的节点增量面
   （`Deps.RecordNodeAssistant` 的流式正文 + 节点阶段事件）；
2. 落账路径若必须读 History，走只读快照 / 带 TTL 的缓存，不要嵌在持锁期间；
3. 补一条断言“长流期间表格投影不阻塞”的回归测试，防止后续再退化。

## 5. 与设计稿的一致性核对

| 设计约定 | 本轮证据 |
|---|---|
| provider role 只有标准集；只有真实用户输入是 `user` | 恢复说明与单元级 `note_role` 均为 `system`；主文档真实轮次 `user_rows=1, assistant_rows=1`（编排态材料没有被伪装成 assistant/user） |
| subagent 是 tool calling 能力，不是团队成员 | `team.view` 无 subagent 角色行；subagent 只出现在表格（`subagent.list`），恢复走 tool-chain 续跑语义 |
| AgentTeam 是 A2A 抽象工厂实例，goal TL 只是第一个实例 | 同一工厂 + 同一 RPC 面装配出 `goal-a2a` 与 `review-team`，preset 清单还含 `research-team` |
| 定时 agent 不入工作顺序 | `set_order` 收 `timer` 角色被拒绝，`team.view.scheduled` 单列且 `in_order=false` |
| 运行中的 active 不写进历史，只在表格 | 冷启动能从表格看到 `active=true, resumable=true`，恢复说明以 `system` 注入后再重跑 |

## 6. 未覆盖与下一步

- 未覆盖：多团队并发装配的压测；>5min 长流下的锁等待上界；Linux/macOS 的 `-race`
  （本机是 Windows + `CGO_ENABLED=1`，`-race` 已实际执行）。
- 未覆盖：`role.*` 单独的真实 API 探针本轮未重跑（上一轮
  `TestRealAPIRoleSessionLiveProbe` 通过，证据见同目录历史文件）。
- 下一步：按 §4.4 解耦投影与上游会话锁，并补长流回归断言。

## 7. 证据清单

原始产物都在 `tmp/headless-smoke/reports/`（`tmp/` 不入库，本机可复查）：

| 产物 | 内容 |
|---|---|
| `team-live-20260910-220736.json` | A2A 冒烟结论（preset/顺序/定时分区/真实轮次） |
| `subagent-live-20260910-220934.json` | subagent 恢复结论（phase1 现场 + phase2 六步报告） |
| `role-mutex-1789049374251234100.txt` | 冷恢复进程 mutex profile |
| `role-block-1789049374301418100.txt` | 冷恢复进程 block profile |
| `fork-live-goroutine-subagent-live-97432.txt` | 冷恢复进程 goroutine dump（21 个，无互斥等待） |
| `role-mutex-1789049256184789200.txt` / `role-block-1789049256194175100.txt` | A2A 冒烟对照 profile（无锁热点） |
| `fork-live-goroutine-team-live-96036.txt` | A2A 冒烟 goroutine dump |

相关设计与工作包：

- [a2a-agent-team-factory.md](../arch/a2a-agent-team-factory.md)
- [2026-09-10-a2a-agentteam-recovery/README.md](../2026-09-10-a2a-agentteam-recovery/README.md)
- [provider-role-audit.md](../2026-09-10-a2a-agentteam-recovery/provider-role-audit.md)
