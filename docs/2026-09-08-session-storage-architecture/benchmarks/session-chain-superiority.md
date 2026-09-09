# 新链路优越性说明（会话恢复 / 存储热点）

> 数据文件：[session-chain-bench.json](./session-chain-bench.json)。旧链路
> 数据取自删除前 AB 对照（[ab-session-chain-report.md](../ab-session-chain-report.md)），
> 新链路为本轮实测（`TestSessionChainSmokeAndMetricsNew`）。

## 1. 功能结果

新链路会话冒烟通过：6 轮 mock 请求 → 进程重启 → `ResumeSession` → 第 7 轮，
“重启后首请求”与“不重启继续运行”逐条一致（9/9 消息，无丢/重/乱序）；
`TestHeadlessRestorePrefixProbe` 同样通过（恢复前缀一致）。

## 2. 会话恢复性能

| 指标 | 旧链路（参照） | 新链路（实测） | 差异 |
|---|---:|---:|---|
| 冷恢复耗时（3 次平均） | 191.2 ms | 47.0 ms | 约 4.1× 更快 |
| 重启后首请求消息数 | 9 | 9 | 一致 |

新链路冷恢复只需读 guide/模块 head + 覆盖区尾分片，再经 wire 装配；旧链路
要读 manifest + history shard/transcript，并曾参与 rollout 重放。

## 3. 存储与读写热点

| 指标 | 旧链路（参照） | 新链路（实测） |
|---|---:|---:|
| store 文件数 | 32 | 8 |
| store 字节 | 47 557 B | 8 727 B（约 5.4× 更小） |
| generation 目录 | 14（每轮新建目录累积） | 0 |
| rollout/transcript 双写 | 12 224 B + 2 715 B | 0（实现已删除） |
| state.json | 28 261 B（14 份 generation 副本） | 2 970 B（单份） |
| history | 3 968 B 分片重写 | message-rows 3 152 B + history-cache 398 B |
| 模块 head | manifest 243 B | metadata-heads 1 659 B（按模块拆分发布） |

## 4. 结论

- 写侧：旧链路每轮“全量 generation 重写 + state/transcript/rollout 多通道
  双写”，目录与字节随时间线性膨胀；新链路为“事件行 append + 模块 head 原子
  替换 + 单份 history/state 缓存”，无目录累积。
- 读侧：旧链路冷恢复需在 manifest/history/transcript（与 rollout）间选择与
  解析；新链路按需读 head + 覆盖尾分片，恢复耗时约 4 倍优势（本机样本）。
- 遗留热点：`history.json` 与 `state.json` 仍每轮整体替换（兼容现有全量读
  语义），消除该热点属后续（惰性派生/重建 + reconcile），不影响本结论。
