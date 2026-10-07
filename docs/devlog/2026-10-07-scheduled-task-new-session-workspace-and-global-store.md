# 定时任务：工作区装配 + 每次触发新建会话 + 定义落全局 JSONL + 编辑

- 日期：2026-10-07
- 起因（用户口径，三件事一起给）：
  1. 「定时任务需要支持工作空间的装配」；
  2. 「每次定时任务发起的时候默认是新开会话发起」；
  3. 「定时任务需要做出全局粒度的 jsonl 的存储，而不是按照项目粒度或者会话的
     粒度存储。当然会话记录还是按照会话本身的存储和读写纪律」；
  4. 「任务需要支持编辑，这个页面和链路需要搭下」。
- 结论：四条一起落地。落点判据收进 `PromptExecutor` 契约一处实现；新会话的
  引擎装配与草稿物化共用同一份 `openSessionEngine`；任务定义进
  `<store>/scheduled-tasks.jsonl` 这一条全局 append-only 通道，会话正文一个字节
  都没进这个文件；编辑与创建共用同一套弹窗与同一份定义判据（`normalizeSpec`）。

---

## 1. 落点：默认新建会话，可选装配工作区

改前：prompt 任务执行器是 `func(ctx, prompt, sessionID)`，空 `sessionID` =
「执行时当前主会话」，显式绑定还要在会话切换后**跳过**（
`main.scheduledPromptExecutor` 里的"绑定会话 ≠ 当前会话 → 本次跳过"）。

改后：契约是

```go
type PromptOutcome struct{ Message, SessionID string }
type PromptExecutor func(ctx context.Context, prompt, sessionID, workspaceID string) (PromptOutcome, error)
```

判据只有一条（写在 `PromptExecutor` 的 doc 注释里，实现只有 `main` 那一份）：

| `ScheduledTaskSpec` | 落点 |
|---|---|
| `SessionID` 为空（默认） | 新建会话发起；`WorkspaceID` 非空时先装配到该工作区 |
| `SessionID` 非空 | 投递到那个既有会话 |

新建路径落在新的应用入口 `Service.StartScheduledSession`（
`application/core/service_scheduler.go`）：

1. 早分配 `sched_<时间戳>_<序号>` 会话 ID（`newGeneratedSessionID("sched")`）；
2. `openSessionEngine`：建引擎 bundle → `SetSessionWorkspace` → 挂自己的 context
   store → 写自己的 system prompt →（有工作区时）`Workspace.BindSession`；
3. `submitConversationFor` 在**后台**开回合——不切用户的视图指针（`active=false`
   分支本来就是给后台会话用的）；
4. 实际落点会话号回填任务状态的 `last_session_id`，面板/冒烟据此指认"跑到哪儿"。

工具根不用另外处理：`runChat` 起点本来就有 `bindSessionProjectRoot`，会话绑了
工作区就按会话解析到那个项目的根（`session_project_root_test.go` 钉的就是这条）。

> 为什么不在 `materializeDraftSession` 里复用草稿槽：草稿槽是**视图单例**，
> 物化会把共享视图镜像切到那个 SID（用户正在看的会话被换掉）。定时任务在后台
> 开新会话，跟草稿物化只共享"引擎 + context + 项目绑定"这一段，不共享视图切换。

## 2. 新增会话这一格：一份装配，两处调用

`materializeDraftSession`（草稿首提物化）与新入口原先会各写一遍
"建引擎 bundle + 显式键 + 挂 context + 写 prompt + 绑工作区"。这次把这段收成
`Service.openSessionEngine(sessionID, workspace) (string, error)`
（`application/core/session_scope.go`），返回实际生效的会话 ID（多会话宿主 =
传入的早分配 SID；legacy 单会话引擎回退 `StartSession()` 自分配）。

它**刻意不碰**进程级执行面（全局工程根 / Router 写作用域）：那条面属于"当前视图
会话"，后台新建的会话必须留给 `runChat` 的按会话绑根，否则新建一个后台会话就会
把别的运行中会话的项目根改掉。

顺带合并同一件事的第二、第三份实现：`session_lifecycle.go`（热挂载）与
`session_history.go`（冷恢复）里各自的
「`SetSystemPromptFor` 优先、回退全局 `SetSystemPrompt`」收成
`Service.writeSessionSystemPrompt(sessionID)`（`session_scope.go`）。三处判据
（哪条路写 prompt）现在是同一个函数，改一处三处一致。

## 3. 存储：任务定义一条全局 JSONL，会话正文照旧

改前：任务只活在进程内（上一批 devlog §7 如实记过"重启即清空"）。

改后：`seelebridge/scheduler/persistence.go` 给出

- `Persistence` 端口：`Append(record)` / `Load()`；
- `FileStore`：一个路径、一条 append-only 通道、一行一条 JSON、`O_APPEND` 写
  整行收尾；`Load` 按 ID 后写覆盖先写重放，丢弃墓碑行、读不动的行与**未收尾的
  残尾**（没有换行 = 没提交完，与会话事件行的恢复语义同一口径）；
- 装配路径：`RuntimeConfig.ScheduledTasksPath` → `main.go` 给
  `filepath.Join(filepath.Dir(*storePath), "scheduled-tasks.jsonl")`
  （与 `workspace_index.json` 同级，仍在 `.seelex/` 数据根内）。

粒度：**全局一份**。任务定义不按项目分区、不按会话分片——"每天九点巡检"在任何
工作区下都是同一份列表。触发产生的会话记录仍然按会话自己的存储与读写纪律落盘
（项目作用域由那次会话的 workspace 绑定决定），`persistence.go` 里没有任何
对 sessionstore 的依赖。

只存**定义与启用状态**（`ScheduledTaskRecord{ID, Spec, Enabled, Deleted, UpdatedAt}`），
不存 `LastResult` / `LogTail` / `RunCount`：那是运行期展示面，命令输出还可能含
敏感内容，不该因为"重启后想看见上次结果"就落盘。代价如实记在 README 里。

### 冷启动恢复（`State.Restore`）

| 记录 | 恢复行为 |
|---|---|
| 周期任务 | 重算下次运行时间；停机期间错过的触发点**不追补**；停用的恢复为停用 |
| 一次性任务（`RunAt` 还在未来） | 按原时刻恢复 |
| 一次性任务（`RunAt` 已过） | 不恢复：一次性的时刻过了没有可执行的意义，也不留假的待运行行 |
| 命令不在白名单 / 周期非法 / 执行器未装配 | 逐条跳过（返回 恢复数 + 跳过数） |

调用点在 `main.go` 的 `ApplyDeps` **之后**（执行器与观察者都注入了，恢复出来的
任务才可能立刻按排期触发），装配失败/跳过都以一行日志如实报出来。

写失败怎么算：`Schedule` 落盘失败 = **没登记**（内存回滚 + 上抛），`CancelTask`
墓碑写失败 = **没取消**（内存保留）。避免"面板没了、磁盘还在"这类劈叉。

## 4. 前端

- 新建弹窗（`index.html`）加 `#sched-workspace` 下拉：选项来自快照的
  `workspaces`，默认跟随当前会话绑定的工作区，空值 = 不绑项目；说明文案同步改成
  "到点会新建一个会话发起（不打断你正在看的会话）"。
- 载荷组装仍在唯一那处 `buildScheduledTaskSpec`：加 `workspaceId`（周期与一次性
  两条路径都带；不选 = 空串，不是 `undefined`）。
- 面板/表格给绑了工作区的任务加一枚 `sched-chip-workspace`：名字从快照的
  `workspaces` 里取，取不到就退回显示 ID（ID 是索引，展示层宁可显示 ID 也不丢信息）。

## 5. 编辑：同一套弹窗、同一份判据、同一条落盘通道

链路：`data-sched-edit`（ID 是操作键）→ `Bridge.UpdateScheduledTask(id, spec)` →
`Service.UpdateScheduledTask`（判工作区可用）→ `Runtime.UpdateScheduledTask` →
`scheduler.Update` → 同一 ID 的**追加定义行**。

- **页面**：编辑不新开弹窗，用的是新建那一个（标题 / 提交按钮 / 启用勾文案切到编辑态）。
  表单回填的唯一映射是 `scheduledTaskFormFields(task)`（`scheduled-tasks-view.js`），
  提交仍走 `buildScheduledTaskSpec` —— 两条腿都是纯函数，node 用例钉住"还原字段再组装
  得到同一条定义"。`session_id` 面板上不编辑，但编辑提交原样带回：改个名字不该顺手把
  API 侧设的绑定清掉（新建路径这一格恒为空 = 默认新建会话）。
- **判据**：`Schedule` 里原来那段校验整块抽成 `normalizeSpec`（唯一一份"什么算合法定义"），
  `Update` 与冷启动 `Restore` 都走它。`restoreRecord` 因此不再自带第二套校验：
  它只加一条恢复特有的口径——**执行时刻已过的一次性任务不恢复**。定义字段进任务/状态的
  落点也收成 `applyDefinition`（创建 / 编辑 / 冷启动三处共用），杜绝"编辑后某几格还是旧值"。
- **语义**：整体替换（PUT）——面板上是什么，任务就是什么；ID 不变，运行账目
  （`run_count` / 上次结果 / 上次落点）保留，下次运行按新定义重算。落盘顺序与取消同构：
  **先写新定义行，成功后再改内存**，写失败 = 这次编辑不成立。
- **持久化**：不做"改一行"的就地编辑（append-only 通道没有原地改），而是同一 ID 再追加
  一行定义；`Load` 的后写覆盖先写天然给出"编辑后的定义"，无需墓碑、也不换 ID。

## 6. 钉子与门禁

| 处 | 内容 |
|---|---|
| `seelebridge/scheduler/persistence_test.go` | 全局单文件（一行定义、无项目作用域字段）→ 冷启动恢复（ID/工作区/锚点/下次运行）；取消写墓碑且不再恢复；恢复逐条跳过（过期一次性 / 未知命令 / 坏周期 / 空名 / 执行器未装配）；残尾丢弃；缺失文件 = 空集 |
| `seelebridge/scheduler/scheduler_test.go` | 执行器参数（prompt / sessionID / **workspaceID**）与落点会话回传；编辑整体替换（ID 不变 / 账目保留 / 重算排期）与"编辑与创建共用校验"（7 种非法搭配逐条拒绝且不动内存） |
| `seelebridge/scheduler/persistence_test.go`（续） | 编辑落成同 ID 的第二行（不是墓碑），冷启动恢复出**编辑后**的定义 |
| `application/core/service_scheduler_test.go` | `StartScheduledSession`：新会话 ≠ 当前会话且带 `sched` 前缀；工作区绑定 + 按会话工具根；**视图指针不动**；提示词落新会话、当前视图会话一条不多；不指定工作区时不绑项目。`UpdateScheduledTask`：按 ID 转发、不存在的工作区/空 ID 当场拒绝且不惊动调度器 |
| `gui/bridge_test.go` | `TestEmbeddedScheduledFormCarriesWorkspacePicker`（弹窗有工作区下拉、`app.js` 把选中的 ID 递进载荷）；`TestEmbeddedScheduledEditEntryWired`（列表/表格有编辑按钮、回填走 `scheduledTaskFormFields`、提交走 `UpdateScheduledTask`、标题按编辑态切换）；`TestBridgeForwardsScheduledTaskCommands` 补编辑转发 |
| `gui/frontend/dist/scheduled-tasks-view.test.mjs` | 载荷两条路径都带 `workspaceId`（不选 = 空串）；工作区 chip 取名字、缺名字退回 ID；编辑按钮；`scheduledTaskFormFields` 的来回（周期/当前时间/一次性/旧 interval 任务/畸形输入） |
| `scheduled_task_live_smoke_test.go`（opt-in） | 判据 3 改成"落到 `last_session_id` 那个**新会话**"；判据 5 走真实链路**编辑**（ID 不变、名称更新、账目保留、下次运行重算） |

```text
gofmt -l . ; go build ./... ; go vet ./...
go test ./seelebridge/scheduler/... ./application/... ./gui/... -count=1
node --test gui/frontend/dist/*.test.mjs
```

## 7. 未做（如实记）

1. **没做"投递到指定会话"的 UI**：弹窗仍只创建"默认新建会话"的任务
   （`session_id` 固定空）。显式的既有会话绑定在 API 面保留并已按新契约实现
   （投递而非"切换后跳过"），但没有入口——那要有"选一个已有会话"的下拉，属于
   下一批的产品决定。
2. **`last_session_id` 只进 JSON，未在面板上呈现**：面板没有"点开这次落点"的
   入口，显示一个 ID 对用户没有用处；等有了跳转再做。
3. **面板不编辑「绑定会话」**：编辑时原值原样带回（不显示、也不提供入口）。要能选
   "投递到哪个既有会话"，得先有"选会话"的下拉，属于下一批的产品决定。
4. `scripts/gen_core_readme_index.py` 的**分卷覆盖自检当前是红的**：
   `plugin_source_projection_test.go`、`turn_status_single_word_test.go` 两个已入库
   文件没有归入任何分卷，脚本因此拒绝刷新。本次没有连带修它（会引入一大片无关的
   索引漂移）；`application/core/README-service.md` / `README-session.md` 的新条目
   是按生成器同一套提取规则手工对齐的。
