# Headless HTTP Dot 开发方案

日期：2026-10-25

状态：**方案，未实现**。本文是开发依据，不是已落地能力的事实描述。

关联背景：

- [Seelex-Dot A2A 星形拓扑头脑风暴](../2026-10-13-seelex-dot-a2a-star-topology/README.md)
- [Agent 远程协作协议调研](../research/2026-10-15-agent-remote-collaboration-protocols.md)
- [远程 Agent 节点可行性评估](../arch/remote-agent-node-feasibility.md)
- [DS-A2A 0.1 协议](../2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md)
- [DS-A2A 详细设计](../2026-09-07-seele-a2a-framework-req/ds-a2a-detailed-design.md)
- [当前 headless 模块说明](../../headless/README.md)
- [当前架构重排结果](../2026-10-15-architecture-reshape/plan.md)

---

## 1. 结论

不建议直接在 `headless/headless.go` 的现有 `/rpc` 上增加远端路由。

推荐把 `headless` 扩成两个明确分离的平面：

```text
本机诊断控制面（现状）             远程 Dot 接入面（新增）
127.0.0.1 + /rpc + /events        显式监听 + /dot/v1/inbox
无远端鉴权，仅调试                 token/TLS + 持久 inbox
保持兼容                          DS-A2A 路由信封
```

第一版只实现一个安全、可验收的纵向切片：

```text
远端 HTTP 请求
  → dot HTTP transport
  → 身份鉴权与信封校验
  → 持久化 inbox
  → 202 Accepted
  → 本地 dispatcher
  → 把内容送入目标高管/主会话
  → 记录 delivered/rejected/interrupted
```

“路由接收内容”不等于允许远端直接操作所有内部能力。第一版禁止 Dot HTTP 直接派发基层 `teammate`、直接操作 worktree、直接执行 `team_*` 或任意 RPC。远端 Dot 只能向内层会话投递受约束内容，团队内部编排仍由高管会话自己完成。

这样做有三个直接收益：

1. 远端只拿到“接收内容”这一条窄能力，不是整个 `Application`。
2. 高管会话仍是本地团队的唯一调度者，不产生第二条团队调度真相。
3. HTTP 接收、持久化和本地执行解耦，断线、重启与背压有明确语义。

---

## 2. 设计目标

### 2.1 必须达成

| ID | 目标 |
|---|---|
| G1 | `-frontend dot` 可以在独立端口启动，不改变现有 `headless` 与 GUI 的启动行为 |
| G2 | 通过 HTTP 接收符合版本化契约的路由内容 |
| G3 | 请求在返回 `202 Accepted` 前已经原子落入本地 inbox |
| G4 | 同一发送者、同一 message id 重复提交只产生一次接收事实 |
| G5 | 内容可以被路由到显式的目标会话；不能依赖“当前视图会话” |
| G6 | 发送者身份由鉴权主体决定，不能信任请求体里的 `from` |
| G7 | 进程重启后，已接收但未执行的内容可被识别；不伪造 delivered |
| G8 | 非回环监听必须启用 TLS 和鉴权；不安全模式只能在显式开发配置下使用 |
| G9 | 共享同一套契约用例：内存 transport 与 HTTP transport 对同一信封给出相同接受结果 |
| G10 | 所有接收/投递状态可查询、可审计，默认日志不打印正文 |

### 2.2 第一版不做

- 不做公开互联网 SaaS 控制面。
- 不做远端任意 RPC 代理。
- 不做远端直接创建/删除会话。
- 不做远端直接派发基层 teammate 或 Work Item。
- 不做双向消息总线、群聊或 agent 自由对话。
- 不做 assistant 完整回复的 HTTP 同步返回。
- 不做跨厂商 A2A v1.0 AgentCard/Task 完整兼容。
- 不做多主节点共识或 Raft。
- 不自动重派处于 `dispatching` 后崩溃的内容。

---

## 3. 当前事实与边界

### 3.1 当前 headless

当前 `headless` 是本地调试控制面：

- 仅绑定 `127.0.0.1`。
- 由 `SEELEX_HEADLESS_PORT` 门控。
- `/rpc` 使用位置参数数组，适合调试快照与冒烟，不适合作为稳定远端协议面。
- `/events` 只发送事件元数据，不是可靠、可重放的消息总线。
- `goal.*` 当前按 `Snapshot().Session.ID` 路由，不适合多高管会话并发接收。
- `Application` 只做窄门面转发，不拥有业务状态机。

因此不能：

- 直接把 `127.0.0.1` 改成 `0.0.0.0`；
- 直接给 `/rpc` 暴露公网；
- 把所有远程语义塞进 `headless.Application`；
- 在 `headless` 包里实现 inbox、路由判定和生命周期状态机。

### 3.2 可直接复用的资产

- `application.Service.SubmitToSession(ctx, sessionID, text)` 已经提供显式会话路由，且不依赖当前视图会话。
- `contract/` 是零依赖契约层，可承载稳定 wire DTO。
- `store/root` 已提供数据根锁、原子写、共享读和残尾处理基础。
- `store` 已有“追加 → 发布/折叠 → 读”一致模式，可复用其规则，不在 Dot 里另造另一套持久化纪律。
- `wiring/config` 是启动参数唯一归属；`wiring/bootstrap` 是装配和逆序关闭唯一归属。
- `headless` 已证明标准库 `net/http` 足够承载控制面，不需要引入 Web 框架。

### 3.3 实施前置门禁

多节点会把单机缺陷乘以节点数。集群入口正式开放前，以下单机问题必须先处理或明确限制：

- `team_close` 后仍可写回计划。
- `max_teammates` 实际按 running job 计数，而不是按不同 teammate 计数。
- `team_recover` 与 `team_items` 对终态句柄的中断判定不一致。
- 派发后计划、绑定和审计不是同一事务。
- worktree 降级到主工作区时，读面不能直观看出没有隔离。

这些问题不阻塞 `M0–M3` 的 Dot 接收纵向切片，但阻塞“多节点生产启用”验收。

---

## 4. 术语与层级

| 术语 | 定义 |
|---|---|
| Dot | 一台可被远端投递内容、运行 Seelex Application 的节点身份 |
| Dot ID | 节点稳定身份；由配置提供，不从 hostname 临时推导 |
| Dot Principal | 经过鉴权确认的调用者身份及能力范围 |
| Envelope | 一次进入 Dot 的版本化路由信封 |
| Route | 把内容送到哪种本地目标，例如主会话输入 |
| Inbox | Dot 节点本地、持久、幂等的接收事实 |
| Delivery | inbox 内容被本地会话接收的执行事实 |
| Executive Session | 高管 Dot 上负责本域编排的主会话；第一版唯一允许的内容目标 |

这里的 Dot 是“节点身份”，不是 agent 身份。一个 Dot 上可以有多个会话，但第一版外部路由只允许显式目标会话，不允许匿名广播或自动创建大量会话。

---

## 5. 方案对比

### 方案 A：直接在 `headless.go` 增加远端路由

在现有 `Server` 上增加 `/dot/...`，请求解码后直接调用 `SubmitToSession`。

| 维度 | 评价 |
|---|---|
| 耦合度 | 高：传输、鉴权、幂等、路由、应用调用全部挤在 `headless.go` |
| 内聚性 | 低：本机调试面和远端持久接入面共享一个生命周期 |
| 可测试性 | 中：HTTP 可测，但崩溃恢复和幂等只能靠 handler 临时状态 |
| 实现成本 | 低，第一阶段最快 |
| 改动面 | 小，但对现有安全边界影响大 |
| 可回滚性 | 中：一旦改造 `StartIfRequested`，容易影响既有 headless 冒烟 |

结论：适合一次性演示，不适合作为集群产品底座。

### 方案 B：契约 + 接收域 + HTTP 适配器

新增 `contract/dot`、`backend/multiagent/dot`、`store/dot` 和 `headless/dot`，由 `-frontend dot` 启动。现有 `/rpc` 保持原状。

| 维度 | 评价 |
|---|---|
| 耦合度 | 低：协议 DTO、状态机、持久化、HTTP、应用用例各自单责 |
| 内聚性 | 高：所有“接收一次”的判据集中在接收域 |
| 可测试性 | 高：domain 用 fake store；HTTP 用 fake ingress；跨 transport 共用契约用例 |
| 实现成本 | 高，但可拆成多个可验收里程碑 |
| 改动面 | 中：新增包，少量入口装配与 README |
| 可回滚性 | 高：关闭 `dot` frontend 或移除新包即可，既有 headless 零变化 |

结论：推荐方案。

### 方案 C：直接复用 A2A SDK/服务端

引入外部 A2A v1.0 服务实现，将 Seelex Dot 映射为 A2A Task/AgentCard。

| 维度 | 评价 |
|---|---|
| 耦合度 | 外部协议边界清晰，但与现有 DS-A2A 语义需要映射层 |
| 内聚性 | 取决于 SDK；本地 inbox/teamwork 仍需自做 |
| 可测试性 | 中：外部 SDK 增加版本和错误映射变量 |
| 实现成本 | 高：要同时解决内部路由与外部协议兼容 |
| 改动面 | 大：协议身份、任务模型、发现和安全面都需重新裁决 |
| 可回滚性 | 低：协议身份一旦公开，迁移成本高 |

结论：作为后续“对外开放互通”阶段，不作为内部第一版。

---

## 6. 设计模式

| 模式 | Go 实现 | 应用位置 | 理由 |
|---|---|---|---|
| Strategy | `Transport`/`ContentSink` 窄接口 | HTTP、测试内存 transport、应用会话 sink | 同一契约可跑多种 binding |
| Adapter | `application` 适配成 `ContentSink` | 组合根 | Application 不依赖 HTTP |
| Chain of Responsibility | `http.Handler` 中间件链 | recover、request id、auth、body limit、schema | 安全门禁顺序显式 |
| Factory Method | `NewServer(config, ingress)` / `NewService(deps)` | `headless/dot`、`backend/multiagent/dot` | 所有依赖显式注入 |
| State Machine | 显式 `DeliveryState` + 单一转换函数 | inbox 生命周期 | 防止多个包各判“是否已投递” |
| Repository | `InboxStore` 接口，store 实现 | `backend/multiagent/dot` 消费方声明 | domain 不依赖 JSON 文件布局 |

禁止：

- 包级可变 inbox map；
- HTTP handler 直接 import store；
- 第二份 idempotency 判据；
- 在 `headless.Application` 上继续追加几十个 Dot 方法。

---

## 7. 目标架构

```text
                          ┌──────────────────────────────┐
远端 Leader / Executive ──▶│ dot HTTP transport           │
                          │ headless/dot                 │
                          │ auth · limits · decode       │
                          └──────────────┬───────────────┘
                                         │ DotIngress
                                         ▼
                          ┌──────────────────────────────┐
                          │ application/core              │
                          │ AcceptDotMessage use case     │
                          └──────────────┬───────────────┘
                                         │
                    ┌────────────────────┼────────────────────┐
                    ▼                    ▼                    ▼
             backend/multiagent/dot   store/dot        Session ContentSink
             route + state machine    durable inbox    SubmitToSession
```

### 7.1 依赖方向

```text
headless/dot
  → contract/dot
  → (声明 DotIngress 接口)

application/core
  → contract/dot
  → backend/multiagent/dot
  → Application 自身已有的会话提交能力

backend/multiagent/dot
  → contract/dot
  → (声明 InboxStore / ContentSink 接口)

store/dot
  → contract/dot
  → store/root

wiring/bootstrap
  → 构造 store/dot、backend/multiagent/dot、application.Service、headless/dot
  → 注入并逆序关闭
```

`headless/dot` 不得 import `store`、`backend` 或具体 `application.Service`。它只声明自己消费的窄接口。

---

## 8. Wire 契约

### 8.1 协议选择

Dot HTTP 的信封不另造第二套 agent 协议，继续以 DS-A2A 为语义基线。

DS-A2A 0.2 增加两个方向：

- `route.deliver`：进入 Dot inbox 的路由内容。
- 本地状态通过 HTTP receipt/status 返回；后续集群回传阶段再增加 `route.result`。

现有 `frame.append`、`goal.*`、`advisory.*`、`gate.verdict` 语义保持不变。

HTTP 传输版本与信封版本分开：

- URL：`/dot/v1/...` 表示 HTTP binding 的第一版。
- Envelope：`v: 2` 表示 DS-A2A 0.2 语义。

不支持的 `v` 返回稳定错误码，不静默降级。

### 8.2 信封

```json
{
  "v": 2,
  "id": "msg-01JXYZ",
  "ts": "2026-10-25T10:00:00Z",
  "from": "node-leader",
  "to": "node-executive-a",
  "kind": "route.deliver",
  "seq": 0,
  "ref_seq": 0,
  "corr": "assignment-42",
  "session": {
    "main": "session-exec-a",
    "peer": "",
    "goal": ""
  },
  "payload": {
    "route": "session.input",
    "target": {
      "kind": "session",
      "session_id": "session-exec-a"
    },
    "content": {
      "media_type": "text/markdown",
      "text": "请处理附件中的模块迁移任务。"
    },
    "deadline": "2026-10-25T12:00:00Z"
  }
}
```

约束：

- `id` 是接收幂等键。
- `from` 必须等于鉴权得到的 `DotPrincipal.id`。
- `to` 必须等于本 Dot 配置的 Dot ID。
- `route.deliver` 的 `seq`、`ref_seq` 必须为 `0`；它是一次 ingress command，不是镜像水位。
- `corr` 用于关联上层 Assignment 与后续状态查询。
- `session.main` 与 `payload.target.session_id` 必须一致；不一致显式拒绝。
- `deadline` 可选；只控制本地等待，不允许无限期占用。

### 8.3 内容范围

第一版支持的 `route`：

| route | 目标 | 状态 |
|---|---|---|
| `session.input` | 显式主会话/高管会话 | 实现 |
| `role.input` | 显式角色会话 | 后续 |
| `goal.advisory` | goal/ADVISOR 受信注入区 | 后续 |
| `teammate.input` | 基层 teammate | 明确禁止 |
| `workitem.input` | Work Item | 明确禁止 |

第一版请求体只接受：

- `text/plain`
- `text/markdown`

附件只允许传递可读 artifact ref，不允许内联 base64。二进制和大对象进入共享 artifact store 或 Git，运输层只传引用。

### 8.4 HTTP API

```text
POST /dot/v1/inbox
GET  /dot/v1/inbox/{messageID}
GET  /dot/v1/card
GET  /dot/v1/readyz
```

`/healthz` 只回答进程活着；`/readyz` 额外检查：

- inbox store 可读写；
- dispatcher 已启动；
- 配置版本与 wire 版本兼容；
- 节点没有处于 draining。

`/dot/v1/card` 返回：

```json
{
  "dot_id": "node-executive-a",
  "protocol_versions": [2],
  "http_binding": "v1",
  "route_kinds": ["session.input"],
  "max_body_bytes": 1048576,
  "auth": "bearer-tls"
}
```

不接受匿名自动创建会话，也不在 card 中暴露本机路径、模型名、账号或凭据配置。

### 8.5 接受响应

首次接收：

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
```

```json
{
  "ok": true,
  "receipt": {
    "message_id": "msg-01JXYZ",
    "dot_id": "node-executive-a",
    "state": "received",
    "inbox_seq": 17,
    "duplicate": false,
    "accepted_at": "2026-10-25T10:00:00.120Z"
  }
}
```

重复接收：

```json
{
  "ok": true,
  "receipt": {
    "message_id": "msg-01JXYZ",
    "state": "delivered",
    "inbox_seq": 17,
    "duplicate": true
  }
}
```

同一 `(sender, message_id)` 携带不同内容摘要时返回 `409 idempotency_conflict`，不能把新内容静默当成旧消息。

### 8.6 状态

```text
received ──▶ dispatching ──▶ delivered
    │              │
    │              └────────▶ interrupted
    ├───────────────────────▶ rejected
    └───────────────────────▶ expired
```

定义：

| 状态 | 含义 |
|---|---|
| `received` | 已持久接收，等待本地投递 |
| `dispatching` | 已开始调用 Application；**开始后不自动重试** |
| `delivered` | Application 已接受目标会话输入 |
| `interrupted` | 进程在 dispatching 期间崩溃，结果不确定；需要人工/上层显式重投新 id |
| `rejected` | 校验或目标投递返回确定性错误 |
| `expired` | 超过 deadline 仍未开始投递 |

投递语义：

- HTTP 接收：按 `(authenticated sender, message_id)` 幂等一次。
- 本地执行：`dispatching` 之后自动重试为 0，避免在无法确认 Application 是否接受时产生重复回合。
- 不在第一版承诺跨 HTTP 与聊天队列的端到端 exactly-once。
- “无法确认”明确记为 `interrupted`，不伪造 `delivered`。

---

## 9. 本地路由

### 9.1 目标解析

`route.deliver` 只解析为受支持的本地目标：

1. 校验 `to == local dot id`。
2. 校验 `from == authenticated principal`。
3. 校验 principal scope 允许 `route.deliver` 与目标会话。
4. 校验 `session.main == payload.target.session_id`。
5. 第一版要求 `target.kind=session`。
6. 不读取 `Snapshot().Session.ID` 作为兜底。
7. 不自动创建会话；目标不存在时投递阶段返回 `target_not_found`。
8. 不允许 `main`、`user` 之外的基层角色通过第一版 Dot 接口直接接管。

### 9.2 文本包装

投递到 `SubmitToSession` 的正文必须包含稳定的来源头：

```text
[Seelex Dot]
from: node-leader
message: msg-01JXYZ
corr: assignment-42

<原始内容>
```

来源头用于审计和模型识别，不作为系统指令。远端正文仍是不可信输入，不能绕过本地权限、工作区和审批。

### 9.3 投递调用

语义等价于：

```go
err := application.SubmitToSession(ctx, targetSessionID, wrappedText)
```

不调用：

- `Submit`（避免依赖当前视图）；
- `goal.*` 当前视图路由；
- `team_*`；
- `role.*` 写面；
- `subagent.fork`；
- 任意 shell 或 RPC 透传。

---

## 10. 持久化设计

### 10.1 存储位置

新增节点级 `store/dot`，不挂到单个 session module：

```text
<data-root>/dot/<dot-id>/inbox/events.jsonl
<data-root>/dot/<dot-id>/inbox/payloads/<sha256>.json
```

理由：

- 接收事实发生在目标 session 被解析之前。
- 节点级数据与 session 生命周期解耦。
- 每个 Dot 节点有自己的 data root，符合单进程锁边界。

### 10.2 事件记录

`events.jsonl` 只追加：

```json
{"kind":"received","inbox_seq":17,"message_id":"msg-01JXYZ","sender":"node-leader","content_sha256":"...","route":"session.input","recorded_at":"..."}
{"kind":"dispatching","inbox_seq":17,"attempt":1,"recorded_at":"..."}
{"kind":"delivered","inbox_seq":17,"attempt":1,"recorded_at":"..."}
```

规则：

- `received` 写入成功后才返回 `202`。
- 状态由事件折叠，不维护第二份可写状态文件。
- 未换行残尾按既有 store 纪律忽略。
- 同 message id 的重复 `received` 不再次追加，读取已有接收事实。
- payload 按内容摘要去重；媒体正文不写日志。

### 10.3 恢复

启动时：

- `received`：可继续投递。
- `dispatching`：转为内存中的 `interrupted` 语义，**不自动重投**。
- `delivered/rejected/expired/interrupted`：终态，不回退。

如果后续需要自动恢复，必须先把 Application 输入接口升级为带稳定 delivery id 的幂等接收入口；在完成该能力前，不得以“重试一次”冒充可靠恢复。

---

## 11. 安全设计

### 11.1 监听与 TLS

- 默认 `dot` frontend 关闭。
- 默认监听 `127.0.0.1`。
- 非回环地址必须同时配置 TLS 和鉴权。
- 不提供隐式 `0.0.0.0` 快捷开关。
- 反代部署必须显式声明可信代理；第一版不信任任意 `X-Forwarded-*`。

### 11.2 身份

第一版使用 Bearer capability token：

- token 从环境变量或受限权限文件读取，不写 YAML 示例。
- token 映射为 `DotPrincipal{ID, AllowedRoutes, AllowedTargets, ExpiresAt}`。
- 请求体 `from` 只作为声明值，必须与 principal ID 一致。
- 记录/审计只保存 principal ID 和 token key id，不保存 token 原文。

后续可增加 mTLS；传输层变化不应改变 inbox 与路由语义。

### 11.3 请求防护

- `http.MaxBytesReader` 限制正文。
- 只接受 `application/json`。
- 使用 `DisallowUnknownFields`；协议扩展必须升版本。
- 限制嵌套深度、字符串长度和 `text` 字节数。
- 拒绝重复 JSON 字段。
- 不启用宽松 CORS；默认无 CORS。
- 设置请求头、读写与 idle timeout。
- 不对远端返回本机绝对路径、内部错误堆栈或配置值。
- 日志默认只记录 message id、sender、route、状态和大小。

### 11.4 权限边界

Dot HTTP 不是“远端 root”：

- 不暴露任意 `/rpc`；
- 不暴露终端；
- 不暴露 worktree 管理；
- 不暴露账户与凭据；
- 不暴露原始模型配置；
- 不允许选择任意本地路径；
- 不允许通过目标字段绕过 leader 的团队计划。

---

## 12. 并发、背压与关闭

- 接收 HTTP 路径只做鉴权、解码、持久化和入队，不等待整个 agent 回合。
- 每个目标会话最多一个 dispatcher，保序处理该会话的 inbox。
- 不同目标会话可以并行。
- 每个 Dot 有全局待投递上限和目标级待投递上限。
- 达到上限时保留已接收事实但不无限收新内容；新请求返回 `429 inbox_full`。
- dispatcher 使用 detached context，不继承已返回 HTTP 请求的取消信号。
- 单条投递受 deadline 和服务器关闭超时共同约束。
- 关闭顺序：

```text
停止接受新 HTTP
  → 停止领取新 inbox
  → 等待在飞投递到上限
  → 未开始项保留 received
  → 在飞未确认项保留 dispatching，重启后为 interrupted
  → 关闭 store
```

---

## 13. 核心接口

以下为开发约束，最终签名以实施时契约为准。

### 13.1 HTTP 消费方声明

```go
package dot

type Ingress interface {
    AcceptDotMessage(
        ctx context.Context,
        principal dto.DotPrincipal,
        envelope dto.DotEnvelope,
    ) (dto.DotReceipt, error)

    DotDeliveryStatus(
        ctx context.Context,
        principal dto.DotPrincipal,
        messageID string,
    ) (dto.DotDelivery, error)
}
```

接口定义在 `headless/dot`，由 `application.Service` 在编译期结构满足。

### 13.2 接收域端口

```go
package dot

type InboxStore interface {
    Append(ctx context.Context, record InboxRecord) error
    Load(ctx context.Context, dotID string) ([]InboxRecord, error)
    LoadPayload(ctx context.Context, contentSHA256 string) ([]byte, error)
}

type ContentSink interface {
    Deliver(ctx context.Context, request DeliveryRequest) error
}
```

接口定义在 `backend/multiagent/dot`，实现分别在 `store/dot` 与装配适配器。

### 13.3 协议 DTO

建议放到 `contract/dot`：

```go
type DotPrincipal struct {
    ID             string
    AllowedRoutes  []string
    AllowedTargets []string
    ExpiresAt      time.Time
}

type DotEnvelope struct {
    Version int             `json:"v"`
    ID      string          `json:"id"`
    Timestamp time.Time     `json:"ts"`
    From    string          `json:"from"`
    To      string          `json:"to"`
    Kind    string          `json:"kind"`
    Seq     uint64          `json:"seq"`
    RefSeq  uint64          `json:"ref_seq"`
    Corr    string          `json:"corr,omitempty"`
    Session DotSessionIDs   `json:"session"`
    Payload json.RawMessage `json:"payload"`
}

type DotReceipt struct {
    MessageID string `json:"message_id"`
    DotID     string `json:"dot_id"`
    State     string `json:"state"`
    InboxSeq  uint64 `json:"inbox_seq"`
    Duplicate bool   `json:"duplicate"`
}
```

`DotEnvelope.Payload` 在领域层按 `kind` 再解码；HTTP 层不解释路由策略。

---

## 14. 文件改动计划

### 14.1 新增

| 文件/目录 | 职责 |
|---|---|
| `contract/dot/envelope.go` | DS-A2A 0.2 `route.deliver`、receipt、status、error DTO |
| `contract/dot/envelope_test.go` | JSON golden、版本、字段基线与未知字段拒绝 |
| `backend/multiagent/dot/service.go` | 接收用例、幂等、目标校验、状态转换 |
| `backend/multiagent/dot/service_test.go` | 纯 domain 用例 |
| `backend/multiagent/dot/dispatcher.go` | 按目标会话串行、跨目标并行的投递循环 |
| `backend/multiagent/dot/dispatcher_test.go` | 背压、关闭、interrupted 语义 |
| `store/dot/inbox.go` | 节点级 append-only inbox 与 payload 存储 |
| `store/dot/inbox_test.go` | 残尾、幂等、冲突、重启折叠 |
| `headless/dot/server.go` | HTTP mux、handler、middleware、Shutdown |
| `headless/dot/auth.go` | principal 解析与 scope 校验 |
| `headless/dot/server_test.go` | HTTP 状态码、鉴权、body limit、幂等 |
| `headless/dot/README.md` | HTTP Dot 传输层说明 |
| `application/core/dot_service.go` | Application 用例门面 |
| `application/core/dot_service_test.go` | 应用用例与 Session sink 接线 |
| `wiring/adapters/dot_content_sink.go` | Application 会话提交适配 |
| `docs/arch/seelex-dot-http-v1.md` | 方案获批后固化稳定协议 |

### 14.2 修改

| 文件 | 修改 |
|---|---|
| `wiring/config/flags.go` | `dot` 启动参数与安全默认值 |
| `wiring/config/modes.go` | `ParseFrontendMode` 接受 `dot` |
| `wiring/bootstrap/frontend.go` | `dot` 前端分派 |
| `wiring/bootstrap/application.go` | 构造 dot domain/store/adapters |
| `wiring/bootstrap/bootstrap.go` | 逆序关闭 dispatcher 与 HTTP server |
| `wiring/registry/README.md` | 若新增诊断/能力探针则同步 |
| `headless/README.md` | 明确本机 RPC 与远端 Dot 的边界和启动方式 |
| `application/core/README-*.md` | 登记 dot 用例 |
| `contract/README.md` | 登记零依赖 Dot 契约 |
| `store/README.md` | 登记节点级 Dot inbox 分卷 |
| `config/README.md` | 新增非秘密配置说明 |
| `test/README.md` | 登记 Dot E2E |

### 14.3 保持不动

- 现有 `headless` 的 `/rpc`、`/events`、`SEELEX_HEADLESS_PORT` 协议。
- `gui` 内的 `StartIfRequested` 调试接线。
- 现有 `Application` 方法签名；Dot 使用独立窄接口。
- 现有 session/task/goal/teamwork 数据布局。

---

## 15. 启动配置建议

新增 `dot` frontend，但不改变 `headless`：

```text
-frontend dot
-dot-id node-executive-a
-dot-listen 127.0.0.1:39124
-dot-token-file <path>
-dot-tls-cert <path>
-dot-tls-key <path>
-dot-max-body-bytes 1048576
-dot-max-pending 256
-dot-max-pending-per-target 32
```

配置硬度：

- 🔴 `route.deliver`、`route.deliver` 的 HTTP 版本：常量，变更必须升协议版本。
- 🟠 默认监听 `127.0.0.1`、默认上限：常量 + flag 覆盖。
- 🟡 节点 ID、token 文件、TLS 文件：部署差异。
- 🟢 目标白名单、保留期、并发量：配置文件。
- 🔵 store、dispatcher、clock：构造注入。
- ⚪ 是否启用 DoT：仅 frontend 选择，不做运行期热切换。

秘密不得写入示例配置、日志或命令行明文；token 文件权限必须校验。

---

## 16. 实施里程碑

### M0：协议与边界冻结

工作：

- 决定 DS-A2A 0.2 字段。
- 决定第一版只支持 `session.input`。
- 固化错误码、HTTP 状态码、幂等键与状态词。
- 明确“Dot 不得直派基层 teammate”。
- 将本方案中的协议部分整理成稳定架构文档草案。

退出：

- 同一信封的 JSON golden 固化；
- 未知版本、未知 kind、重复 id、不同内容哈希均有预期结果；
- 无需写代码即可评审协议。

### M1：Store-first 纵向切片

工作：

- `store/dot` append-only inbox；
- payload 内容寻址；
- 重启折叠与残尾处理；
- 同 id 幂等和冲突。

退出：

- 接受后崩溃，重启仍能读到 `received`；
- 重复写入不产生第二事实；
- 半行、坏行、payload 缺失都有显式状态；
- 不触碰现有 session 数据格式。

### M2：接收域状态机

工作：

- `backend/multiagent/dot` 实现 `received → dispatching → delivered/interrupted`；
- 目标校验与 principal scope；
- 单目标串行 dispatcher；
- 背压和关闭。

退出：

- fake store/fake sink 全套通过；
- 同一 message id 并发 100 次只接收一次；
- dispatching 后崩溃不自动重投；
- 关闭时不丢未开始记录。

### M3：Application 用例

工作：

- `application/core/dot_service.go`；
- 将 `ContentSink` 接到 `SubmitToSession`；
- 显式目标会话，禁止当前视图回退；
- 编译期断言 `Application` 满足 HTTP 消费方接口。

退出：

- 内容进入指定 session，不串到当前界面会话；
- `main/user` 以外的内部能力不能经 Dot 路由；
- 目标不存在/未加载/忙时状态可解释；
- 单测 + 真实 Application 测试通过。

### M4：HTTP Dot Transport

工作：

- `headless/dot` server 与中间件链；
- `/card`、`/inbox`、`/inbox/{id}`、`/readyz`；
- 标准库 HTTP、body limit、严格 JSON；
- 独立 listener 与 shutdown。

退出：

- 所有错误映射稳定；
- 请求体不进入日志；
- 现有 headless 测试不变且全绿；
- HTTP 与内存 binding 对同一信封产生一致结果。

### M5：安全启用

工作：

- Bearer token principal；
- TLS / mTLS 配置；
- 来源与 target allowlist；
- 非回环不安全配置启动即拒绝；
- 节点 ID 不可由 hostname 漂移。

退出：

- 伪造 `from`、越权 target、过期 token、重放/超大 body 全部被拒；
- 日志和错误不泄密；
- 默认配置无远端暴露；
- 安全评审通过。

### M6：远端验收

工作：

- 两台机器/两个 data root；
- 一个 Leader 向一个 Executive 投递内容；
- Executive 本地 team 自行工作；
- Leader 查询 inbox 状态；
- 断网、节点重启、目标会话忙、重复投递实测。

退出：

- 远端只接收内容，不获得任意 RPC；
- 重启后状态不伪造；
- 同 id 重试不重复进入会话；
- 目标不可达和本地 application 失败可区分；
- 原始报告落到 `docs/test/`。

### M7：后续出站与多节点

仅在 M0–M6 稳定后进行：

- `route.result` / artifact refs；
- 出站 callback 或 SSE；
- Executive heartbeat / lease；
- Leader 侧 assignment 表；
- 与 A2A v1.0 的 AgentCard/Task 映射。

---

## 17. 测试策略

### 17.1 红灯先行

以下用例必须先写并确认在旧实现中失败：

1. 远端投递不会落到当前视图会话。
2. 同一 message id 并发投递只创建一个输入。
3. 请求体 `from` 与服务端鉴权身份不一致时拒绝。
4. `dispatching` 中崩溃后不自动产生第二个回合。
5. 非回环且无 TLS/鉴权时启动失败。
6. `teammate.input` 请求被明确拒绝，而不是被当成 `session.input`。

### 17.2 分层测试

| 层 | 用例 |
|---|---|
| `contract/dot` | JSON round-trip、版本、未知字段、必填字段、错误码词表 |
| `backend/multiagent/dot` | 幂等、冲突、状态转换、scope、deadline、状态词唯一 |
| `store/dot` | 追加、折叠、残尾、payload 缺失、重启、并发 |
| `application/core` | 显式 session 路由、不读当前视图、target not found |
| `headless/dot` | method、media type、body limit、auth、HTTP 映射 |
| cross-binding | 同一信封分别走内存与 HTTP，只比较语义结果 |
| E2E | 两节点、两 data root、真实 session、真实重启 |

### 17.3 最小命令

```text
go test ./backend/multiagent/dot/... -count=1
go test ./store/dot/... -count=1
go test ./headless/... -count=1
go test ./application/core/... -count=1
go test ./test/ -run Dot -count=1
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
```

Windows 若 `CGO_ENABLED=0`，不得声称执行过 `-race`。CI 必须补 Linux `-race`。

### 17.4 E2E 验收场景

1. Leader 向 Executive 的显式 session 投递 markdown。
2. Executive 回复，证明内容进入了目标会话而非当前界面会话。
3. 相同 HTTP body 重发 3 次，目标会话只增加一个输入。
4. 相同 message id 改正文，返回 409。
5. 目标 session 不存在，状态为 rejected/target_not_found。
6. 投递前杀进程，重启后仍可继续 `received`。
7. 投递中杀进程，重启后为 interrupted，不自动重复回合。
8. 两个目标 session 并发投递，互不串写。
9. 未授权 token、越权 target、超大正文、非 JSON、未知版本全部拒绝。

---

## 18. 可观测性

必须记录但不含正文：

- `dot_id`
- authenticated principal
- `message_id`
- `corr`
- `inbox_seq`
- route kind
- content bytes
- state transitions
- queue depth
- accepted / duplicate / rejected / delivered / interrupted 计数
- accept latency 与 delivery latency

建议事件：

```text
dot.inbox.received
dot.inbox.duplicate
dot.inbox.dispatching
dot.inbox.delivered
dot.inbox.interrupted
dot.inbox.rejected
dot.inbox.expired
```

错误返回包含稳定的 `code/message/retryable`，但不包含内部 cause、绝对路径或 token。

---

## 19. 兼容与迁移

- 现有 `headless` 零行为变化。
- 新增 `dot` 是 opt-in frontend。
- 无已有数据迁移；`store/dot` 首次启用时惰性创建。
- DS-A2A 0.1 保持可读；不升级的既有调用不受影响。
- 新 route kind 只由 Dot HTTP binding 接受。
- 若多节点试点失败，停止 `-frontend dot` 即可退回单机。
- 新增数据位于独立 `dot/` 子目录，不修改现有 session JSON v8 格式。

---

## 20. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| 把 `/rpc` 暴露成远端面 | 任意内部能力被调用 | 独立 listener、独立 route table、禁止 RPC 透传 |
| 请求体 `from` 伪造 | 身份冒用 | principal 由鉴权中间件生成并与 body 校验 |
| 不可靠重启导致重复 agent 回合 | 重复工具调用/副作用 | dispatching 后不自动重试，显式记录 interrupted |
| 高管直接向基层转发不受控任务 | 绕过 team plan、权限和隔离 | 第一版禁止 teammate/workitem target |
| 正文无界 | 内存/磁盘耗尽 | 双层 body limit、inbox quota、目标级 quota |
| 目标会话串写 | 多高管结果污染 | 显式 sessionID，禁止当前视图回退，per-target 串行 |
| 日志泄露内容或凭据 | 安全事故 | 结构化日志白名单，默认不记正文 |
| 单机状态机缺陷进入集群 | 故障按节点数放大 | 多节点启用前完成 §3.3 门禁 |
| DS-A2A 版本漂移 | 多节点互不兼容 | `/card` 协商、信封版本、Unknown 拒绝 |
| 把 mailbox 当可靠消息队列 | 停机丢消息或假 ACK | ACK 前 inbox 原子落盘；状态可查询 |

---

## 21. 待裁决项

以下问题必须在 M0 结束前拍板：

1. DS-A2A 0.2 是否正式增加 `route.deliver`，还是另开 Dot HTTP 私有协议？
2. 第一版鉴权选 Bearer+TLS，还是直接要求 mTLS？
3. token 仅环境变量，还是允许受限权限文件？
4. 第一版是否只支持 `session.input`，还是同时实现 `role.input`？
5. 目标会话不存在时，是接收时同步拒绝，还是先接收、投递时 rejected？
6. delivered 指“Application 接受了会话输入”，还是“assistant 回合完成”？

本方案的建议：

- 用 DS-A2A 0.2，不造第二套语义。
- Bearer+TLS 起步，mTLS 后置。
- token 文件与受限环境变量二选一，均不进日志。
- 第一版只支持 `session.input`。
- 先接收、后投递；目标不存在记 `rejected`，HTTP 202 表示“已可靠接收”，不表示“业务已执行”。
- delivered 只表示 Application 已接受会话输入，不等待完整 assistant 回合。

---

## 22. 循环依赖检查

```text
contract/dot
  → 只允许 stdlib
  ✗ 不得 import application/backend/store/headless

backend/multiagent/dot
  → contract/dot
  ✗ 不得 import headless/application/store 具体实现

store/dot
  → contract/dot + store/root
  ✗ 不得 import backend/application/headless

headless/dot
  → contract/dot
  ✗ 不得 import backend/store 或具体 Service

application/core
  → contract/dot + backend/multiagent/dot
  ✗ 不得 import headless

wiring/bootstrap
  → 所有实现
  ✗ 不承载业务判定
```

没有依赖环。

---

## 23. 最终验收清单

- [ ] `-frontend dot` 与 `-frontend headless` 独立共存。
- [ ] 现有 headless RPC/SSE 测试零变化通过。
- [ ] HTTP 接收前先持久化，再返回 202。
- [ ] 同一 message id 幂等；不同正文同 id 返回 409。
- [ ] 内容只进入显式目标 session。
- [ ] 远端不能透传任意 RPC。
- [ ] 远端不能绕过 leader 直派 teammate/work item。
- [ ] 重启后 received 可继续、dispatching 不自动重投。
- [ ] 非回环无 TLS/鉴权时拒绝启动。
- [ ] 正文默认不进入日志。
- [ ] 多节点 E2E 有可复跑报告。
- [ ] 模块 README、协议文档、测试资料同步。

---

## 24. 推荐的第一笔实现

只做下面这一条纵向切片，不先做多节点：

```text
POST /dot/v1/inbox
  → bearer auth
  → route.deliver + session.input
  → durable inbox
  → dispatcher
  → SubmitToSession(explicit session id)
  → status query
```

当这条链路满足“显式路由、幂等、重启不伪造、日志不泄密”后，再扩展：

```text
role.input
  → goal.advisory
  → route.result
  → heartbeat / lease
  → multi-node leader assignment
```

这个顺序能保证 Dot 首先是一条**可靠的远端内容入口**，之后才成长为一个集群控制面的外围成员。
