# 会话切换全链路 A/B 对比

> 测试：`repro_chain_ab_test.go`（TestChainSwitchABStable）
> 日期：2026-09-01

## 目的

用真实链路（seelebridge Runtime + adapters.EnginePort + sessionstore 真
数据 + 假 provider）验证会话切换行为：同一脚本跑两遍（变体 A/B），断言
结果一致且无死锁。

## 脚本

1. 长会话 A 首轮完成；
2. fork 出 B 并切换（活跃 = B）；
3. 切回 A，发起长任务（provider 第二请求阻塞 → A 运行中）；
4. 切到 B（空闲）→ B 提交并完成；
5. 切回**运行中**的 A（热挂载，必须快速返回，无死锁）；
6. 释放 A → 全部收敛。

## 断言

- 每次运行：热挂载运行中会话耗时 ≤ 2s（无死锁）；
- A 对话含自身输入与 DONE_FROM_A，不含 B 输入（无串写）；
- A/B 两遍运行：活跃会话、会话列表、对话文本、事件指纹逐项一致。

## 背景

历史事故：热挂载运行中会话时 `SetSystemPromptFor` 等待框架 Session 锁，
且锁序为 TransitionLock → 引擎锁，导致切换后提交消息、再切换全部冻结。
本用例把该场景固化到全链路层；修复为运行中会话热挂载不触碰引擎 prompt。
