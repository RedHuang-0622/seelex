# 提示词：应用层业务接线（按 R1 → R2 → R3 → R4）

> 用法：基建完成后的应用层接线提示词，喂给下一个实现 agent。开工前先读
> `AGENTS.md`、根 `MEMORY.md`、`my_design.md`（§2.0/§3/§4/§5.7/§8.3）、
> `prompt.md`（R1–R4 基建口径）与
> `docs/research/2026-09-10-collaborative-doc-session-model.md`。
>
> 编号沿用 `prompt.md` §0：**R1** 后端收口、**R2** goal/TL 独立会话、
> **R3** 中间缓存、**R4** 群聊/角色 draft/顺序/定时插话。

## 0. 边界与总原则（AGENTS 铁律）

1. 应用层只经端口/DTO/Event 消费存储：端口在 `application/contract/ports.go`，
   DTO 在 `application/contract/dto`；`tui/`、`gui/` 只消费 Application API/Event，
   不得直连 sessionstore 或读文件。
2. 名称只用于展示；`session_id`/`role_session_id` 仍是唯一键。
3. 会话读写携带显式 project scope；禁止回退活跃写作用域；禁止跨会话 RawHistory
   回退（I22/I25）。
4. 存储写入只经 ports；应用层不得直接调 sessionstore 内部方法或文件。
5. 先补契约测试再接实现；退役不留兼容；字段无处安放就停下上报。
6. 危险操作按 `MEMORY.md` 中文预警 + 备份 + 等确认；不 commit/push 除非明确要求。

## R1 应用层接线：后端实现退役（保留枚举 + 显式错误）

### 目标

旧 SQLite/PostgreSQL/Redis 实现删除后，应用层必须**明确失败、可提示、由用户手动
改配置**；不得静默切换后端、不得自动改写配置、不得触碰旧数据。

### 端口与类型

| 端口/类型 | 位置 | 职责 |
|---|---|---|
| `StorageConfigPort`（既有） | `application/contract/ports.go` | 读写/测试存储配置 |
| 退役错误映射 | `application/contract` 或 core 错误层 | `ErrBackendRetired` → 可展示错误（含 backend 名） |
| `StorageStatusDTO` | `application/contract/dto` | backend/path/状态/错误/可用性 |
| Event `storage.backend_unavailable` | `application/event` | 设置页/启动失败提示 |

### 接线点

1. `internal/adapters`：`ConfigureStorage`/`TestStorage` 捕获退役错误并类型化返回；
   不回退 JSON、不改写 config。
2. `application/core` storage 用例：转成 `StorageStatusDTO` + 事件，文案含
   “该后端实现已退役，请改回 JSON 或等待新实现”。
3. `main.go` 启动：配置指向退役后端 → 启动失败并给可操作错误；不默认回退。
4. GUI/TUI：设置页展示错误；不提供“一键切换并删数据”。
5. 日志脱敏：不打印 DSN/密钥。

### 验收

- T-APP-R1-01：退役后端 → 显式错误、配置未改写；
- T-APP-R1-02：启动即失败且有提示（无静默回退）；
- T-APP-R1-03：GUI/TUI 能展示 `storage.backend_unavailable`；
- T-APP-R1-04：旧数据目录不被删除/移动。

## R2 应用层接线：goal / TechLeader 独立角色会话

### 目标

TechLeader 从 stateless completer + 同步注入主会话，升级为独立角色会话
（`role_session_id`），具备备份/冷恢复、`join_seq_id`、`compact_ref`；b 的回合
不再同步占用主会话写路径（顺序与提交交给 R4 sequencer；R2 先立端口与生命周期）。

### 端口与类型

| 端口/类型 | 位置 | 职责 |
|---|---|---|
| `RoleSessionPort` | `application/contract/ports.go` | Spawn/Archive/Status；join_seq 入参 |
| `RoleSessionDTO` | `application/contract/dto` | role_name/role_session_id/join_seq_id/status/joined_at/compact_ref |
| `TLSessionPort`（可并入上者） | 同上 | TL 回合执行（替换 stateless completer 直连） |
| Event `role.online/offline/status_changed` | `application/event` | Agent Team 面板 |
| `GoalDirectiveDTO` 扩展 | `application/contract/dto` | role_name/role_session_id/round_id/corr_id/commit_id |

### 接线点

1. `seelebridge/runtime_goal_tl.go`：b 改为经 RoleSessionPort 的真实角色会话
   （复用 subagent 会话机制或新 role 工厂）。
2. `application/core/goal`：Begin/Update/Finish 产生 role 上线/状态事件；
   终态映射（verdict_done/escalate_human/终态 gate）触发归档或保留备份。
3. `application/core/chat`：收窄“回合边界把 TL 指令同步注入主会话”的路径；
   b 的正文/备份走角色会话，主会话只收 sequencer 同步的群聊行（R4）。
4. goal stack 条目 payload：`role_name` + `role_session_id` + message 锚。
5. `compact_ref`：main 帧发布后写各角色会话（frame_id + applied_seq）。
6. 冷恢复：角色会话按“自身备份 + WAL + compact_ref + seq > join_seq_id”
   （与 my_design §5.7 一致）。

### 验收

- T-APP-R2-01：新建 goal → 创建 TL 会话 → role.online → 面板显示 online；
- T-APP-R2-02：b 回合不触碰主会话写锁/同步注入路径；
- T-APP-R2-03：goal stack 条目带 role_name/role_session_id + 锚；
- T-APP-R2-04：TL 冷恢复按 §5.7 成立；
- T-APP-R2-05：TL 终态后主会话可读、TL 备份/引用不丢。

## R3 应用层接线：会话中间缓存（物化视图）

### 目标

读路径优先 cache hit（不扫全量行/不重复装配），写路径 cache 与 storage 同步
append-only；缓存 = 实际发送出去的上下文（含 compact），按角色会话分域；
未命中必须能完全从存储派生（缓存不是第二事实源）。

### 端口与类型

| 端口/类型 | 位置 | 职责 |
|---|---|---|
| `SessionViewCachePort` | `application/contract/ports.go` | Get/Put/Invalidate(session, role_session)；含 applied_seq/prefix_digest |
| `AssemblyPort` | 同上 | `AssembleRoleWire`：按 §5.7 冷/热装配 |
| `SessionSwitchPort` | 同上 | hot attach / cold load / 原子 swap / 运行中切换 |
| `CacheMetricsDTO` | `application/contract/dto` | hit/miss、bytes、evictions、rebuilds |
| Event `session.view_ready` | `application/event` | 原子切换完成后前端才显示新视图 |

### 接线点

1. `application/core/session_runtime`：冷加载按 §5.7.1（最后一步原子 swap）；
   热切换按 §5.7.2 cache hit；A 在跑切 B/C 再切回按 §5.7.3（不中断 A、不串内容）。
2. `application/core/chat` + `seelexctx`：装配输入改为 role_wire
   （compact + tail + 自身 pending draft + K attempts），不读 RawHistory/全量 message。
3. 生命周期钩子：chat start、turn end（sync）、切换、compact 完成（失效旧视图）、
   LRU/删除（失效）、退出（close）。
4. 失效键：session_id + role_session_id + head.commit_id/revision +
   compact.frame_id/applied_seq + floor；缓存只读、深拷贝给消费方。
5. 内存上限按 §11 配置；命中率/淘汰经 CacheMetricsDTO 暴露；超限 LRU 淘汰重建。

### 验收

- T-APP-R3-01：冷启动 miss → 按 §5.7 装配 → 与纯存储派生逐字节一致；
- T-APP-R3-02：hit 路径不读全量分片、不重复装配；
- T-APP-R3-03：compact/head/floor 变化后旧缓存失效；
- T-APP-R3-04：`session.view_ready` 前保持旧视图，不出现跨会话内容；
- T-APP-R3-05：缓存清空/崩溃后可完整恢复（I22/I25）。

## R4 应用层接线：群聊、角色 draft、顺序与定时插话

### 目标

message = append-only 在线文档；user/main/tl/agent-team 走统一
`role draft → sequencer → message`；同步后即删 draft；顺序支持 goal 固定循环与
user+main 编排；定时 agent 单独触发不按顺序；前端右侧栏「状态 → Agent Team」
维护团队/在线状态/顺序栏/定时任务。

### 端口与类型

| 端口/类型 | 位置 | 职责 |
|---|---|---|
| `RoleDraftPort` | `application/contract/ports.go` | Append/Load/DeleteAfterSync；每角色独立锁/actor |
| `SequencerPort` | 同上 | Submit/SyncOnce/Floor；排序 round→role→unit；分配全局 seq |
| `GroupOrderPort` | 同上 | 读写 `order_policy/order_roles`（main 编排） |
| `SchedulePort` | 同上 | 注册/取消定时 agent；timer 在应用调度器；schedule.* EVENT |
| `RolePresencePort` | 同上 | 在线状态/floor → 事件 |
| DTO：AgentTeamDTO/RoleMemberDTO/OrderEntryDTO/ScheduledAgentDTO | `application/contract/dto` | 前端 Agent Team 页 |
| Event：role.*/floor.changed/order.updated/schedule.fired | `application/event` | 驱动面板与顺序/定时 UI |

### 接线点

1. 角色 actor 注册表：user/main/tl/agent-team 各 actor + 独立 draft 锁；
   actor 只写自己的 draft，不触 message 锁。
2. Sequencer：单写者；消费“可同步”通知；按顺序键批量 sync；
   sync 后删 draft、更新 floor、回写 goal stack 锚与 goal.* EVENT。
3. 顺序：goal 阶段固定 `user→main↔tl` 直到 TL 终态；agent-team 阶段读
   `order_policy/order_roles`（main 编排；前端只更新该字段）。
4. 定时插话：SchedulePort 到点产生 op（role draft），sequencer 在下一
   `unit_seq` 边界插入；不阻塞其他 actor、不绕过 sequencer。
5. 前端：Agent Team 成员/在线/floor；新建 goal→新建 TL→online；
   工作顺序栏（main 编排）；定时 agent 单独分区；角色区分展示 + message 角色归属。
6. 装配：按 my_design §5.7.4 分域；user draft→输入框；main/tl/agent-team→各自 wire；
   join_seq_id/compact_ref 驱动恢复。

### 验收

- T-APP-R4-01：四类角色同路径，无专属旁路；
- T-APP-R4-02：同步后 draft 即删；半同步按 commit_id 幂等；
- T-APP-R4-03：goal_loop 顺序/TL 终止正确；user_main_decided 按 order_roles；
- T-APP-R4-04：定时 agent 在下一 unit 边界插入且不阻塞；
- T-APP-R4-05：Agent Team 面板显示在线/floor/顺序/定时分区；
- T-APP-R4-06：join_seq_id/compact_ref 驱动角色恢复与装配（复用 T-ASM）。

## 5. 横切接线（四阶段共用）

| 项 | 要求 |
|---|---|
| DTO/Event 契约 | 先改 `application/contract`（DTO 纯数据、方法转自由函数），再改前端；字段同步文档/测试 |
| 错误分类 | retired backend、role/session not found、compact_ref missing、draft corrupt 等类型化错误，不吞错 |
| 幂等 | sync/commit 使用调用方逻辑操作 commit_id；禁止随机/常量；读侧条目键去重、高 revision 胜出 |
| 事务与顺序 | draft append → sync（message head 发布）→ 删 draft → floor/缓存更新；不得反向 |
| 可观测 | cache hit/miss、draft bytes、sync batch、floor transitions、schedule fires、role online |
| 配置 | backend（退役显式错误）、groups.order_mode/max_roles、schedule.enabled/catch_up、draft.delete_after_sync、缓存上限 |
| 迁移 | 旧 `.seelex` 只读跳过不迁移；旧配置读退役后端 → 启动错误 + 指引；不自动改写 |
| 特性开关 | 群聊/缓存按会话灰度；关闭时退回当前 JSON 单写路径，不回退旧布局 |

## 6. 每阶段施工纪律

1. 先写失败测试（T-APP-* + 存储 T-*）再接线；
2. gofmt + go build ./...（含 gui tags）+ go vet + 目标包测试 + go test -race；
   同轮 A/B 对比（本机抖动不可比）；
3. 新语义先登记 my_design §4 术语表（D-A2A 编号）再补 T-*；
4. 回填 conformance-checklist §6/§7 或工作包 README；
5. 不 commit/push 除非用户明确要求。

## 7. 执行顺序与依赖

```text
编号顺序（本文件叙述）：R1 → R2 → R3 → R4
依赖顺序（建议实施）：R1 → R2 → R4（角色/draft/sequencer/floor 端口）→ R3（缓存）
  · R3 的缓存键/失效依赖 R4 的 role_session_id、floor、draft/sync 语义；
  · 若先接 R3，需先冻结 R4 端口/DTO 契约（可先空实现 + 契约测试）。
```

## 8. 完成定义

- R1：退役后端错误全链路可达（启动/设置/测试/前端），无静默回退；
- R2：TL 是真角色会话，online/锚/compact_ref/冷恢复可用；
- R3：cache hit 与纯存储派生输出一致，失效与原子切换可验证，内存有上限；
- R4：群聊统一 draft→sequencer→message；顺序/定时/角色区分/恢复可测；
- 文档一致：my_design（权威）+ prompt.md（基建）+ 本文件（应用接线）+
  application 模块 README。

## 9. 待用户点

1. R3 与 R4 的先后：是否接受“R1→R2→R4 接口→R3”的依赖顺序？
2. 群聊特性开关默认值：新会话默认开启，还是按会话灰度？
3. 定时 agent 时间源与重启补发默认（本地时钟 + catch_up=true 是否可接受）？
