# 性能与可观测性审查 + 模块化多二进制目标架构（工作包）

- 日期：2026-10-15
- 状态：**提案／审查记录，不是实施计划授权，不是验收证明**。本工作包未修改任何业务代码、测试或配置；未执行 benchmark 与构建。
- 范围：沿 Application → Runtime → Tool/Job → Store/MCP/子进程关键路径的性能与可观测性审查；模块化、多二进制目标架构设计。

## 证据分级口径

| 标记 | 含义 |
|---|---|
| 〔源码〕 | 本轮以 file:line 核实的当前源码静态事实；不等于运行行为已实测。 |
| 〔待测〕 | 源码无法定论、需要 benchmark/profiling/运行曲线才能确认或证伪的假设。 |
| 〔历史基线〕 | 历史工作包的结论；其实现状态可能已被后续变更覆盖，引用时注明日期与后续状态。 |
| 〔提案〕 | 本工作包的设计建议，未经裁决与实施。 |

## 与前序工作包的关系（不重复造结论）

- [`../2026-10-14-memory-cpu-accumulation/`](../2026-10-14-memory-cpu-accumulation/README.md)：**历史基线**。其裁定（bundles 只增不删、telemetry 无界、Snapshot 深拷贝）是**修复前线索**；P0 释放面已落地，P1/P2 未开始。本包不重复报旧缺陷，只把未决项纳入基准计划。
- [`../2026-10-15-product-architecture-pre-design/global-review.md`](../2026-10-15-product-architecture-pre-design/global-review.md)：同日的有限证据全局审查。其「先模块化、再有条件拆进程，推荐混合架构」「SeelexDot 未裁决」「SeelexBoss 模型 A/B」结论与本包一致；本包在其之上补充 file:line 级调用链证据、逐二进制契约表与可复现 profiling 计划。
- [`../arch/ARCHITECTURE_REVIEW.md`](../arch/ARCHITECTURE_REVIEW.md)：历史评审，H1–H28 风险假设为历史口径。
- [`../arch/remote-agent-node-feasibility.md`](../arch/remote-agent-node-feasibility.md)：远程化阶梯 L0–L2 与本包多进程设计的直接依据。

## 文档导航

| 文档 | 内容 |
|---|---|
| [`perf-observability-review.md`](perf-observability-review.md) | 关键路径逐段审查（源码可确认问题 / 待测假设 / 历史基线三分）、可复现基准与 profiling 计划（指标定义 + 命令）、现架构可观测性补齐方案、未来跨进程 trace 传播方案、拆进程成本分析 |
| [`multi-binary-architecture.md`](multi-binary-architecture.md) | 逻辑模块图与部署进程图、候选二进制逐条契约、三种架构对比与推荐、不应拆出的模块、Seele v0.3.4 可复用组件清单、Seelex/seelexdot/seelexboss 定位备选模型 |

## 关键结论速览

1. 源码可确认的性能事实集中在六处：每轮上下文全量装配 + 启发式 token 重扫、Snapshot 全量深拷贝、runtime 投影逐边界整份重建、每次提交 module head 整份重写（C1 已知未完成项）、结构性事件通道逐写 fsync、MCP trace 整份重写。**均无量化实测，不构成瓶颈排行**。
2. 可观测性现架构可立即补齐：pprof tag 已就绪、Seele `telemetry.OTelTracer` 已存在但未接线、无 metrics Sink、日志无结构化。单进程不等于无法 tracing。
3. 多二进制推荐**混合架构**：主二进制保持模块化单体（Application/会话/数据根单写者不拆），`computer-use-mcp` 是已存在的第二进程先例，下一个候选是「可控 Tool/Job 执行路径」的远端驱动验证；不为拆而拆。
4. Seele v0.3.4 已有可复用底座（jobs/event/telemetry/accountpool/workplan/mcp/microhub/limits），其中 cache、microhub、limits、workplan codec/runtime 主体尚未被 Seelex 复用——优先复用，不重造。
5. Seelex 是产品主干与共享底座；seelexdot / seelexboss 目前**只是产品概念词汇，仓库内无代码实体**，本包给出备选模型而非定案。
