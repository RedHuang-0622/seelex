# 事件轨真·追加：执行事实事件库改逐行日志（锁面审计 §2.9）

- 日期：2026-09-29
- 范围：锁面审计 §2.9 —— `sessionstore.EventStore.Append` → `AppendFrameworkEvent`
  的「读整份 JSON 数组 → merge → 原子整文件重写」
- 不改：`EventStore.mu`（全进程唯一 sink）与「无轮转/压缩」两条（理由见「未决」）

## 1. 改前

- 链：`EventStore.Append`（`sessionstore/event_store.go`，持 `store.mu`，全进程唯一 sink）
  → `Router.AppendFrameworkEventWorkspace` → `jsonRepository.AppendFrameworkEvent`
  （持 `repository.mu`，**跨会话共用**）→ `os.Stat` → `readEventLogLocked`
  （读整份 JSON 数组）→ `mergeEventLogEntries` → `json.Marshal`（整份）
  → `writeAtomic`（整文件重写，含 rename 退避 sleep）。
- 性质：不是死锁（无反向边），是**锁持有时间 = 事件库体积**的写放大：每追加一条事实的
  代价随库线性增长，且都发生在 `store.mu`/`repository.mu` 上 → 所有会话的事实落库在一把
  锁上排队，临界区还随事件库体积一起长。

## 2. 改后

落盘形态：`<sessionRoot>/framework-events.json` 从「整份 JSON 数组」改为**逐行日志**
（一条事实 = 一行 compact JSON：`{"seq":…,"payload":{…}}`）。

- **真追加** `appendEventLogLine`：`O_APPEND|O_CREATE|O_WRONLY` 写一行 + `'\n'` 就返回，
  不读旧内容、不重写整份文件。不做 fsync，与 `writeAtomic` 同口径（进程崩溃不丢已写页，
  掉电丢未落盘页）。
- **迁移/自愈** `prepareEventLogForAppendLocked`：追加路径上唯一可能触碰全量内容的地方，
  且每会话只发生一次：
  - 文件不存在 → 按旧口径迁移 v1 布局遗留（`legacyFrameworkEventEntriesLocked`：
    `generation-N/events.json` + 会话根 `events.json`）→ `writeEventLogLines`；空库则
    留给追加路径的 `O_CREATE` 建文件；
  - 首字节 `'['` → 升级前形态：`decodeLegacyEventLogArray` + `writeEventLogLines`
    原子重写为逐行日志；
  - 首字节 `'{'` → 已是逐行日志：只做尾部自愈（`os.Truncate` 到撕裂点）；
  - 其它首字节（前导空白等手工文件）→ 退回整份判定，属罕见路径，不进入热路径代价。
  - 判形态**只看首字节**（`ReadAt` 1 字节）、自愈**只看尾部**（64 KiB 分块向前找最后的
    `'\n'`）：追加**不读整份日志**，否则「追加也要读一遍全库」会把 §2.9 要消掉的线性
    代价原样带回来。
- **读侧** `decodeEventLog`：两种形态都认（逐行日志 / 升级前整份数组）；按 `Seq` 升序
  （`sort.SliceStable`）+ `dedupeEventLogEntries`（同 `Seq` **后写者胜**），与旧
  「读整份 → `mergeEventLogEntries`」的幂等口径一致。
- **崩溃截断（撕裂尾）**：文件不以换行结束 = 最后一条是半条记录 → 读时丢弃该尾行、
  不报损坏；下一次追加前把它截掉（自愈）。已以换行结束的行损坏仍显式报错。
- **锁**：仍走 `repository.mu` 写锁，**没有**为事件库另开按会话的模块锁。这是取舍：
  追加已是 O(1) 尾写，代价不再随负载放大（`sessionstore/README.md` 同记「要再切分
  请先量出排队，别只凭『共用一把锁』下结论」）。

顺序保证：追加顺序 = 落库顺序（`O_APPEND` 尾写）；`Seq` 由 sink 侧（Recorder /
planEventSink）保证严格递增，本库不解释其语义。

## 3. 判据（红→绿实测有牙）

`sessionstore/event_log_append_only_test.go`：

- 主判据 `TestEventLogAppendsLinesWithoutRewriting`：落盘必须是逐行日志；追加后**旧字节
  是新文件的前缀**（旧实现整文件重写会把尾部 `]` 变成 `,`，前缀断言立刻红）；文件不以
  `]` 结束；读回 seq 1..4。
- 迁移 `TestEventLogMigratesLegacyArrayFileOnFirstAppend`（存量整份数组：未追加时按旧格式
  读得到，首次追加迁移成逐行且旧事实不丢）、
  `TestEventLogMigratesLegacyGenerationFilesOnFirstAppend`（v1 generation 遗留仍按旧口径
  合并）。
- 自愈 `TestEventLogDropsTornTailAndHealsOnNextAppend`（截断 12 B：读不报损坏、丢弃半条，
  下次追加截掉、日志仍合法且以换行结尾）。
- 幂等 `TestEventLogKeepsLastEntryForRepeatedSeq`（同 Seq 后写者胜，与旧 merge 一致）。
- 并发 `TestEventLogConcurrentAppendsKeepEveryEntry`（6 goroutine × 20 条：每条都要落盘、
  读回 Seq 严格递增；`-race` 下同时验追加路径不引入竞争）。

**有牙实测（本次复核）**：临时把 `AppendFrameworkEvent` 换回「读整份 → merge → 重写」→

```
--- FAIL: TestEventLogAppendsLinesWithoutRewriting (0.02s)
    event_log_append_only_test.go:111: event log is a whole-file JSON array, not an append-only line log: "…}]"
--- FAIL: TestEventLogDropsTornTailAndHealsOnNextAppend (0.02s)
    event_log_append_only_test.go:225: read with torn tail must not fail: session storage: decode event log: unexpected end of JSON input
```

复原（文件 hash 与备份一致、`git diff --numstat` 仍 261/21）后两条用例转绿。

## 4. 量测（被移除的写放大）

量测件 `_tmp/event-log-append-only-bench/zz_event_log_bench_test.go`（临时件，**不进提交**：
连同一份 `sessionstore` 包内副本才能编译，量完已移出包目录——`_logs/`/`_scratch/` 放 `.go`
会被 `e2e/layout_test.go::TestEveryGoPackageDirectoryHasReadme` 判红，只有 `_tmp/` 是被跳过
的目录）；用「直接播种 + `os.WriteFile`」避开播种期本身成为瓶颈；改前路径经
`benchEventLogRewrite` 复刻同一 `writeAtomic`。Windows / 8 核 / `-benchtime 100x`：

| 场景（已有条目数） | 改后：逐行尾写 | 改前：读整份→重写 | 倍数 |
|---|---|---|---|
| 0（空库） | 385,214 ns/op | 2,308,802 ns/op | 6.0× |
| 1,000 | 354,386 ns/op | 16,045,267 ns/op | 45× |
| 10,000 | 302,167 ns/op | 38,754,399 ns/op | 128× |

- 每次追加的**写盘字节**：改后 ≈159 B（常数，与库体积无关）；改前 10,000 条时 1.65 MB。
- 改前随体积线性增长，改后平（302–385 µs 的差是文件系统噪声，空库那次含首次建文件）。

## 5. 验证

```
go build ./...                          → ok
go vet ./sessionstore/... ./seelebridge/... ./application/core/... ./session/...  → ok
go test ./sessionstore -count=1         → ok (40.4s)
go test ./sessionstore -run TestEventLog -count=1   → ok (2.8s)
go test -race ./sessionstore -count=1   → 见下「跑测记录」
go test ./... -count=1                  → 见下「跑测记录」
```

## 6. 未决与风险

1. **`EventStore.mu` 未动**：仍是全进程唯一 sink 锁，与 `repository.mu` 一起构成事件写的
   两条串行点。本批只把临界区长度从 O(库体积) 降到 O(1)；要按会话切锁请先量出排队。
2. **无轮转/压缩**：事件库随会话单调增长，消费方按 `Seq` 区间读，但
   `ReadFrameworkEvents` 仍是整份读进内存。保留策略/分片属产品项，本批不做。
3. **读路径仍 O(库体积)**：`readEventLogLocked` 整份读 + 排序 + 去重。它走读锁、不在追加
   热路径上，与 §2.9 关注的「追加写放大」不是同一个面；若后续要动，前置条件是先有按
   `Seq` 区间读的索引面。
4. **迁移原子性**：迁移写用 `writeAtomic`（临时文件 + rename）。崩溃在 rename 前 = 仍是
   旧形态，下次追加重试；rename 后 = 已是逐行日志。无中间态，不需要额外修复路径。
5. 追加不 fsync：沿用既有取舍（与 `writeAtomic` 同口径），本批不改变。
