# `store` DDD 拆分下一开发会话提示词

日期：2026-10-27
状态：**可直接用于下一开发会话**
上位文档：

- [`plan.md`](plan.md)
- [`milestones.md`](milestones.md)
- [`external-change-protocol.md`](external-change-protocol.md)

## 1. 可直接粘贴的开发提示词

```text
工作目录：G:\Program\go\seelex

你的任务：只执行 SEELEX-STORE-DDD M0「基线门禁」，不要开始 M1，不要改业务语义。

背景：
store 根包已经确认按 DDD 做增量 strangler 拆分。方案与里程碑位于：
- docs/2026-10-27-store-ddd-split/plan.md
- docs/2026-10-27-store-ddd-split/milestones.md

预算基准：application 拆包约 1 天 1 夜、成本约 150（币种未注明），但应用层仍未拆完。
因此 150 是未收敛投入下限，不是完整拆包基准；store 总预算已上调到
约 2.29M–4.41M tokens、86–152 个有效小时、校准成本 600–1100。
本 M0 预计累计消耗 100k–200k tokens、3–6 个有效小时、校准成本 25–55。
若明显将超过 200k 且完成度仍低，暂停并报告原因；不得通过少跑测试压预算。

M0 的目标是固定当前行为与结构边界，为后续 Board/Layout/Artifact/Session/Context 拆分提供红绿证据和回滚基线。M0 默认只允许测试、测试资料和结构门禁改动；除为构造可控失败点而必须增加的最小测试接缝外，不改生产实现。若必须改生产代码，先停止并说明为什么无法只用现有 API 测试。

开始前必须完整阅读：
1. MEMORY.md，尤其危险操作铁律和新功能归属决策。
2. AGENTS.md，尤其 §0、§5、§8、§9。
3. docs/2026-10-27-store-ddd-split/plan.md。
4. docs/2026-10-27-store-ddd-split/milestones.md。
5. docs/2026-10-27-store-ddd-split/external-change-protocol.md。
6. store/README.md 以及与本任务相关的分卷：
   - store/README-session.md
   - store/README-message.md
   - store/README-compact.md
   - store/README-retention.md
7. docs/arch/absent-state-reads.md。
8. 以下现有测试/门禁：
   - store/state_test.go
   - store/conversation_test.go
   - store/event_range_test.go
   - store/event_store_test.go
   - store/draft_append_test.go
   - store/pending_tail_test.go
   - store/compaction_records_test.go
   - store/retention_prefix_eviction_test.go
   - store/lifecycle_fork_test.go
   - store/module_lock_identity_test.go
   - store/message_read_lock_test.go
   - store/head_self_heal_test.go
   - store/head_rebuild_test.go
   - e2e/store_boundary_test.go

先运行 git status --short，记录并保留所有已有改动。不得回滚、删除或格式化无关文件；当前工作树可能有其他迁移在并行进行。

如果本批次会修改当前领域目录之外的文件，或会改变外部包消费的导出类型、方法、常量、
接口或 JSON 形状，必须先完整执行 external-change-protocol.md，不能直接批量改：
1. 为外部批次创建专用 codex/store-ddd-<milestone> branch/worktree；
   脚本禁止在当前主工作区运行。若无法创建隔离 worktree，停止并报告。
2. 在专用 worktree 中生成外部影响清单，逐文件记录 package、依赖项、被依赖项、
   使用符号、替换项和验证包。
3. 确认清单内文件没有任务外的未提交改动；不得把主工作区的并行迁移带进来。
4. 在专用 worktree 内提交 checkpoint commit，保存 commit hash；不得 push。
5. 迁移脚本必须落盘、版本化、支持 dry-run，并只处理清单内文件；
   执行时校验 worktree 根与 branch，不匹配立即退出。
6. 脚本失败必须自动恢复本次触碰的文件；保留 worktree 和 journal 供诊断，
   不清理、不归档，直到报告完成。
7. 脚本成功后先在 worktree 内跑编译和局部测试，再提交批次变更并跑全量门禁。
8. 全量门禁失败时在 worktree 内对批次提交执行 git revert，保留 checkpoint，
   不改用户数据。

没有专用 worktree、外部影响清单、checkpoint、脚本和回滚证据的跨包改动不得交付。

禁止：
- make clean / make release / rebuild-gui / publish-rebuild-gui。
- Remove-Item、rm、git clean、git reset --hard、git checkout --。
- 删除或重建 .seelex、dist、config 或任何用户数据。
- 读取、打印、提交 config/accounts.yaml 或 *.local.yaml。
- 除 external-change-protocol.md 明确规定的 checkpoint / 批次 commit 外，自行 commit；
  任何时候都不得 push。
- 未执行 external-change-protocol.md 就批量修改外部包。
- 在主工作区运行外部依赖批量脚本。
- 为了让测试通过而修改现有断言、放宽契约或复制生产判据。

M0 需要交付：
1. 代表性会话物理布局 golden：message/event/compact/retention/lifecycle/tool-result/media。
2. Commit 写序特征测试：tool result → message head → compaction records。
3. Commit 半失败特征测试：明确并锁定当前“message 已发布后 compaction 失败”的真实可见性和错误语义。
4. 锁身份门禁：同模块同锁、不同模块不同锁、未知模块显式失败。
5. God 类型方法面 ratchet：Router、jsonRepository、storeEngine 只许收缩。
6. I6 import 门禁扩到 ./store/...；领域包未来不得 import 根 store 或上层。
7. 一份代表性旧会话 fixture，能由当前实现冷读，作为后续每批回归基线。
8. 当前耦合基线报告：方法数、直接 import 根 store 的生产/测试文件数、领域文件清单。
9. 若 M0 需要修改外部测试/门禁文件，附着专用 worktree/branch、外部影响清单、
   checkpoint hash、脚本 dry-run/应用结果。

红测要求：
- 先运行新测试并保存失败证据，再补最小实现使其通过。
- 若现有代码已经通过某个行为断言，说明它是特征测试不是红测，不要伪造红灯。
- 如果发现注释、测试和实际行为冲突，停止并把冲突写入报告，不擅自决定新的产品语义。
- 不得通过 sleep、放宽超时、跳过测试或环境判断掩盖竞争/失败。

实现后至少运行：
- gofmt -l .
- go test ./store/... ./e2e/... -count=1
- go test ./test/ -count=1
- git diff --check

如果全仓测试代价过高，先跑上述最小集；汇报时明确哪些全仓门禁未跑及原因。

最终报告必须包含：
1. 已完成项，以及每项对应的证据。
2. 正在完成项；若没有持续执行任务，必须写“无”及判定依据。
3. 未完成项，包括本里程碑未交付、跳过和推迟到后续里程碑的事项。
4. 未决风险，包括风险、影响、缓解措施、阻塞点和裁决人。
5. 下一步内容，必须给出里程碑、入口文件或动作及首次验证命令。
6. 改了什么，以及为什么属于 M0。
7. 红测首次失败的命令和关键输出。
8. 新增/修改的测试、fixture、结构门禁。
9. 当前 Router/jsonRepository/storeEngine 方法数。
10. 直接 import 根 store 的生产文件/包数、测试文件数。
11. 外部包影响面与哪些里程碑会触及它们。
12. 专用 worktree/branch、外部影响清单路径、依赖项/被依赖项、脚本路径、
    checkpoint hash、dry-run/apply 结果。
13. 数据兼容结论：路径、JSON tag、schema、head、watermark 是否零变化。
14. 删除/合并了哪些重复判据；若没有，明确写没有。
15. 回滚确认：自动恢复是否演练、批次提交如何 revert、是否触摸用户数据。
16. Token 预估、可观测实际用量（不可得时明确写“不可观测”）、校准成本和偏差原因。

完成 M0 后停止，不要自动进入 M1。
```

## 2. 必读文档与用途

| 文档 | 必读原因 |
|---|---|
| `MEMORY.md` | 危险操作、用户数据位置、新功能归属、会话存储约束 |
| `AGENTS.md` | 模块边界、文档规则、测试门禁、单一实现、契约优先 |
| `docs/2026-10-27-store-ddd-split/plan.md` | 目标包结构、领域边界、Commit/Layout/Facade 设计 |
| `docs/2026-10-27-store-ddd-split/milestones.md` | M0–M11 的交付、门禁和逐里程碑回滚 |
| `docs/2026-10-27-store-ddd-split/external-change-protocol.md` | 外部依赖清单、脚本批次、checkpoint 和回滚唯一事实源 |
| `store/README.md` | JSON v8、Router、存储布局、review 风险和验证入口 |
| `store/README-session.md` | 会话句柄、上下文、历史读面 |
| `store/README-message.md` | message/event 事件行、wire 和发布点 |
| `store/README-compact.md` | compact 帧、压缩记录和运行期缓存 |
| `store/README-retention.md` | 草稿尾、队列和 LRU 水位 |
| `docs/arch/absent-state-reads.md` | 缺失 head 的业务状态读取口径 |
| `e2e/store_boundary_test.go` | 底座不得向上依赖的机械门禁 |

如果本任务触及 teamwork/role，还必须读 `store/README-team.md` 和当前 teamwork 迁移文档；
M0 不触及这些实现。

## 3. M0 改动边界

### 允许修改

- `store/**/*_test.go`
- 新的 test fixture，但必须落在现有测试资源或明确测试目录。
- `e2e/store_boundary_test.go` 及必要的 test helper。
- M0 测试资料和一次性工作包文档。
- 外部影响清单和一次性迁移脚本，但必须服从下节协议。
- README 中仅限测试命令、门禁清单或事实状态同步。

### 原则上禁止

- `store/**` 生产 Go 文件。
- `contract/**` 中的生产 DTO/接口。
- `application/**`、`middle/**`、`backend/**`、`wiring/**` 的生产代码。
- JSON 路径、tag、schema、head、watermark、锁粒度变化。
- 为未来包结构提前创建空包或未使用接口。

### 例外

若半失败测试无法通过现有 API 构造，只允许增加不改变生产行为的测试接缝。
提交前必须说明：

- 现有 API 为什么无法表达；
- 接缝是否只在测试构建中出现；
- 它如何保证生产路径行为零变化；
- 回滚是否只需删除测试接缝。

## 4. M0 红测设计约束

### 4.1 物理布局 golden

优先断言业务可见结果和稳定物理事实：

- message/event 分片名与已发布水位；
- compact/retention/lifecycle head；
- tool result 引用和落点；
- media 内容寻址目录；
- 重开存储后能读回相同事实。

不要复制整份磁盘树做脆弱的绝对路径快照；临时根目录必须使用测试临时目录。

### 4.2 Commit 写序

至少覆盖：

- 只有 Events；
- Events + ToolResults；
- Events + CompactionRecords；
- Events + ToolResults + CompactionRecords；
- 同一 commit 重复提交。

断言重点是顺序和可见性，不是内部函数调用次数。

### 4.3 Commit 半失败

必须固定当前语义：

- artifact 成功、message 未发布时是否成为不可见孤儿；
- message 已发布、compaction 失败时返回值是什么；
- 重试同一次提交会产生什么；
- reopen 后能读到什么。

如果无法注入失败，先写测试缺口说明并请求最小接缝审批，不要直接改生产实现。

### 4.4 锁与结构门禁

- 同模块多次取锁必须是同一临界区；
- 不同模块不得静默别名；
- 未登记模块必须显式失败；
- `Router`、`jsonRepository`、`storeEngine` 的方法数只能下降；
- `./store/...` 的传递依赖不得出现 application/middle/backend/wiring。

## 5. Review 重点

开发会话提交 review 时，审查者按以下顺序检查：

1. **归属**：每个新增测试/门禁是否确实属于 M0，有没有提前实现未来包。
2. **红测真实性**：是否先红后绿；是否改旧断言迁就实现。
3. **行为兼容**：路径、JSON、head、watermark、错误语义是否不变。
4. **单一判据**：是否新增了第二份 session-not-found、折叠、锁身份或布局判定。
5. **并发**：测试是否真的覆盖发布/读取/清理竞争，而不是只跑顺序路径。
6. **结构门禁**：ratchet 是否可执行且不会阻止合法缩减；I6 是否包含测试文件 import。
7. **回滚**：M0 是否只删测试/门禁即可恢复，且不触碰用户数据。
8. **文档**：事实与测试命令是否同步，是否把规划写成已实现。
9. **无关改动**：是否保留工作树里已有的 application/teamwork 迁移。
10. **外部依赖**：是否在专用 worktree/branch 中执行；清单是否覆盖直接依赖和反向依赖；
    checkpoint 是否只含本任务；脚本是否只改清单内文件并能自动恢复。
11. **最终证据**：是否列出测试命令、结果、未跑项和风险。
12. **状态真实性**：已完成、正在完成、未完成是否互斥且可证；
    未决风险和下一步内容是否明确，不得用含糊描述代替。
13. **预算真实性**：是否先给出 token/工时预估；实际或不可观测状态是否诚实；
    是否通过减少测试、review 或回滚演练压低成本。

发现以下任一情况应退回：

- 生产行为被改动但没有单独说明和审批；
- 半失败语义被测试“定义成希望值”而不是锁定当前值；
- 结构门禁只是注释或约定，没有可执行断言；
- 新增 God 方法；
- 为了让测试稳定而扩大超时、增加 sleep 或串行化全局锁；
- 改动 `.seelex`、配置、dist 或用户 fixture 内容。
- 没有 checkpoint 或回滚证据就批量修改外部包。
- 没有专用 worktree/branch 就运行外部依赖批量脚本。
- 省略已完成项、正在完成项、未完成项、未决风险或下一步内容。
- 把跳过测试、未实施代码或等待用户裁决的事项标成“已完成”。
- 编造 token 实际值，或为了满足预算而省略测试、review 和回滚演练。

## 6. 当前外部包影响面

当前生产代码有 **91 个文件、22 个包**直接 import 根 `store`；
测试侧另有 **135 个文件、21 个包**。因此后续任何公开类型或方法签名变化都不能只看 `store/`。

每个会修改这些包或改变其消费符号的里程碑，都必须在
`docs/2026-10-27-store-ddd-split/impact/<milestone>-external-deps.json`
记录清单。清单至少包含：

- 专用 worktree 对应的 branch、base commit 和 checkpoint commit；
- 外部文件绝对仓库相对路径；
- 包路径和所属层；
- `depends_on`：该文件依赖的包和符号；
- `depended_on_by`：哪些文件/包反向依赖它；
- 计划替换的类型、方法、常量或 import；
- 迁移后必须运行的验证包。

### 6.1 按包族列出的直接依赖

| 外部包族 | 当前主要依赖 | 受影响里程碑 | 风险 |
|---|---|---|---|
| `application/goal` | Goal board、SessionContextStore、Router、SessionBoards | M1、M7、M8 | goal 归档和 context 读面可能同时变化 |
| `backend/engine/assemble` | Key、NodeSession*、Teamwork*、SessionContextStore、ProjectRecord | M2、M5、M7、M8、M9 | 汇合点最多，容易把多个域一起改 |
| `backend/engine/context` | CompactFrame、Event、EvidenceRef、SessionContext* | M5、M7 | provider context 行为敏感 |
| `backend/engine/context/seelectx` | CompactFrame、EvidenceRef、SessionContextRecord、Event、RouterStorage | M5、M7、M10 | 最大上下文读面，wire/prefix 回归风险高 |
| `backend/engine/context/seelectx/memory` | EvidenceRef | M7 | contract DTO 迁移 |
| `backend/engine/context/seelectx/search` | Event、Router、SessionContext*、is-not-found | M5、M7、M10 | 事件源、错误语义、StackSource |
| `backend/engine/obs` | EventStore、Event、SessionIDFromEvent、Router | M5、M10 | 观测事件链和关闭路径 |
| `backend/engine/session` | Event、CompactFrame、SessionContext*、ToolResult、端口 | M3–M7 | Session 拆分核心调用方 |
| `backend/multiagent/agentsession` | DurableHistory、Router | M5、M10 | 历史读写装配 |
| `backend/multiagent/goal` | Goal board、GoalFrame、SessionBoards、SessionContextStore | M1、M7、M8 | goal 状态与看板投影 |
| `backend/multiagent/subagent` | NodeSessionRecord、NodeSessionStore、EventLogEntry、Router | M5、M9、M10 | 子代理恢复与主会话索引 |
| `backend/multiagent/team` | TeamworkMember、ValidateTeamworkPlan | M8 | 团队装配校验 |
| `backend/multiagent/teamwork` | TeamworkPlan/Event/Binding/WorkItem | M2、M8 | 当前迁移尚未完全落定，必须等稳定 |
| `backend/tool/io/media` | Router、MediaItem | M3、M10 | 工具侧媒体落点 |
| `backend/tool/io/multimodal` | Key、MediaRefHash、MediaStore | M2、M3 | 媒体引用与 wire |
| `backend/tool/worktree` | NodeSessionRecord | M9 | worktree 与 workunit 身份 |
| `backend/workunit` | NodeSessionRecord、NodeSessionStore、NodeWorktreeRecord | M5、M9 | 进度、恢复、终态 |
| `middle/view/board` | ProjectBoard*、Router、Key、TeamworkState | M1、M2、M8 | 第一个试点消费方 |
| `middle/view/board/boardtest` | ProjectBoardRow、Fold/Append | M1 | 测试替身需要与契约同步 |
| `wiring/adapters` | Event、Router、RolePermission、Commit、ToolResult、SessionDisplayMeta | M2–M9 | 几乎所有 store 形状都在此处适配 |
| `wiring/bootstrap` | Router、Key、构造函数、EventStore、ProjectRecord | M2–M10 | 组合根改动，容易混入业务规则 |
| `wiring/registry` | ProjectKnown、TeamworkPlan、ProjectRecord、ProjectBoard | M1、M7、M8 | 工具注册与看板读面 |

### 6.2 按里程碑的风险排序

| 里程碑 | 首轮必跑的外部包 | 说明 |
|---|---|---|
| M1 Board | `middle/view/board/...`、`wiring/registry`、`application/goal` | 确认 Board DTO 无第二身份 |
| M2 Layout | 所有直接 store 消费者 | 公共签名应零变化；只需全仓编译作为传播检查 |
| M3 Artifact | `backend/tool/io/media`、`backend/tool/io/multimodal`、`backend/engine/session`、`wiring/adapters` | 媒体/工具结果引用不可漂移 |
| M4 Commit | `wiring/adapters`、`application/core`、`backend/engine/session` | 当前直接构造 store.Commit 的主要是 adapters |
| M5–M6 Session | `backend/engine/{session,assemble,obs}`、`backend/multiagent/*`、`backend/workunit`、`wiring/*` | 影响面最大，必须按包分批 |
| M7 Context | `backend/engine/context/...`、`backend/engine/assemble`、`application/goal` | provider 前缀和 goal 上下文 |
| M8 Teamwork | `backend/multiagent/{teamwork,team,goal}`、`middle/view/board`、`wiring/registry` | 先等 teamwork 契约迁移 |
| M9 Role/WorkUnit | `backend/multiagent/{subagent,agentsession}`、`backend/workunit`、`backend/tool/worktree`、`backend/engine/assemble` | 子代理恢复和索引 |
| M10 Facade | 全部 22 个生产包 | 最后一批，不应与领域迁移混提交 |

### 6.3 最容易漏测的边界

- `wiring/adapters` 同时适配多个领域，最容易通过类型 alias 掩盖身份漂移。
- `backend/engine/assemble` 是 Node、Teamwork、Context、Board 的汇合点。
- `backend/engine/context/seelectx` 同时消费 Event、CompactFrame、EvidenceRef 和 SessionContextRecord。
- `middle/view/board/boardtest` 的内存实现与真实存储必须共享同一端口，不能各自漂移。
- `backend/multiagent/teamwork` 当前存在并行契约迁移，不能在本拆分中顺手改写。

## 7. 下一条会话的验收格式

开发会话最终必须使用以下结构回复。五类状态信息为必填项，不能省略：

- 已完成项
- 正在完成项
- 未完成项
- 未决风险
- 下一步内容

即使某项为空，也必须写“无”并说明判定依据。不得把“未开始”写成“正在完成”，
不得把“跳过/未跑”写成“已完成”。

```text
结论：M0 完成 / M0 阻塞

已完成项：
- ...

正在完成项：
- ...；若会话结束时无持续执行任务，写“无（当前无后台任务/无未收敛操作）”

未完成项：
- ...；包括本里程碑未交付内容和明确推迟到后续里程碑的事项

未决风险：
- 必须列出风险、影响范围、当前缓解措施、阻塞点或需要谁裁决
- 没有已知风险时写“无；依据：<已运行的测试/检查>”

改动：
- ...

红测：
- 命令：
- 首次失败：
- 修复后结果：

结构证据：
- Router: X -> Y
- jsonRepository: X -> Y
- storeEngine: X -> Y
- 根 store 生产 importer: 91 files / 22 packages
- 根 store 测试 importer: 135 files / 21 packages

外部影响：
- 已检查：
- 未检查：
- worktree / branch：
- 清单：
- 依赖/反向依赖：
- checkpoint：
- 脚本与 dry-run：
- 批次提交与回滚：

数据兼容：
- 路径：
- JSON/schema：
- head/watermark：
- 用户数据：

回滚：
- 反向补丁范围：
- 是否改生产代码：
- 是否触碰用户数据：

Token / 成本：
- 预估：
- 实际：
- 偏差与原因：

下一步内容：
- 必须给出下一步里程碑或具体动作、入口文件和首次验证命令
- 默认写“等待 M0 review；未进入 M1”
```
