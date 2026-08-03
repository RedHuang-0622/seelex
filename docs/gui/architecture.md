# Seelex Agent Workbench 总体架构

> 文档状态：权威设计入口
> 当前实现：protocol v1、单 `application.Service`、单活动 Engine
> 目标架构：protocol v2、隔离的 SessionActor、Workspace/Card、generation repository 与可选 HTTP adapter

## 1. 范围与事实边界

本设计包描述 Seelex 桌面 Agent Workbench 及其共享 Application contracts。当前 Wails GUI 已实现的能力和未来能力必须明确区分，模块状态以 [`module_dotting.json`](module_dotting.json) 为准。

以下是不变量：

1. Application Core 是业务状态唯一事实源；GUI、TUI、HTTP 都是 adapter。
2. Snapshot 是权威恢复点，Event 是有序增量路径，Query Page 承载大结果。
3. 一个 session 内只有一个状态写者；跨 session 并发由有界 scheduler 控制。
4. Schema 是 JSON 契约事实源，Markdown 示例不定义字段。
5. Workspace 只接受 opaque resource ID 或经 PathGuard 处理的相对路径。
6. generation 一旦发布不可修改；`current` 只能指向完整、校验通过的 generation。

非目标：在 protocol v2 落地前让 v1 客户端猜测新事件；在 GUI 复制 Engine/Plugin/Approval 状态机；把整个 Workspace 或完整历史塞进 Snapshot；让 HTTP DTO 直接复用 Wails 绑定类型。

## 2. 系统上下文

```text
                    ┌────────────────────────────┐
                    │      Application ports     │
                    │ Snapshot/Event/Query/Action│
                    └──────────────┬─────────────┘
                                   │
       ┌──────────────┬────────────┼─────────────┬──────────────┐
       ▼              ▼            ▼             ▼              ▼
   Wails GUI        TUI       HTTP adapter   Scenario runner  composition root
       │                           │
       ▼                           ▼
 client reducer              auth/idempotency
       │                           │
       └──────────────┬────────────┘
                      ▼
        WorkbenchCoordinator + bounded scheduler
              │               │
              ▼               ▼
        SessionActor A   SessionActor B ...
          │  │  │           │  │  │
          │  │  └─ Presentation/Card
          │  └──── Workspace port
          └─────── Generation repository
                      │
                      ▼
             Seele Engine / MCP / storage
```

`main` 是唯一 composition root。高层只依赖调用方定义的 ports；实现包不得反向 import Application service。

## 3. 模块与依赖方向

模块的职责、接口、输入输出、计划路径和依赖见 [`module_dotting.json`](module_dotting.json)。依赖必须构成 DAG，自动化测试会拒绝未知模块和有向环。

关键边界：

| 边界 | 允许 | 禁止 |
|---|---|---|
| GUI → Application | 调用窄接口、消费 DTO | 持有 Engine、Session store 或 Plugin manager |
| Application → domain adapter | 调用 caller-owned port | import `workspace`、`presentation`、`gui` 具体包 |
| SessionActor → Coordinator | 发布摘要、申请 permit | actor 持全局锁执行 Engine/IO |
| Card → Workspace | opaque action + resource ID | Card 直接读取路径或执行命令 |
| HTTP → Application | 映射认证后的 action/query | 将 bearer token 或网络 headers 传入领域层 |
| Generation store → filesystem | staging、fsync、atomic replace | 原地改写已提交 generation |

## 4. 运行时数据流

### 4.1 启动与恢复

```text
load config
  → verify current generation manifest and resources
  → if invalid, scan parent/previous committed generations
  → restore Workbench shell summaries
  → open active session first, then bounded lazy-open remaining pages
  → adapters fetch Snapshot
  → subscribe from the Snapshot revision/sequence floor
```

单个 session 恢复失败只把该页标为 `error/read-only`，不能阻塞 Workbench 其他 session。

### 4.2 提交与流式事件

```text
adapter request(session_id, expected_revision, idempotency_key)
  → authenticate / authorize / validate
  → Workbench routes explicit session_id
  → SessionActor mailbox serializes the command
  → scheduler grants bounded execution permit
  → Engine callbacks become actor commands
  → actor mutates Snapshot and bumps revision
  → EventHub publishes per-scope seq/revision Event
  → client reducer merges or requests scoped Snapshot resync
```

Engine callback 不得直接并发写 Snapshot；它先进入 actor mailbox。同 session 内顺序确定，不同 session 可以并行。

### 4.3 Workspace 与 Card

```text
Agent emits card operation
  → schema + semantic limits validation
  → PresentationPort stores normalized Card
  → session event references card_id
  → client allowlisted renderer creates inert DOM
  → user action sends action_id + expected revision
  → Core resolves opaque target
  → Workspace PathGuard revalidates root and precondition
  → query/action result returns as DTO/event
```

Card 不携带任意 JavaScript、HTML 或原始可执行路径。所有 action 都经过 Core allowlist 和权限门。

### 4.4 分页查询

Workspace、历史消息和 artifacts 使用独立 Query Page。Cursor 绑定 query fingerprint、scope、revision/generation 和到期时间；客户端不得修改或从内容推导 cursor。详见 [`api/pagination.md`](api/pagination.md)。

### 4.5 证据门禁驱动的需求到 Dev 自迭代

```text
versioned sources
  → LLM candidate requirements + atomic claims
  → exact/BM25/vector/graph retrieval + receipt
  → evidence relationship assessment
  → evidence readiness gate / human review
  → requirement baseline generation
  → architecture evidence gate + allocation
  → architecture generation
  → detailed-design evidence gate + frozen contracts
  → detailed-design generation
  → traced Dev work items + implementation revision
  → schema/unit/integration/race/security/E2E evidence
  → classified feedback reopens the owning layer
```

RAG 在该流程中是受控证据获取机制，不是单纯 prompt 上下文。在线阶段使用 claim/source coverage、support、authority、applicability、freshness、trace completeness 和 unresolved contradictions 组成 `evidenceReadiness`；严格 retrieval Recall 只在离线标注集计算。向量/rerank 分数不能单独放行条目。

门禁输出 `eligible / review_required / evidence_insufficient / blocked_by_conflict`。只有 eligible 或明确人工接受/项目特有确认的对象可进入下一层；其余对象保留在 baseline 候选和范围报告，不得静默删除或通过“保留全部平台单元”扩大范围。

需求、架构和详设每层发布不可变 generation。Dev/E2E 失败必须分类为 evidence/requirement、architecture、design、implementation、test 或 environment 问题，再精确重开对应 gate；重开只发布后继版本，不覆盖已批准基线。循环受最大次数、重复失败和人工授权限制。详见 [`modules/evidence-gated-dev-loop.md`](modules/evidence-gated-dev-loop.md) 与 [`recipes/iterate-requirement-to-dev.md`](recipes/iterate-requirement-to-dev.md)。

## 5. 并发模型

| 资源 | 所有者 | 并发规则 |
|---|---|---|
| session Snapshot、PromptStack、interaction、queue | SessionActor | mailbox 单写；读返回深拷贝或不可变值 |
| Workbench open registry、active ID、summary | Coordinator | 短临界区；锁内不调用 actor/IO/emitter |
| Engine | 单个 SessionRuntime | 不跨 actor 共享；callback 转换为 actor command |
| scheduler permit | SessionScheduler | FIFO + session round-robin；取消/崩溃必须归还 |
| Workspace index/cache | Workspace service | 读可共享；写带 revision/hash precondition |
| Event subscription | 每 scope EventHub | 慢订阅者只触发该 scope resync，不阻塞其他 scope |
| generation writer | session repository | 每 session 单提交；不同 session 可并行 staging |
| requirement-to-dev iteration | DevLoop Orchestrator | 单 run 单写；claim retrieval 可有界并行，gate/baseline 使用幂等键和 CAS |

锁顺序原则：Coordinator → registry snapshot 后立即解锁；不建立 Coordinator → Actor → Workspace 的嵌套锁链。任何网络、磁盘、MCP、Engine、emitter 或注入回调都应在产品锁外执行。

## 6. Snapshot、Event 与通用结构

### 6.1 通用 envelope

所有 v2 payload 使用：

- `protocol_version`：当前固定为 `2`；不兼容时明确拒绝。
- `scope`：`workbench`、`session` 或 `workspace`。
- `revision`：该 scope 权威状态版本；只在提交状态变更后增加。
- `seq`：Event 在 scope 内的严格单调序号。
- `generation_id`：可恢复持久化基线，不等于内存 revision。
- `request_id`：调用链关联 ID，不包含用户秘密。

权威定义：[`snapshot.schema.json`](schemas/snapshot.schema.json)、[`event.schema.json`](schemas/event.schema.json)、[`page.schema.json`](schemas/page.schema.json) 和 [`error.schema.json`](schemas/error.schema.json)。

### 6.2 一致性规则

1. Snapshot 返回时记录 `revision floor`；其已包含的旧 Event 只推进 seq，不重复归并。
2. seq 缺口、未知 event、无效 payload 或无基线时，只重拉对应 scope Snapshot。
3. 同 revision 可以有多个兄弟 Event；客户端不能用 `revision <= current` 一概丢弃。
4. Query Page 固定在 cursor 内的 snapshot revision/generation，不与后续写入混页。
5. mutation 使用 `If-Match`/`expected_revision`，冲突返回 typed `409`，禁止 last-write-wins。

## 7. Generation 发布模型

generation 是一次不可变、可验证的 session checkpoint。目录建议：

```text
sessions/<session_id>/
  current                       # 只保存 generation_id
  generations/
    gen-.../
      manifest.json             # 最后写入 staging 的内容清单
      snapshot.json
      events/events-0001.jsonl
      cards/cards.jsonl
      artifacts/index.json
  staging/<random-id>/           # 未发布，崩溃后可清理
```

发布协议：

1. 在同一文件系统创建唯一 staging 目录。
2. 写入所有资源，限制路径/数量/大小，并逐文件计算 SHA-256。
3. flush 文件内容及必要目录元数据。
4. 生成 [`generation-manifest.schema.json`](schemas/generation-manifest.schema.json)，状态只能是 `committed`。
5. 重新读取并校验 manifest、资源大小和 hash。
6. 原子 rename staging 为最终 generation 目录；若目标已存在，内容必须完全一致，否则冲突。
7. 用临时指针文件 + flush + atomic replace 更新 `current`。
8. 发布成功后才向内存状态写入新的 `generation_id` 并发 Event。

失败语义：第 7 步前失败，旧 `current` 完全可用；第 7 步后进程崩溃，新 generation 已完整。恢复时不信任目录名，只接受 schema、hash、协议和父链校验通过的 manifest。

generation 不等于事务日志。正在运行的 turn 在 checkpoint 中标记 `interrupted`，恢复后进入 idle/error，不伪装成仍在执行。

回收策略：保留 current、其父链保护窗口、最近 N 个成功 generation 和运维 pin；只删除不被指针引用且超过保留期的完整目录。staging 可按 TTL 清理。

## 8. 错误与恢复

错误使用稳定 `code`，人类文本不可用于程序分支。主要类别：

| 类别 | HTTP | 恢复 |
|---|---:|---|
| validation | 400/422 | 修正请求；不改变状态 |
| authentication/authorization | 401/403 | 重新认证或申请权限 |
| not found | 404 | 刷新目标 scope |
| revision/idempotency conflict | 409/412 | 拉 Snapshot，对比后重试 |
| rate/queue limit | 429 | 遵循 `Retry-After`，保留本地草稿 |
| transient dependency | 502/503/504 | 有界退避；不能重复非幂等副作用 |
| corrupt generation | 500 + diagnosis | 回退前一个完整 generation，只读保留损坏副本 |

完整错误契约见 [`api/errors.md`](api/errors.md)。运行步骤见 [`recipes/`](recipes/)。

## 9. 安全模型

- HTTP 默认只绑定 loopback；远程暴露必须 TLS、显式认证和最小 scope。
- Bearer token 不进入 URL、日志、Snapshot、Event、generation 或示例。
- 所有 mutation 记录 request ID、主体、scope、资源和结果；敏感输入只记录 hash/摘要。
- Workspace 每次打开/执行前重新解析根、symlink/reparse 和 revision，避免 TOCTOU。
- Markdown/Card 默认转义；URL scheme、component、action、输出大小全部 allowlist。
- Cursor、resource ID 和 generation ID 都是 opaque identifier，不能被当作文件路径拼接。

## 10. 兼容、发布与演进

- protocol v1 与 v2 不在一个 reducer 中静默兼容；同一发行包中的 Core 与 embedded GUI 原子升级。
- Wails Bridge 与 HTTP adapter 共享 Application contracts，但各自拥有 transport DTO 和错误映射。
- 新增 optional 字段可向后兼容；删除/改义字段、修改 enum 或 scope 序列语义必须升级 protocol/schema version。
- 重要决策写入 [`CHANGELOG.md`](CHANGELOG.md)；操作变更必须同步 recipes；Schema 变更必须同步 examples 和契约测试。
- 旧的 [`../arch/agent-workbench-architecture.md`](../arch/agent-workbench-architecture.md) 保留方案推演价值，不再作为字段或发布语义的事实源。

## 11. 质量门禁

交付必须同时满足：

1. 所有 Schema 可编译，`$ref` 可解析。
2. 所有示例通过对应 Schema。
3. `module_dotting.json` 无重复、未知依赖或环，登记的文档存在。
4. Go build/vet/unit/race 通过；并发模块必须有取消、背压、关闭和重入测试。
5. generation 提交包含崩溃点测试；恢复包含半写、坏 hash、坏 manifest 和不兼容协议测试。
6. HTTP 包含认证、越权、分页 cursor 篡改、幂等冲突和日志脱敏测试。
