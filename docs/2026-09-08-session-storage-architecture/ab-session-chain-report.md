# AB 双链路会话冒烟与性能对比报告（2026-09-09）

> 性质：一次性验收工作包。旧链路 = `SEELEX_STORAGE_LAYOUT=legacy`
> （manifest/generation/transcript.log/rollout 布局 + 旧三读恢复）；新链路 =
> v8 默认布局 + R2 装配恢复（compact 摘要 + 尾窗 + 最近 K 条尝试）。

## 1. 场景与断言

测试：`go test . -run TestABSessionChainSmokeLegacyVsV8 -count=1 -v`（根包
[ab_session_chain_test.go](../../ab_session_chain_test.go)）。

同一 mock provider 场景：6 轮快速请求 →（对照 A：不重启继续第 7 轮；
对照 B：进程重启 → `ResumeSession` → 提交第 7 轮）。冷恢复耗时采样 3 次取
平均；store 文件/字节按类别统计（读写热点）。

断言（全部通过）：

1. 链路内一致性：两条链路各自“重启后首请求”与“不重启继续运行”逐条一致；
2. 跨链路一致性：v8 与 legacy 的重启请求、继续运行请求逐条一致
   （无丢、无重、无乱序；消息数均为 9/9）；
3. 冷恢复内容完整：可见会话恢复后包含 seed 与后续轮次，状态离开 restoring。

## 2. 结果对比（本机 2026-09-09 两次运行）

| 指标 | legacy | v8 | 说明 |
|---|---|---|---|
| rounds_wall（6+1 轮） | 622–661 ms | 375–459 ms | mock 往返；v8 本次略快（含噪声） |
| cold_resume（3 次平均） | 191 ms（首轮 57 ms） | 45 ms（首轮 60 ms） | 第二次运行 v8 显著更快；首轮受进程冷启动主导 |
| restart_messages | 9 | 9 | 逐条一致 |
| store_files | 32 | 8 | 6 轮 + 重启后 |
| store_bytes | ~47.6 KB | ~8.7 KB | 约 5.5× |
| generation_dirs | 14 | 0 | legacy 每轮提交新 generation 目录累积 |

## 3. 读写热点对比（字节分布，第二次运行）

| 类别 | legacy | v8 | 热点解读 |
|---|---:|---:|---|
| state.json | 28 261 B | 2 974 B | legacy 每轮在 14 个 generation 目录各复制一份；v8 单份 |
| rollout.jsonl | 12 224 B | 0 | legacy 双写全序日志；v8 由 message/event 行承担 |
| transcript.log | 2 718 B | 0 | legacy 事件双写；v8 正文即 `message_*.jsonl` |
| legacy-history-shard | 3 968 B | 0 | legacy 每轮整段重写 history 分片 |
| v8-message | 0 | 3 154 B | v8 事件行 append（含 6 轮 + 重启后新行） |
| v8-metadata-head | 0 | 1 655 B | guide + message/event/toolresult 等模块 head |
| v8-history-cache | 0 | ~398 B（计入 other） | provider 缓存单文件替换 |
| manifest.json | 242 B | 0 | legacy manifest 发布点；v8 用模块 head |

结论：

- **写热点**：legacy 每轮提交 = 新建 generation 目录 + 全量 history/state
  重写 + transcript/rollout 双写；v8 = 事件行 append + 模块 head 原子替换 +
  history.json 缓存替换。v8 文件数与字节量约低一个数量级，且不存在目录
  累积（generation_dirs=0 vs 14）。
- **读热点**：legacy 冷恢复读 manifest → history shard / transcript /
  rollout（三读取一）；v8 冷恢复读 guide/模块 head → 覆盖区间尾分片，再经
  R2 装配出 wire（本机 3 次采样 v8 更快；大会话的收益预期更大，因为 v8
  无需整段解析旧 generation/rollout）。
- **已知写侧保留热点**：v8 每轮仍整体替换 `history.json` 与 `state.json`
  （兼容现有全量读语义）；若长会话要消除该热点，属 M5（由事件行派生/惰性
  重建 + reconcile）范畴，本次未实现。

## 4. 运行期接线验证

- R2：`resumeSessionCold` 对 v8 会话经
  `SessionWireAssemblerPort` → `Router.AssembleWireWorkspace` 装配
  engineHistory；legacy/SQL/未装配回退旧链路（ok=false）。
- compact：`SessionContextStore.PushCompact` 把运行期压缩帧桥接进 v8
  compact 通道（幂等），帧摘要进入后续 R2 装配；同时触发 retention
  advisory（mode=manual，不自动删）。
- lifecycle：`resumeSessionCold` 重启后调用
  `LifecycleRecover`（v8 队列发送未确认项回 queued、message 已发布项出队）；
  engine 侧的发送/排空仍由运行期引擎负责。
- LRU：`Router.LRUDeleteWorkspace(…, confirmed)` 已暴露（manual 模式未确认
  拒绝）；运行时默认不自动删，需用户确认后由调用方触发。
