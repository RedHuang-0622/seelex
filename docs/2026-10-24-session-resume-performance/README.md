# Session resume performance — round 1

日期：2026-10-24
状态：第一轮恢复读放大、第二轮 draft 周期快照、第三轮 TUI/GUI 构建边界与
GUI 资产外置已实现。

## 目标

1. 冷加载只读取 compact 指针之后的会话后缀，不重复构造事实源前缀。
2. 草稿尾探测只检查 `head.Bytes` 后的末分片后缀和 head 未索引分片。
3. 优化前后 provider 上下文 SHA-256 保持一致。
4. 不引入跨会话状态机、整会话缓存或新的后台调度器。

## 现场

dev 数据根共 41 个会话、约 337 MB。会话行数中位数 316，P90 为 1005；
最大会话 1665 行、约 117 MB。问题不是会话数量，而是大会话分片的重复 JSON
解码。

合成基准使用 1700 行、每行 64 KiB（约 111 MB），compact 指针位于 seq 1200。
同一进程内执行优化前/后 A/B，避免机器温缓存差异。

## 第一轮实现

### 事实源读取

- `DerivedResumeRecordWorkspace`：从最新 compact 帧的 `MessageToSeq + 1`
  派生可见后缀，并返回完整序号规模。
- `LoadSessionResumeRecord`：生产 v8 存储走紧凑读面；旧格式仍回退完整
  `LoadSessionRecord`。
- `resumeSessionCold`：record 命中时不再读取 provider history；旧格式会话
  仍保留 history 回退。

### 草稿尾探测

- 已发布分片按 `shardInfo.Bytes` 定位发布点，只读取该字节偏移后的未索引后缀。
- head 未索引的分片才整片读取；老 head 没有 `Bytes` 时保留整片回退。
- 不改变“探测只读、恢复/丢弃显式”的语义。

### 上下文一致性

compact 摘要 + 后缀的 wire 装配结果在 store 关闭重开前后保持相同
`PrefixDigest`：

```text
4cd5d1e5c4ebd48a65c80c30aaed59a6adafc8312e4b6a350938cde59f20b59c
```

该值由 `TestResumeRecordUsesCompactPointerAndKeepsContextDigest` 断言。

## 第一轮结果

| 指标 | 优化前 | 优化后 | 变化 |
|---|---:|---:|---:|
| 1700×64 KiB 冷读耗时 | 2.745 s | 0.802 s | -70.8% |
| 分片解码次数 | 58 | 12 | -79.3% |
| 累计分配 | 1.697 GB | 0.374 GB | -78.0% |
| 单次 record 派生 | 2.007 s / 17 片 | 1.029 s / 5 片 | -48.7% |
| pending-tail 探测 | 全部分片 | 0 次解码（无尾）/ ≤2 次（末片+新片） | 消除前缀重读 |
| 上下文 SHA-256 | 一致 | 一致 | 无漂移 |

应用层状态机对照（同一个内存 store 夹具，不含真实磁盘）：

| 路径 | 耗时 | 分配 |
|---|---:|---:|
| hot resume | 248 µs | 155 KB / 147 allocs |
| cold resume | 1.402 ms | 1.21 MB / 863 allocs |

复现命令：

```text
go test ./store -run '^$' -bench '^BenchmarkSessionColdLoad/tool-heavy-1700rows-64KiB/(record_full|record_resume|pending_tail|wire|parallel_reads_legacy|parallel_reads)$' -benchmem -benchtime=3x
go test ./application/core -run '^$' -bench '^BenchmarkSessionRecoveryPath$' -benchmem -benchtime=50x
go test ./store -run '^TestResumeRecordUsesCompactPointerAndKeepsContextDigest$' -count=1 -v
go test ./store -run '^$' -bench '^BenchmarkDraftSnapshotAppend$' -benchmem -benchtime=20x
go test ./application/core -run '^TestDraftSnapshot' -count=1
```

## 验证

- `go test ./store`：通过。
- `go test ./application/core ./backend/engine/task ./backend/engine/session ./wiring/adapters ./store`：通过。
- `go test -race ./application/core ./backend/engine/task ./backend/engine/session ./wiring/adapters ./store`：通过。
- `go vet ./application/core ./backend/engine/task ./backend/engine/session ./wiring/adapters ./store`：通过。
- `go test ./... -run '^$'`：全仓测试二进制编译通过。
- 全仓 `go test ./...` 仍被工作区中既有的环境上下文/SSH registry 未完成改动阻断；
  不是本轮恢复性能改动引入。

## 后续里程碑与回滚

### M2 — draft 双指针快照（已实现）

- 内存态保存 `left = 已写入快照的 seq`、`right = 最新 draft seq`；
  高频动作只追加已存在的内存行并推进 `right`。
- 长任务定时器按固定周期把 `(left, right]` 追加到 draft JSONL，成功后推进
  `left`；不按每个 chunk/tool action 写文件。
- 回合提交把同一后缀并入 message 事实源，清空快照；冷加载才读取快照恢复。
- 周期为 5 秒；短回合不触发文件 I/O。连续快照复用已核对的末分片指针，
  `BenchmarkDraftSnapshotAppend` 同现场 A/B：21.75 ms / 1.9 次解码 →
  17.89 ms / 0 次解码，分配 2.04 MB → 0.52 MB。
- 回滚：保留现有同步 `appendDraftRows` 作为对照实现；若快照写入或恢复校验
  失败，停止 snapshot 调度并回到回合收尾整次持久化。已写 draft 仍由现有
  pending-tail 探测/丢弃处理，无需数据迁移。

### M3 — binary size（已实现可安全部分）

- 已确认 GUI 产物约 36.56 MiB，CLI 约 26.37 MiB；前端静态载荷约 7.75 MiB
  （mermaid 单文件约 3.18 MiB）。`-s -w` 已启用。
- GUI/TUI 启动分派按 `gui` 标签拆分：GUI 不链接 Bubble Tea/TUI；TUI 不链接
  GUI terminal/PTY。没有删除任何用户可见能力，只禁止每个前端加载另一个前端。
- 启动核心（HTML/CSS/业务 JS、Pico）仍内嵌；约 5.36 MiB 的 mermaid/pdf.js/
  docx-preview/xterm/highlight vendor 移到 `<exe>/frontend/vendor`。运行时
  overlay 优先读外部目录，缺失时由前端既有“组件未加载”分支降级。
- 生产 embed 不再收录 `*.test.mjs` 等测试资产。
- GUI stripped exe 从 36.73 MiB 降到 29.15 MiB（-7.58 MiB / -20.6%）。
  TUI 保持约 26.51 MiB：GUI stub 与 terminal 原本已被 linker DCE，进一步缩小
  只剩全局禁用内联等会伤害运行性能的选项，因此未采用。
- 共同大头 `Seele/agent → microHub + gRPC/protobuf`（约 1.4 MiB text）和完整
  MCP/工具注册面属于上游硬依赖或产品能力，不在本轮删除。
- 依赖归因（未剥离符号，text 段）：TUI 中 seelex 约 3.94 MiB、stdlib 约
  4.28 MiB、microHub/grpc/protobuf 约 1.40 MiB、config parsing 约 0.48 MiB、
  TUI 库约 0.42 MiB。GUI 与 TUI 的 microHub/gRPC 等 1.40 MiB 完全相同，来自
  `Seele/agent` 的静态 import；Seelex 侧无法无损裁剪。
- GUI 新增的核心 embed 约 2.4 MiB；外置 vendor 5.36 MiB（18 文件）。生产 embed
  排除 `*.test.mjs` 后，可执行文件不再携带测试资产。
- 依赖图验证：TUI 不再含 `seelex/gui`、GUI terminal 或 Wails；GUI 不再含
  `seelex/tui`、Bubble Tea 或 Lip Gloss。`go test ./gui` 与
  `go test -tags gui ./gui` 均通过，外置 vendor overlay 有独立单测。
- 回滚：删除 `frontend/` 外部目录仍可启动，但大预览能力降级；完整回滚可恢复
  `all:frontend/dist` embed 和单一未拆分 frontend 文件，不影响会话数据格式。
