# 服务边界、性能观测与 SeelexDot 接口工作包

> 状态：当前架构快照 + 后续开发规划。除本文档集与验证门禁外，未实施新的服务拆分或 Dot 生产能力。

日期：2026-10-28
基线：`c88b1b5c` 之上的当前工作树；仓库仍有未提交迁移收尾项。
事实口径：`〔当前〕` 表示当前源码/测试可核实；`〔实测〕` 表示本轮实际执行过；
`〔待测〕` 表示尚未取得可复现数据；`〔提案〕` 表示尚未实施和裁决。

## 1. 为什么需要这份规划

Seelex 已经完成从集中式 `seelebridge` 到
`application / backend / kernel / middle / contract / store` 的物理分层，但下一阶段若直接
“按 package 拆服务”，会同时制造以下问题：

1. 把会话状态、锁和事件顺序复制到多个进程，破坏当前 data root 单写者语义。
2. 在没有基准数据前拆进程，只把内存和 CPU 分摊开，无法判断是否真的解决了热点。
3. `RuntimePort` 含 channel、回调和会话级内存所有权，不是一个可直接远程化的 wire 契约。
4. SeelexDot 尚无代码实体；若直接以完整 Runtime 为接口，会把所有内部耦合永久协议化。

因此本工作包的结论是：**先模块化单体硬化，再做可控执行面拆进程，最后有条件引入
SeelexDot**。拆分单位是服务边界，不是 Go package。

## 2. 文档导航

| 文档 | 内容 |
|---|---|
| [`architecture.md`](architecture.md) | 当前分层、权威状态、关键流、规模、已知热点与目标服务边界。 |
| [`development-plan.md`](development-plan.md) | S0–S5 里程碑、依赖、验收、回退、删除证据和当前进度。 |
| [`dot-interface.md`](dot-interface.md) | SeelexDot 的窄 Go 端口、wire 信封、MCP binding、安全与一致性语义。 |
| [`architecture.dot`](architecture.dot) | 可由 Graphviz 渲染的完整依赖/部署图源文件。 |

## 3. 当前规模快照〔实测〕

测量范围为当前工作树，排除 `vendor/`、`dist/`、`tmp/`、`.tmp/`、`.gocache/`、
`.seelex/`，并按生产文件与测试文件分开统计：

| 指标 | 当前值 |
|---|---:|
| `go list ./...` 包数 | 160 |
| 生产 Go 文件 | 858 |
| 生产 Go 行数 | 147,296 |
| 测试 Go 文件 | 1,094 |
| 测试 Go 行数 | 172,144 |

规模最大的生产包（按当前目录聚合）：

| 包 | 生产行数 | 主要风险 |
|---|---:|---|
| `store` | 17,646 | head 重写、fsync、模块锁与读写竞争 |
| `backend/engine/assemble` | 9,200 | 上游适配与宿主转发仍集中 |
| `backend/tool/registry` | 7,135 | 工具路由、子进程和异步作业 |
| `application/assembly` | 6,053 | 跨域事务与锁序 |
| `backend/tool/computer` | 5,381 | 桌面控制、截图和跨平台差异 |
| `backend/multiagent/teamwork` | 5,124 | 团队计划、作业和看板投影 |
| `kernel/task` | 4,951 | Task/Plan/transcript/checkpoint 权威状态 |
| `internal/host` | 4,030 | 端口适配面宽，需继续按消费者收窄 |

这些数字只用于确定观测顺序，不等于瓶颈排行。

## 4. 本轮验证基线

原始全量命令：

```text
go test ./... -count=1 -timeout=120s
```

首次全量运行暴露了迁移后文档/路径门禁、`.tmp` 扫描污染、两个长包在并行负载下超时、
一个结果引用测试使用旧引用形状，以及一次 store 锁热点预算超线。本轮已修正不影响业务
语义的文档路径、测试引用和扫描盲区；隔离复跑结果记入
[`development-plan.md`](development-plan.md) 的进度表。

最终在 160 包工作树以 `-p 1` 串行复跑 `go test ./... -count=1 -timeout=5m`，全部通过。
串行不是掩盖失败，而是把共享 Go build/cache 的机器资源竞争与代码失败分开；并行全量
下 `assemble`、`worktree`、`store` 与 `test` 的耗时仍应进入 S0 的 CI 分片与资源基线。

## 5. 已裁决方向

| 决策 | 结论 | 原因 |
|---|---|---|
| 是否按 package 拆进程 | 否 | 无状态 owner、锁序和 channel 端口不能直接远程化。 |
| 主状态面 | 保持模块化单体 | Application Snapshot、EventHub、Session state 与 data root 需要单写者语义。 |
| 第一个候选服务 | Tool/Job 执行面 | 最适合故障隔离、资源限额和独立重启，且不迁移会话事实所有权。 |
| SeelexDot 定位 | 执行节点/remote workunit 实现 | 各自持有 data root；主实例只做控制与汇总。 |
| Dot 的协议基础 | transport-neutral A2A 信封 + MCP binding | 复用 Seelex A2A 协议语义；MCP 只是可替换 transport。 |
| 性能优化前提 | 先有跨边界基线 | 没有 TTFT、Snapshot、commit、锁等待和队列数据，不先猜优化项。 |

## 6. 非目标

- 本工作包不重写 Seele 的 ReAct、jobs、workplan、MCP 或 accountpool。
- 不把 `RuntimePort` 整体远程化。
- 不让 Dot 直接共享主实例 data root。
- 不引入第二份会话、权限、恢复说明或状态判定。
- 不在没有产品裁决前把 `seelexboss` 当作独立组织级控制产品。

## 7. 安全边界

- 所有新增文档不得包含真实 API key、token、password、DSN 或个人绝对路径。
- Dot 的远端能力不能只靠“工具是否注册”限制；远端凭据必须按分支/目录/操作最小授权。
- 跨进程取消使用显式消息、deadline 和租约，不依赖 `context.Context` 跨机传播。
- 数据根、`.seelex/`、`dist/`、`config/` 的任何清理都必须遵守根 `MEMORY.md` 的预警、
  进程检查、备份与确认流程。
