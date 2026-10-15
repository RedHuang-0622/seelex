# 子进程调用系契约（2026-09-26 工作包）

日期：2026-09-26（设计）/ 2026-10-02（打点表）
状态：**P0（§A 契约落实）与 P1（§B 契约下的实现）已落地；P2（§C 契约进 seele + loop 并发）未开工。**
来源：2026-10-15 文档治理时，由 `docs/` 根目录的三份散文件迁入本目录（内容不变，仅补状态与位置）。

## 本目录文档

| 文档 | 作用 | 状态 |
|---|---|---|
| [`tool_concurrency_design.md`](tool_concurrency_design.md) | 工具调用并发与「子进程调用系」详细设计（v3 修订） | 设计稿；P0/P1 已落地，P2 未开工 |
| [`tool_calling_step0_contracts.md`](tool_calling_step0_contracts.md) | 打点表：契约落实（P0）→ 契约下的实现（P1）→ 契约进 seele + loop 并发（P2） | P0/P1 行已勾选，P2 未开工 |
| [`subprocess_contract_testcases.md`](subprocess_contract_testcases.md) | 子进程契约的测试用例清单与落地记录 | P0/P1 用例已跑 |

## 相关实现与模块文档

- 工具面与注册：`seelebridge/tools/README.md`
- 权限与并发策略：`seelebridge/security/README.md`
- 契约测试：`seelebridge/tools/subprocess_contract_test.go`
