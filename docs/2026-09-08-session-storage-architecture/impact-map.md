# v8 会话存储改造影响地图（快照）

> 性质：一次性分析工作包（2026-09-09）。为“JSON 后端新会话默认切 v8 布局”
> 提供波及面与回归依据；与 [README.md](./README.md)（总览）和
> [my_design.md](./my_design.md)（权威明细 v8.2）同目录，随实施收敛。
>
> 快照时间：工作树 2026-09-09 02:45 前后（v8 适配仍在演进，以代码与测试
> 为最终事实源）。结论先行的核心口径：
>
> 1. 改动**物理集中在 `sessionstore` 包**：新增 v8 引擎（未提交工作树），
>    `jsonRepository` 增加布局分派，`Event` 增加三个扩展字段。
> 2. **公开 API 零签名变化**：`Repository`/`Router`/`SessionGranularStore`
>    方法集不变；v8 会话在方法内部转接，旧会话只读兼容。
> 3. 影响是**运行时语义与数据布局**层面的：新会话写 v8 目录，读路径按
>    `metadata/guide.json` 判定；调用方无需改编译期接口，但要验收
>    seq/回放、tool-result 发布点、rollout 回退与“无 provider history 时
>    全量读为空”等语义差异。

## 0. 结论速览

1. **新会话默认 v8**：`newJSONRepository` → `newJSONRepositoryWithV8(..., false)`；
   目录无 `manifest.json` 即按 v8 创建（`v8Writable`），已有 `manifest.json`
   的旧会话继续旧布局（`v8Active=false`，只读兼容）。
2. **正文事实源切换**：v8 会话中 `commit.Events` append 进 message 事件行
   （`session/message/message_*.jsonl`，head 在 `metadata/message.json`）；
   provider history 不再是 generation 快照，而是会话目录 `history.json`
   可替换缓存；state/tool-results 保留原通道 + tool-result refs 改由
   `metadata/toolresult.json` 发布。
3. **P2 rollout 通道在 v8 会话返回 `ErrRolloutUnavailable`**：上层回退旧三读，
   实际以 durable SessionRecord/事件尾窗为准——恢复路径语义需要按此验收。
4. **波及面在调用链中段收敛**：`internal/adapters`、`session`、`seelebridge`、
   `application/core/...`、`seelexctx` 只消费公开方法，编译零改动；当前
   全仓 `go build ./...` 与这些包测试绿。
5. **已发现需要人工收口的差异**（见 §6）：全量读在“仅事件行、无 history 缓存”
   时为空；`MessageCount` 仍走 manifest 读；rollout 语义回退；fork 的
   `CurrentGeneration` 返回合成串 `v8-<commitid>`；READ 与事件流的
   双轨一致性；`Event` 新字段会被读路径剥离。

## 1. 本次变更范围（sessionstore 内部）

| 文件 | 变化 |
|---|---|
| `sessionstore.go` | `Event` 新增 `commit_id`/`in_out_json`/`wire_material`
  （omitempty，旧布局不消费）；`jsonRepository` 增 `v8 *v8Store` +
  `forceLegacy`；`WriteCommit/Read/ReadRange/ReadEventTail/ReadToolResult/
  ListToolResults/CurrentGeneration/List` 加布局分派 |
| `event_range.go` | `ReadEventRange` v8 会话转 `v8ReadRows` |
| `rollout.go` | `ReadRollout` v8 会话转 `readRolloutV8`（返回
  `ErrRolloutUnavailable`） |
| `v8_*.go`（新增，未提交） | message 引擎、metadata/guide 模块 head、
  event、compact、retention/LRU、lifecycle（draft/queue）、attempt cache、
  blob、search、fork/subagent、config、R1/R2/R3 读取器、M1–M4 契约测试 |
| `sessionstore/README.md` | **尚未同步**：仍描述 manifest/generation/
  transcript.log/rollout 布局为 JSON 后端当前实现（见 §7） |

测试：`v8_m1_test.go`（T-M1-01~08）、`v8_m2_test.go`（T-R1/R2/R3 系列）、
`v8_m3_test.go`（T-LC-01~09、T-FK-01~09）、`v8_m4_test.go`
（T-WM/T-EV/T-SR/T-BL 系列），共 65 条用例；`go test ./sessionstore` 全绿。

## 2. v8 分派矩阵（公开契约不变，JSON 后端内部分流）

写路径：

| 方法 | v8 行为 |
|---|---|
| `WriteCommit` | `commit.Events` append 进 message 事件行（Seq=0 续号、
  显式 Seq 必须严格递增，≤ head 按幂等跳过）；`ProviderHistory != nil`
  时原子替换 `session/history.json`（可替换缓存）；`state.json` 沿用；
  tool results 写原文件并把 refs 发布到 `metadata/toolresult.json` |
| `WriteAtomic`/`SaveWorkspace` | 同 `WriteCommit`（history-only 兼容写） |

读路径：

| 方法 | v8 行为 |
|---|---|
| `Read` | 读 `history.json`（缺失：会话仅事件行 → 空；会话不存在 →
  ErrNotExist） |
| `ReadRange` | 优先 `history.json` 窗口；缺失回退事件行→完整单元映射 |
| `ReadEventTail` | 读全部事件行 → `selectEventTail`（完整单元语义），
  返回前剥离 v8 扩展字段 |
| `ReadEventRange` | 事件行 seq 区间 |
| `ReadToolResult`/`ListToolResults` | 读文件 + `metadata/toolresult.json`
  refs 发布点 |
| `CurrentGeneration` | 合成 `v8-<commitid>`（无真实 generation 目录） |
| `List` | 项目目录枚举：先认 `metadata/message.json` 的 v8 meta，再认
  manifest（新旧会话同目录并列可枚举） |
| `ReadRollout` | 返回 `ErrRolloutUnavailable`，上层回退 |

不涉及布局切换的通道（两种布局共用）：`state.json`（record/conversation/
context）、`framework-events.json`、project record、node session
（subagent 节点记录）、tool-results 文件本身、媒体等。

## 3. 模块级波及（消费链）

下表给出直接引用 `sessionstore` 并实际走“会话读写”路径的调用方；v8 改造后
它们**不要求改代码**，但都属于回归与手工验收范围：

| 模块/文件 | 消费点 | 需要关注的 v8 差异 |
|---|---|---|
| `main.go` | `NewRouter`/`NewNodeSessionStore`/`NewEventStore`/`SessionMetaStore` | 新会话首次写创建 v8 布局；枚举同时支持两套 |
| `session/manager.go`、`session/ports.go` | Router 装配、别名类型 | 方法不变；会话列表/加载走 v8 分派 |
| `internal/adapters/session_workspace_ports.go` | `SaveSessionSnapshot(Workspace)`/三读端口/fork 端口/rollout 回退 | 每次快照的 `ProviderHistory+Events` 都写；rollout 回退分支被触发；`CurrentGenerationWorkspace` 返回 `v8-…` |
| `seelebridge`（runtime/event sink/subagent） | `SaveCommit`、事件持久化、子代理节点记录 | v8 会话下事件流 = message 行；seq 单调性由 event sink 保证（须验收重试不重复） |
| `application/core/session_runtime` | fork 深拷贝（段落/事件区间/generation 血缘）、尾部窗口读 | 窗口读走 `history.json`；fork 血缘串语义变化 |
| `application/core/session_history.go`、`session_cold_read.go` | 冷恢复三读 | rollout 重放 ok=false → 走 record/事件尾；事件尾 = v8 行 |
| `application/core/chat`、`composer` 等 | 会话粒度端口 | 无直接存储依赖，靠适配层；回归跑通即可 |
| `seelexctx/controller.go` 等 | compact 帧 → `SessionContextRecord` | v8 compact 引擎（`v8_compact.go`）尚未接入此路径（见 §6） |
| GUI 侧 | `gui/bridge.go` 存储配置、frontend 只读 DTO | JSON schema 不变；conversation 仍读 state blob，不受布局影响 |

依赖方向无变化：以上模块仍单向依赖 `sessionstore`；v8 引擎不对外暴露
新类型，`sessionstore` 未引入新包级导出。

## 4. 语义差异清单（接收入口，建议逐条验收）

1. **事件行的 seq 是全文唯一键**：v8 提交要求同会话事件 seq 严格递增；
   `event_sink` 每会话单调即可，跨会话/重启恢复须验证重放不产生同 seq 异
   内容的行。
2. **message head 是发布点**：先 append 再原子替换 `metadata/message.json`；
   崩溃残尾在下次提交前被清理（未发布行不可见）。
3. **`commit_id` 幂等只存在于引擎层**：生产适配路径每次提交用新随机
   commit_id，重复提交去重靠 seq ≤ head 跳过；字段级幂等契约（T-M1-08）
   在引擎测试覆盖。
4. **全量读 ≠ 事件流**：`Read` 返回 `history.json` 快照；`ReadEventTail`
   返回事件行。若调用方只传 `Events` 不传 `ProviderHistory`，`Read` 为空而
   事件流非空——这是最容易被恢复/UI 读漏的差异（探针已复现）。
5. **tool result refs 发布点迁移**：legacy 在 manifest，v8 在
   `metadata/toolresult.json`；读接口不变。
6. **rollout 不可用**：v8 会话 `ReadRollout` → `ErrRolloutUnavailable`，
   P2“rollout 优先重放”自动回退；验收时确认回退路径能看到会话（依赖
   record 与事件尾，不回退到空）。
7. **generation 语义合成**：`CurrentGeneration` 返回 `v8-<commitid>`；fork
   血缘 `forked_from_generation` 只作不透明串则兼容，若解析“generation-*”
   结构会失效（当前无解析方）。
8. **读路径加锁**：v8 分派在 jsonRepository RWMutex 下执行，另持模块锁；
   注意不能持有 jsonRepository 写锁再进 v8 读路径（会自死锁），当前
   WriteCommit 分支与各读分支的锁序需要 review 清单项。
9. **布局判定**：`guide.json` 存在 = v8；`manifest.json` 存在 = legacy；
   两者都不存在 = 新会话按 v8 创建。同一会话目录不会同时存在两套正文。

## 5. 回归证据（快照时点）

已跑绿：

```text
go build ./...                                  # 全仓编译
go test ./sessionstore -count=1                 # 含 65 条 v8 契约测试
go test ./session/ ./internal/adapters/ ...     # 消费适配层
go test ./application/core/... ./seelebridge/... ./seelexctx/...
```

尚未跑（建议在实现稳定后补）：`go test ./... -count=1`、`go vet ./...`、
`node --test gui/frontend/dist/*.test.mjs`、tags 构建
`go build -tags "gui,desktop,production" ./...`。

## 6. 仍未接线 / 待收口项

1. **v8 引擎能力只接了“会话提交”一条线**：compact/R2 装配/attempt cache/
   LRU/lifecycle（draft/queue）/fork/subagent 引擎与 M1–M4 测试已就位，但
   运行期装配、压缩栈、草稿/队列、fork 深拷贝尚未切换到 v8 模块（仍走
   原 `SessionContextRecord`、state/context 通道、manifest 时代的既有流程）。
2. **`MessageCount` 未做 v8 分派**（走 manifest 读；v8 会话可能返回 0 或
   ErrNotExist）——当前无调用方依赖，但属于 API 语义空洞。
3. **R2 `wire_material`/尝试缓存未进装配链**：公开 `ReadEventTail` 仍按
   完整单元裁剪；“compact 摘要 + 尾窗 + 最近 K 条”的装配材料改造属于后续
   阶段（对应 my_design §5 契约测试已先行）。
4. **`v8_config.go` 尚未接入 config 体系**：`session_storage.*` 键
   （shard_rows、K、retention 等）仍是默认值；SQLite/PostgreSQL/Redis
   后端不在本次 v8 范围。
5. **layout 迁移工具 / verify / reconcile 未实现**：旧会话只读兼容，
   不自动改写；迁移单独方案（M5）。
6. **数据清理纪律**：任何对 `.seelex`/dist/老布局会话目录的清理必须遵循
   MEMORY.md 铁律（先查进程、备份、中文预警、确认）。

## 7. 文档同步清单

- [sessionstore/README.md](../../sessionstore/README.md)：JSON 后端章节需
  改为“新会话 v8 布局 + 旧会话 manifest 只读兼容”，补 v8 模块/文件索引与
  验证命令；当前描述（generation/transcript.log/rollout 为现行路径）已与
  代码冲突。
- [docs/README.md](../README.md) 一次性工作包索引：登记本工作包与
  impact-map。
- 依赖 v8 口径的历史文档（rollout-p2、session-order-log）需标注“JSON 新
  会话已切 v8，rollout 为旧会话兼容/非 JSON 后端通道”的状态。

## 8. Review 清单（对照 AGENTS.md §6）

- Router/Repository 读写原子性：v8 分支是否覆盖所有读方法且锁序正确。
- seq/commit_id 幂等与崩溃残尾：重试提交不重复、head 不双跳。
- 全量读（history.json）与事件流是否双写一致；空 history 缓存不误伤恢复。
- rollout 回退路径在 v8 会话下是否仍能恢复（record/事件尾兜底）。
- fork 血缘/深拷贝在 v8 会话下的 generation 串与段落语义。
- 文档是否准确区分“已实现”（v8 JSON 适配层）与“规划/未接线”（R2 装配、
  LRU/lifecycle 接入、迁移工具）。
