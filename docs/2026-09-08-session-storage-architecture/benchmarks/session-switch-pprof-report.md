# 多会话冷热切换 pprof 探查报告（2026-09-09）

## 1. 场景与命令

探查测试：
[session_switch_profile_test.go](../../../session_switch_profile_test.go)

- `TestSessionSwitchHotColdProfile`：A→B→C fork 链 + 独立会话 D，各自完成
  轮次后：热切换 8 次、冷切换（UnloadSession → ResumeSession）2 轮、进程
  重启冷恢复 C；每次切换校验目标会话无其它会话 seed（幻读/跨会话污染）。
- `TestStorageConcurrentSessionLockProfile`：4 个会话 4 写 + 4 读并发
  （SaveCommit / AssembleWire / ReadRange / ReadEventTail），制造可观测的
  锁竞争与读写热点。

命令（产物在 `%TEMP%\switch.{cpu,mem,mutex,block}`）：

```text
go test . -run 'TestSessionSwitchHotColdProfile|TestStorageConcurrentSessionLockProfile' \
  -count=1 -cpuprofile %TEMP%\switch.cpu -memprofile %TEMP%\switch.mem \
  -mutexprofile %TEMP%\switch.mutex -blockprofile %TEMP%\switch.block -timeout 600s
go tool pprof -top -nodecount=30 %TEMP%\switch.mutex
go tool pprof -top -nodecount=30 %TEMP%\switch.block
go tool pprof -top -alloc_space -nodecount=24 %TEMP%\switch.mem
```

## 2. 应用层切换结果

- 热/冷切换与恢复全部通过，无死锁（测试快速收敛，无 timeout goroutine
  dump）、无跨会话污染/幻读。
- 实测冷恢复约 42–90 ms/次，进程重启冷恢复 C 约 28–41 ms；热切换为驻留
  视图切换，未观察到锁等待热点。

## 3. 锁竞争（mutex/block profile）

并发存储压力下存在**仓库级全局锁串行化**，这是当前主要锁热点：

- `jsonRepository.mu`（WriteCommit/read 外层 RWMutex）+ `Router.mu`
  （withRepositoryAt）把不同会话的写与读串到同一把锁：mutex profile 中
  SaveCommitWorkspace 占约 79% 的锁延迟，读路径约占 11%；
- 写锁（WriteCommit）42.1s / RWMutex.Unlock 15.6s / RLock 4.5s（压力场景
  累计），说明多会话后台落盘与读会互相等待；
- 应用层 actor（会话过渡/生命周期/目录刷新/看板发布）阻塞延迟合计很小
  （1–4s 量级，主要来自 channel 空转等待），热/冷切换本身无锁竞争。

结论：存储引擎内部已按模块拆分锁（module_heads.go 各模块锁），但
jsonRepository/Router 的外层全局 RWMutex 仍把所有会话串行化。多会话并发
落盘时的锁竞争属于当前真实热点；优化方向为把外层锁收敛到
“config/Repository 原子切换”与“单会话单写者”粒度，不再跨会话共享同一把锁。

### 优化实施后复测（2026-09-09 同机）

已将实现落到代码并复测：

- Router：`withRepository*` 不再在数据操作全程持有 `router.mu`，改为
  acquire/release 在途计数（`activeOps` + cond），仅在 Repository 切换与
  Close 时等待归零；
- jsonRepository：JSON 读写不再持仓库级 `repository.mu`（文件/head 由引擎
  会话锁保护）；
- 引擎：模块锁从“仓库级单实例”改为“会话 × 模块”注册表
  （`storeEngine.sessionMu`），单会话单写者、跨会话并行。

复测 mutex/block 指标：

| 指标 | 优化前 | 优化后 | 变化 |
|---|---:|---:|---|
| mutex delay 总量 | 53.1 s | 11.5 s | 约 4.6× 下降 |
| SaveCommitWorkspace 锁延迟占比 | 79%（42.1 s） | 58%（6.7 s，跨会话全局锁已消除） | 显著下降 |
| 探查场景总时长 | 12.7 s | 6.2 s | 约 2× |

剩余等待为**同会话**读与写之间对会话 message 锁的串行（单会话单写者 +
读需等待该会话提交完成），不再跨会话互相阻塞；进一步可把 message 锁升级为
读写锁或按请求异步 actor 解耦同会话读写，属于下一步。

## 4. 数据热点（cpu/mem profile）

- 读侧：`readRowsLocked → io.ReadAll + decodeMessageRows` 分配占比最高
  （io.ReadAll 16.4%，decodeMessageRows cum 19.1%，readRowsLocked cum
  34.2%）；每次 AssembleWire/ReadEventTail/ReadRange 都会全量读文件并解码
  全部行，长会话下应改为“按 head 水位读尾分片 + 增量缓存/校验复用”。
- 写侧：每轮提交的原子替换（os.rename ≈ 4.5% CPU）与 JSON
  Marshal/Unmarshal 是固定开销；GetFileAttributesEx（布局判定 stat）
  约 3.4%，属于每操作一次的目录探测，可缓存会话布局判定。
- 冷/热切换与恢复路径占 CPU 很小（resumeSessionCold cum 3.4%，
  ForkSessionLatest 3.1%），未发现锁等待或阻塞热点。

### 读放大修复（2026-09-09）

已实现尾窗按需读分片（`readTailRowsForSelection`）：正常窗口读只从最后一个
分片向前累积到 maxUnits/token 预算（含边界余量），不再每轮全量解码整个
message 文件。5000 行/50 分片实测（`TestTailReadAmplificationMeasurement`）：

| 读取方式 | 耗时 | 说明 |
|---|---:|---|
| 全量读（旧行为） | 69.0 ms | 50 个分片全部读+解码 |
| 尾窗按需读（新） | 3.8 ms | 仅 7 个分片（700 行输入） |

约 **18× 提速**，且 `selectEventTail` 结果与全量读完全一致。该优化直接降低
“每轮上下文读/冷恢复尾窗”的读放大；长会话全量需求（history 缓存、R1 历史
翻页）仍按需读取，属后续可继续缓存/分片化点。

## 5. 结论

- 无死锁、无数据竞争（见 race 全绿记录）、无幻读/跨会话污染。
- 真实热点 = ① 仓库级全局锁导致的跨会话写/读串行；② 读侧每次全量读文件
  + 全量 JSON 解码；③ 写侧每次原子替换的固定 syscall/序列化开销。
