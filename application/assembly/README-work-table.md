# assembly/work-table（根包分卷）

## 生态位

后台作业面与工作表格投影：执行登记表的只读投影、请求尾部打点块（在途行 + 待取回的完成行）、作业终态为**空闲**会话触发对话、项目看板的派生投影（按会话落盘 + 按需读取面）

覆盖：`work_table*.go`、`async*.go`、`project_board*.go`；未归属文件由覆盖自检拦下。

## Execution Application 用例（AP-017 首个切片）

`WorkItem execution application` 集中 WorkItem execution、teammate inspection/live、
团队看板和 todo 状态更新。Runtime 与 teamwork board reader 仍是具体能力提供方；
Service 入口只保留一行转发。

`worktable.changed` 既是表格增量，也是子代理树的送达通道：树内容变化时
随包附带 `subagent_tree`（清空时显式空数组），前端详情入口据此把工作表格行
解析成弹窗节点，避免"行先到、树未到"时详情打不开。

AP-115C1 起，latest-wins publisher 与树 payload 去重已迁入
[`application/execution/table`](../execution/table/README.md)；assembly 只保留树快照、
session ID 和事件发布回调。
AP-115C2a 起，TraceBlock 的过滤、排序、预算与 async/teamwork 提示也由该包生成。
AP-115C2b1 起，async/teamwork 信号 consumer 和补扫 timer 由该包管理。
AP-115C2b2 起，subagent/task consumer 与三路 start-once 也由该包管理。
AP-115C2c 起，WorkItem/批次输入投影与 task-change 依赖补齐也经该包委托。

## 后台命令行（第四路输入，只读投影）

`buildWorkTable` 有四个输入：plan、task 注册表、子代理树，以及 **后台执行登记表投影**
（`Runtime.AsyncRunsSnapshot()`）。第四路刻意**不写进 task 注册表**：注册表随会话
`record.Tasks` 落盘并在恢复时回灌，而后台句柄活在内存——真进去就会在重启后留下一条永远
running 的假行。因此后台行的生命完全跟着登记表：派发出现、探针更新、终态被驱逐即消失
（不变量 I-21：registry 是 async 行的唯一事实源，投影只读、不落盘）。

- 行身份：`ID = SourceID = async:<handle>`、`kind = task`（owner 裁定）；行标题取模型派发时
  写的 `description`，描述列是命令行原文，探针读数（state/exit/已产出字节/末行/耗时/是否
  降级）落在可展开的打点行，日志路径落在附件列——**这四项前端零改动**，列位本来就存在。
- 三处建表入口（实时重投影、会话快照、冷读面）统一走 `asyncRunsForTable()`，避免"切到哪个
  会话才看到哪些行"。行数上限 `asyncWorkMaxRows`（在途优先、终态按最新补齐），否则登记表
  的 256 个记录槽会挤掉真实任务行。
- 请求尾部打点块并**在途行 + 待取回的完成行**（打点 K-5 的完成回填），且字段比界面窄：
  句柄、`kind`、state、已产出字节，在途行带标题、完成行带**有界摘要**（state + exit +
  行数 + 字节数 + 有界末行，≤512B）。**不含**日志路径、末行原文（摘要里的末行是压成一行、
  限长的采样）、时间戳——那三者是探针给 GUI 的，进上下文等于每轮重付一遍，还会把日志正文
  变成注入面。整块行数（含开/闭标记、标题与读法说明）≤
  `application/execution/table.TraceMaxLines`；被截断
  的完成行补一行汇总，不静默消失。模型 `bg_manage(op=fetch)` 取回（或 `op=done` 销项）
  之后那一行才从投影里消失——history 里的结果成为唯一事实。
- 生命周期消费者由三个变四个：`consumeAsyncRuns()` 收登记表的变化信号（派发/终态/驱逐/
  去抖后的新字节），走 `refreshWorkTableFromSources()` 复用同一条发布路径，所以表格与打点块
  不会分叉。信号是容量 1 的汇聚口，中间态被合并掉无所谓：每次重投影读的都是当下全量。
- 同一个信号还驱动**后台作业终态触发对话**（`async_completion.go`）：落到 done / failed 的
  作业若所属会话**空闲**，就为它起一个回合（`submitConversationFor`），正文给出 handle 与
  取回指令；忙会话不唤醒（铁律 §6.1），它走上面那条"完成行回填"。它刻意**并进**
  `consumeAsyncRuns` 而不是再起一个消费者——信号口是容量 1 的**单接收者**通道，两个消费者
  抢同一次发送时只有一个收得到。开关 `limits.async_exec.trigger_conversation`（默认关，
  出厂打开）；幂等键是句柄，账本随登记表裁剪、被登记表规模封顶。
- 锁纪律：登记表锁是**叶子锁**（tools 侧从不回调进 application，只发 channel），所以持
  `ViewMu` 时读它是安全的；必须锁外取的是 `Engine.SubAgentTree()` 那类会拿会话锁的读面。

## 项目看板投影（project board，M2/M3；落点 M7 搬迁、M9 按会话分目录）

把**已经存在**的两份投影按会话落成一组追加式 JSONL **派生件**，供按需读取；物理落点是**项目目录下按会话分的目录**（2026-10-15：先从数据根 `project-board/project-<hash>/…` 搬进项目根，再**按会话分目录**——一个会话一份文件，防串写、水位按会话各自推进、回收即摘目录）：

```text
<项目根>/.seelex/board/<会话 ID>/worktable.jsonl   # 工作：todo / task 行（会话私有）
<项目根>/.seelex/board/<会话 ID>/teamwork.jsonl    # 编排：milestone / milestone_item 行
<项目根>/.seelex/board/BOARD.md                    # 人读件（全项目汇总，看板根）
```

- **落点解析只有一处**：`Service.ProjectRootFor(sessionID)`（= 会话所属工作区的 `RootPath`；
  未绑定工作区返回 `""` ⇒ **不落盘**，契约 §3）。存储面用
  `Router.SetProjectBoardRootFor(func(sessionID) string)` 注入同一解析器——存储面不认识工作区，
  因此会话删除时的回收挂在 `Router.DeleteWorkspace` 上（不在 `jsonRepository.Delete` 里）；
  落点目录由 `store.ProjectBoardDir(projectRoot)` / `ProjectBoardSessionDir(projectRoot,
  sessionID)` 一处派生（固定名 + 字面会话 ID，不再派生 hash 目录）。
- **事实层不动**：工作表格行来自 `application/execution/table/runtime.go`，里程碑/工作项来自
  teamwork 计划与运行态（`store/teamwork.go`）。本模块只做**字段映射**，不判定、
  不重算状态（`AGENTS.md §8.3` 的唯一判据纪律）。
- **纯函数**：`BuildProjectBoardRows(scope, seq, at, workItems, teamPlan, teamState)`。
  只搬 `kind ∈ {todo, task}` 的行——plan 节点与 subagent 行各有自己的读面（`read_plan` /
  子代理树），搬进来就是第二份判据。里程碑状态**运行态优先、声明态兜底**；整队是否收口
  读 `TeamworkState.State`（**不是** `TeamworkPlan.State`，那是 `TeamworkState` 结构体本身）。
- **单写者同步器**：`ProjectBoardSyncer`。`OnToolComplete` **不做 IO**（判触发面 → 标脏 →
  非阻塞唤醒；信号口容量 1、latest-wins），唯一 flush goroutine 跑全部落盘：取活体 →
  `BuildProjectBoardRows` → 内容指纹去重（去掉 seq/at）→ 差集补 `payload{"removed":true}`
  （tombstone）→ 追加。写失败只记日志（投影可重建），不 panic、不阻塞回合。
- **I1 自愈**：指纹相同但水位退到记账值之下（文件被删/被截）⇒ 即使内容没变也重建，
  否则一次误删会被去重固化成"投影永远缺失"。
- **触发装配表**：`TriggerFor(hook)` = `off | todo_list（缺省）| task_complete |
  tool_complete/all`；词表唯一来源是 `ProjectBoardHookModes()`（装配期用它校验配置里拼错的
  档名，**显式启动失败**，不静默降级成"从不触发"）。
- **注入缝**：`Service.SetProjectBoardSink`（`components.projectBoard` 槽，装配期交接一次）；
  完成段在**既有** `refreshWorkTableFromSources()` 之后交接——`ToolHookBridge.Hooks()` 是固定
  闭包，不新造回调入口。
- **不进上下文（I7）**：投影不进 system/project 前缀，也不进请求尾部；能力开/关的装配前缀
  逐字节相同（根包 `project_board_i7_test.go`）。
- **关就是关**：`limits.project_board.enabled=false` ⇒ 不注入订阅口、不注册读取面、一个文件
  都不写。**出厂档 `config/seelex.yaml` 显式打开**（2026-10-15 起：代码落地 ≠ 盘上有东西，
  缺省必须是"真的会留下投影"那一侧；回滚臂就是这一行改回 false）。
- **写侧端到端**：根包 `project_board_write_test.go` 走**生产口径**的全链路——建工作区 →
  `BeginNewSession`（早分配 SID）→ 绑定工作区 → 模型真的调 `todo_add` → 工具完成段 →
  同步器取活体工作表格 → `<项目根>/.seelex/board/{worktable.jsonl,BOARD.md}` 真的出现，
  且行（落在该会话自己的目录里）归属该会话。它同时钉住 R-2 那条前提：**视图会话 = 运行时当前 task 会话**时
  `WorkTableRowsFor` 才取得到行（不是该口径的会话取到空集 ⇒ I6 退场、什么都不落）。
- **按需读取面（M4）**：插件技能条目 `plugins/default/project-board/SKILL.md`（只有 name +
  description 进技能目录）+ 只读工具 `project_board_read`（`registry.RegisterProjectBoardReadTool`，
  **只在 enabled 时注册**；取数在 `kernel/board/read.go`（P21-11 归位），有界、越界显式报错）。

契约：`docs/arch/seelex-project-board.md`。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### async_completion.go

- `func (service *Service) sessionIdleForAsyncTrigger(sessionID string) bool` — sessionIdleForAsyncTrigger reports whether a session can start a completion
