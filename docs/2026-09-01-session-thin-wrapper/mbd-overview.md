# 会话薄封装 MBD 总览（Model-Based Development Overview）

> 日期: 2026-09-01 | 状态: 定稿
> 依据: [thin-wrapper-session-design.md](./thin-wrapper-session-design.md)
> 配套: [mbd-us.md](./mbd-us.md)（用例建模）、[mbd-models.md](./mbd-models.md)
>       （数学模型：组成部分/轨迹投影/视图切换/多会话并行/trace 注入与结果返回）

## 0. MBD 原则

模型先行 → 实现跟随 → 测试映射模型不变量。每个模型给出：
**符号 → 状态空间 → 转移/函数 → 不变量 → 对应测试用例**。

## 1. 阶段划分（9.x 里程碑）

| 阶段 | 目标 | 模型依据 | 门禁 |
|---|---|---|---|
| 9.1 | session/ 端口契约 + SessionUnit 骨架 | §M2（组成部分） | 编译期接口断言 + 全量绿 |
| 9.2 | runChat 委托 Seele loop（hooks 投影） | §M1（trace 注入/结果返回）、§M3（轨迹投影） | 事件指纹回归 |
| 9.3 | 存储会话粒度（StorePort） | §M2（B 绑定/持久化键） | 迁移测试 + 全量绿 |
| 9.4 | main/subagent 同构 + fork 会话粒度 | §M2（K 种类）、深拷贝不变量 | fork 隔离测试 |
| 9.5 | 清理旧状态机/平行 ChatRuntime | 全部 | -race + 冒烟 |

## 2. 活动图

完整活动图集（UML 泳道 + fork/join + 判定/汇合，10 张）见
[mbd-activities.md](./mbd-activities.md)：

| 图 | 主题 | 图 | 主题 |
|---|---|---|---|
| AD-0 | MBD 阶段流程 | AD-5 | FC + 沙箱管线 |
| AD-1 | 提交/执行/持久化 | AD-6 | 会话粒度持久化 |
| AD-2 | 会话生命周期 | AD-7 | trace 注入/返回 |
| AD-3 | 视图切换 | AD-8 | 并发关闭 |
| AD-4 | subagent 并行 + merge-back | AD-9 | fork |

## 3. 模型索引

| 编号 | 模型 | 文档 |
|---|---|---|
| M1 | trace 注入与结果返回 | mbd-models.md §1 |
| M2 | session 组成部分（含 main/subagent） | mbd-models.md §2 |
| M3 | 组成部分 → 前端投影（对话 Π_conv + 轨迹 Π_traj，含一致性） | mbd-models.md §3 |
| M4 | 视图切换 | mbd-models.md §4 |
| M5 | 多会话并行 | mbd-models.md §5 |
| US | 用例建模 | mbd-us.md |

## 4. 验证映射（模型 → 测试）

测试按四维度组织（功能 F / 边界 B / 压测 P / 竞争-死锁 R），
完整清单见 [test-cases.md](./test-cases.md) §0。

| 不变量 | 测试 |
|---|---|
| M2 域不相交 | session/domain_test、S0 污染测试 |
| M4 视图不写执行 | hot_attach 快照指纹、lifecycle 测试 |
| M5 并行无串写 | 压力测试 + -race |
| M1 trace 按会话 | trace 会话过滤测试（9.2 新增） |
| M3 投影确定性 + 对话/轨迹一致性 | 前端 165 测试 + T3.7 一致性用例 + 事件指纹 |
