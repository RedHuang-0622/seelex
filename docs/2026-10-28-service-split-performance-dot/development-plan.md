# 服务拆分与性能观测开发规划

> 状态：规划。里程碑按退出条件推进，不按日期承诺；未完成项不得写成已实现。

## 1. 开发原则

1. **先契约、后实现**：先定义消费方窄端口和 wire DTO，再选 transport。
2. **先进程内双实现一致，再拆进程**：同一契约在 in-process fake 与真实 transport 上跑同一组
   一致性用例。
3. **状态不搬家**：Snapshot、Session、EventHub、Store 权威状态留在核心；执行面只拿任务输入，
   回传事件和制品引用。
4. **一份判据一处实现**：超时、取消、租约、恢复、权限与错误分类不得在核心和节点各写一套。
5. **删除清单是交付物**：每批迁移列出“旧实现 → 新唯一位置 → 归零命令”。
6. **先红后绿**：新增门禁先证明旧行为会失败，再实现；不能只靠现有测试仍绿。
7. **性能由证据驱动**：每个优化项必须有基线、变更、同剧本复测和方差说明。
8. **失败可回退**：先保留同契约的进程内实现；关闭服务开关即回退，不迁移或改写用户数据。

## 2. 里程碑总览

| 里程碑 | 状态 | 目标 | 主要退出条件 |
|---|---|---|---|
| S0 基线、README 与观测契约 | 进行中 | 文档与性能信号可复现 | 所有包 README、全量测试分组稳定、指标命名与 span 级联表落定 |
| S1 端口与 wire 契约 | 待开始 | 定义 Exec/Dot 信封与消费者端口 | 契约包零依赖；fake transport 一致性用例先红后绿 |
| S2 进程内 Exec Plane | 待开始 | 在单进程内按服务边界隔离工具/Job 执行 | in-process transport 通过；核心不再直接持有执行期临时状态 |
| S3 独立 Exec Node | 待开始 | stdio/pipe 或 loopback 进程外执行 | 崩溃隔离、取消、租约、重启对账、trace 贯通 |
| S4 SeelexDot MVP | 有条件开始 | 远端 workunit 执行节点 | 身份/凭据/审计/独立 data root/制品回传通过 |
| S5 性能专项与运营化 | 并行分片 | 消除已证实热点并建立 SLO | 同剧本 p95/p99 改善且内存/写入无回归 |

## 3. S0：基线、README 与观测契约

### 3.1 交付

- 160 个 Go 包的 README 逐包核实，至少覆盖当前 owner、依赖、Review 与验证。
- 固化测试基线，区分“全量并行资源性超时”和“真实失败”。
- 定义统一 span/metric 名称、标签白名单和采样规则。
- 为关键边界补无侵入计数器：
  - `turn.submit`、`turn.queue_wait`、`turn.first_token`、`turn.complete`
  - `context.build`、`context.compact`
  - `snapshot.clone`、`event.publish`
  - `tool.execute`、`job.execute`、`process.spawn`
  - `store.commit`、`store.head.write`、`store.fsync`
  - `mcp.call`、`mcp.breaker_rewrite`
- 建立基准剧本 S1–S5（长会话、多会话浸泡、后台扇出、MCP 抖动、冷恢复）。

### 3.2 指标契约

| 指标名 | 类型 | 单位 | 标签 |
|---|---|---|---|
| `seelex.turn.duration` | histogram | ms | frontend、effort、outcome |
| `seelex.turn.ttft` | histogram | ms | provider、model |
| `seelex.context.build.duration` | histogram | ms | mode、compacted |
| `seelex.snapshot.clone.duration` | histogram | ms | resident、session_size_bucket |
| `seelex.event.publish.duration` | histogram | ms | kind、subscribers_bucket |
| `seelex.tool.execute.duration` | histogram | ms | tool、outcome、transport |
| `seelex.store.commit.duration` | histogram | ms | backend_kind、shard_bucket |
| `seelex.store.fsync.count` | counter | count | channel |
| `seelex.queue.depth` | gauge | count | queue |
| `seelex.jobs.inflight` | gauge | count | kind |
| `seelex.dot.dispatch.duration` | histogram | ms | node、job_kind、outcome |

标签禁止包含会话正文、工具原始参数、用户名、绝对路径、密钥或完整会话 ID。需要关联时使用
稳定哈希或 trace/span ID。

### 3.3 退出条件

- `go test ./... -count=1` 在可控并行度下结果稳定；若仍受资源影响，提供串行和分组命令并
  记录原因，不能只报告“偶发失败”。
- README 链接与包覆盖门禁通过。
- pprof 可抓到 CPU、heap、mutex、block；至少跑完一轮 S1 和 S5 基线并存档摘要。
- 观测实现不新增控制流依赖，采集失败不能改变业务结果。

### 3.4 回退

关闭指标 Sink 和 OTel exporter 即可回退；核心业务路径不依赖观测服务可用。

## 4. S1：Exec/Dot 端口与 wire 契约

### 4.1 先定身份与聚合

| 项 | 归属 |
|---|---|
| 一次可取消执行 | `ExecutionJob` 聚合 |
| Dot 节点 | `DotNode` 聚合，拥有能力、租约、健康状态 |
| Dot 上的一次工作 | `RemoteWorkItem`，引用主实例 WorkUnit/Session，不复制其状态 |
| 制品 | 内容寻址的 `ArtifactRef`，只传引用与摘要 |
| 事件流 | `ExecutionEvent` 只追加，带 `seq`、`correlation_id`、终态唯一性 |

### 4.2 实现顺序

1. 在 `contract/protocol` 定义版本化 JSON DTO；禁止 wire 中序列化 Go interface、channel、
   callback 或 `time.Time` 之外的实现细节。
2. 消费方定义窄 Go 端口：dispatch、observe、cancel、lease、health。
3. 写 in-process fake transport 与红测：重复投递、乱序事件、重复终态、过期租约、取消竞态。
4. 同一测试套件跑 in-process 与 file/pipe transport，确认语义一致。

### 4.3 退出条件

- 契约 import 闭包只含 stdlib。
- `go list` 门禁证明 contract 不反向依赖 Application/Backend/Store。
- 同一输入在两套 transport 产生逐字段一致的终态与错误码。
- 取消在 deadline 内传播；重复请求由 `message_id`/`idempotency_key` 去重。

## 5. S2：进程内 Exec Plane

### 5.1 范围

先选 scoped shell/job 和 computer 子集中的可控执行项，不搬 LLM 循环、上下文装配和会话状态。

### 5.2 结构迁移

```text
caller
  -> DispatchPort
  -> in-process transport
  -> execution service
       -> scoped shell / process tree
       -> job store projection
       -> event stream
```

每次迁移一个能力面；旧路径保留一层转发必须有 owner、删除里程碑和归零测试。

### 5.3 退出条件

- 工具结果、退出码、取消与错误语义和现实现逐字一致。
- 生产调用点不再直接依赖执行实现的具体类型。
- 现有异步作业的句柄命名空间、observe/fetch/kill 和 session scope 不变。
- 删除清单列出被合并的重复执行/回收/状态映射实现。

## 6. S3：独立 Exec Node

### 6.1 部署形态

首选 stdio/pipe，后续可加 loopback HTTP/SSE；每个节点无持久会话状态，输出走事件流。
主进程保留 data root 唯一写者；节点不得挂载主数据根作为可写目录。

### 6.2 故障模型

- 节点崩溃：未完成 job 记 `interrupted`，按 T-RESUME 语义恢复；不伪造成功结果。
- 主进程重启：根据 job id、节点实例 epoch 和事件水位对账。
- 网络抖动：命令幂等、事件可重放、终态 exactly-once；超出重放窗口返回显式错误。
- 租约过期：节点停止接受新工作；主进程重新调度或标记失败。

### 6.3 退出条件

| 验收 | 判据 |
|---|---|
| 隔离 | kill 节点进程不影响核心会话与 EventHub |
| 取消 | cancel → provider/工具/进程树终止有独立耗时指标 |
| 对账 | 主/节点双侧随机重启一致用例通过 |
| 资源 | CPU/内存/并发/临时目录有硬上限 |
| 可观测 | `traceparent` 贯通 core ↔ node，输出事件带 correlation ID |
| 回退 | 配置关闭时使用同契约 in-process transport |

## 7. S4：SeelexDot MVP

启动前置：S1/S3 完成；Dot 产品关系、邮箱语义、凭据模型和发布边界已裁决。

### 7.1 首批能力

- Dot 注册与能力声明。
- 单一 workunit dispatch（含 workspace/worktree 约定和最小凭据范围）。
- 事件观察、取消、租约续约、制品引用回传。
- 健康检查、节点版本与协议版本协商。
- 节点独立 data root、审计和脱敏。

### 7.2 明确不做

- 不在首版做任意工具反射暴露。
- 不允许远端回写核心 Snapshot 或 Store。
- 不把平面化权限表穿透到远端；高层只签发最小能力租约。
- 不自动发布到主分支。

## 8. S5：性能专项

每个专项必须先提交基线脚本、环境说明和原始样本，再修改实现。

| 专项 | 现状证据 | 最小改动 | 验收 |
|---|---|---|---|
| Store commit/head | 每提交 head 重写、结构性通道 fsync | 拆计数器、批量/映射方案评估 | commit p99 与写字节下降，崩溃语义不变 |
| Snapshot/projection | 全量深拷贝与运行投影重建 | 引入共享不可变段或按 revision 差量 | GUI 事件后 CPU/分配下降，Snapshot 等价 |
| Context/token | 全量装配与估算 | 缓存稳定段、增量 token 计数 | TTFT/回合耗时改善，前缀字节不漂移 |
| EventHub | 序列化与订阅者锁内投递 | 预编码/分桶/有界队列 | publish p99 与 resync 率改善 |
| MCP trace | 全量 JSON 重写 | append-only JSONL + 有界折叠 | 写放大与内存下降，追读语义一致 |
| 子进程 | 进程树终止与平台差异 | 指标化 spawn/cancel/残留 | 取消延迟与残留率达标 |

性能门槛使用相对基线，例如同剧本 p95 不得恶化超过 5%，目标优化项必须给出方差和重复轮次；
绝对阈值只在硬件与数据规模固定后进入门禁。

## 9. 当前进度

| 项 | 状态 | 证据 |
|---|---|---|
| 最终全量测试 | 通过 | `go test ./... -count=1 -timeout=5m -p 1`，当前 160 包全部通过 |
| 构建 | 通过 | `go build ./...` 与 `go build -tags "gui,desktop,production" ./...` |
| 静态检查 | 通过 | `go vet ./...`；`gofmt -l` 对仓库源码无输出 |
| 前端测试 | 通过 | `node --test gui/frontend/dist/*.test.mjs`：874/874 |
| README 逐包核实 | 已完成（索引/链接/图门禁） | 生成器刷新 190 个 README；包覆盖、相对链接与 Mermaid 检查通过 |
| 迁移路径/扫描污染修正 | 已完成 | 隔离复跑 boot/host/plugin/TUI/DTO 门禁 |
| Store 锁热点 | 隔离 3/3 通过，最终全量通过 | 仍需加入负载标签并在专项中稳定化 |
| assemble/worktree 超时 | 隔离与最终全量均通过 | 并行包调度下仍需分组或提高资源预算 |
| 性能指标实现 | 未开始 | S0 |
| Exec Node | 未开始 | S1–S3 |
| SeelexDot | 未开始 | S4，有条件 |

## 10. 风险台账

| 风险 | 后果 | 缓解 |
|---|---|---|
| 把 package 当服务 | IPC/序列化爆炸，边界更碎 | 只按状态/故障/生命周期拆 |
| 远端复制权限判据 | 两套权限漂移 | 签发最小凭据；核心保留权威判定 |
| Dot 共享 data root | 锁冲突、数据损坏 | 每节点独立 data root |
| 重试造成重复副作用 | 双执行/双发布 | idempotency key、租约 epoch、终态去重 |
| 事件无限重放 | 内存与延迟增长 | 有界 replay + 显式 cursor expiry |
| 观测标签泄密 | 敏感信息进入 telemetry | 标签白名单、脱敏测试 |
| 迁移期双实现 | 永久兼容层 | 每层死亡条件与归零门禁 |
