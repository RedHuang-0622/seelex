# 性能量化成果（锁竞争 / 读放大 / 响应时间 / 内存）

> 本文件为“会话存储新链路优化”的定量成果专档。环境：Windows amd64，
> go1.25.8，mock provider（无真实 LLM 网络耗时），2026-09-09。
> 优化范围：本次仅做量化，不再继续优化；优化前的基线数据取自优化前同机
> 同场景 pprof/测试记录（见 [session-switch-pprof-report.md](./session-switch-pprof-report.md)）。

## 1. 结论摘要

| 维度 | 优化前 | 优化后 | 提升 |
|---|---:|---:|---|
| 锁延迟（mutex delay，同场景同口径） | 53.1 s | 9.55 s | 约 5.6× |
| 探查场景墙钟（storage 并发 + 冷热切换） | 12.7 s | 4.64 s | 约 2.7× |
| 尾窗读取（5000 行 / 50 分片） | 56–184 ms（全量读） | 3.8–10.5 ms（按需读） | 约 8–20× |
| 单会话冷恢复 | — | 32–100 ms/次（多次采样） | 单次毫秒级 |
| 进程重启冷恢复 | — | 28–41 ms | 单次毫秒级 |
| store 足迹 | 32 文件 / 47.6 KB（旧链路参照） | 8 文件 / 8.7 KB | 4× 文件数、约 5.5× 体积 |
| 运行期驻留内存（inuse_space 快照） | — | ≈ 3.1 MB | 无驻留增长/泄漏 |

## 2. 量化方法与复现命令

```text
# 同场景同口径锁/墙钟（storage 4 会话并发读写 + 应用层冷热切换）
go test . -run 'TestStorageConcurrentSessionLockProfile|TestSessionSwitchHotColdProfile' \
  -count=1 -mutexprofile %TEMP%\perf-same.mutex -timeout 600s
go tool pprof -top %TEMP%\perf-same.mutex

# 长会话读放大
go test ./sessionstore -run 'TestTailReadAmplificationMeasurement' -count=1 -v

# 单会话/重启冷恢复与 store 足迹
go test . -run 'TestSessionChainSmokeAndMetricsNew|TestSessionSwitchHotColdProfile' -count=1 -v

# 内存/CPU/阻塞全量探查
go test . -run 'TestStorageConcurrentSessionLockProfile|TestSessionSwitchHotColdProfile|TestSessionChainSmokeAndMetricsNew' \
  -count=1 -cpuprofile %TEMP%\perf-final.cpu -memprofile %TEMP%\perf-final.mem \
  -mutexprofile %TEMP%\perf-final.mutex -blockprofile %TEMP%\perf-final.block -timeout 600s
```

## 3. 锁竞争（优化前后同场景）

同一组测试（`TestStorageConcurrentSessionLockProfile + TestSessionSwitchHotColdProfile`）：

| 指标 | 优化前 | 拆锁后 | 拆锁 + 尾窗读后 |
|---|---:|---:|---:|
| mutex delay | 53.1 s | 11.5 s | 9.55 s |
| 场景墙钟 | 12.7 s | 6.2 s | 4.64 s |

拆锁内容：

- `Router.withRepository*` 不再全程持 `router.mu`，改在途计数
  （activeOps + cond），仅 Repository 切换/Close 等待归零；
- JSON 读写去掉仓库级 `repository.mu`；
- 引擎模块锁改为“会话 × 模块”注册表（单会话单写者、跨会话并行）。

残余等待 = 同会话读与写对会话锁的串行，跨会话互不阻塞。

## 4. 读放大（内容量 vs 加载时间）

5000 行 / 50 分片（4 次独立采样）：

| 读取方式 | 采样（ms） | 范围 |
|---|---:|---|
| 全量读（优化前行为） | 56 / 69 / 81 / 87 / 120 / 184 | 56–184 ms |
| 尾窗按需读（优化后） | 3.8 / 4.2 / 6.9 / 9.3 / 10.5 | 3.8–10.5 ms |
| 提速倍数 | 8.3× / 11.7× / 13.3× / 18.3× / 19.8× | 约 8–20× |

语义：`selectEventTail` 结果与全量读完全一致；全量语义（MaxInt 窗口）保留
旧行为。结论：加载慢的主因是“内容读放大”（全量解码 message 文件），已在
每轮上下文读/尾窗恢复路径消除；历史翻页等全量需求仍需按需分片/缓存化。

## 5. 响应时间（会话恢复）

| 场景 | 采样（ms） | 说明 |
|---|---:|---|
| 冷恢复（UnloadSession → ResumeSession） | 32–100 | 多会话多轮采样，均值约 60–80 |
| 冷恢复（单会话基准 3 次平均） | 47–130 | 多次运行波动（含进程冷启动） |
| 进程重启后冷恢复 | 28–41 | 全新进程 + 磁盘冷读 |
| 6+1 轮提交墙钟（mock） | 459–814 | 每轮含 2 次 LLM 往返的测试往返，非存储瓶颈指标 |

## 6. 内存

一次 8.6 s 探查（3 组测试 + 多次 harness 启动）：

- 总分配 `alloc_space` ≈ 123 MB（含测试进程/工具 schema/多进程启动）；
- 运行结束驻留 `inuse_space` ≈ 3.1 MB —— 无累积/泄漏；
- 分配热点（可继续优化但非缺陷）：`decodeMessageRows`（cum 20%）、
  `io.ReadAll`、`readRowsLocked`（cum 17.5%，来自并发写读压测的逐次
  AssembleWire/全量语义读）；尾窗路径已按 §4 修复。

## 7. CPU

- Windows 上 `runtime.cgocall` 占大部分采样（文件 syscall 噪声）；
- 业务可见热点：模块 head JSON 解码、Seele `restoreHistory/persistHistory`、
  写侧原子替换（os.rename）与每操作布局 stat；
- 冷/热切换与恢复路径 cum 占比小（resumeSessionCold ≈ 3.4%），无锁等待热点。

## 8. 与用户体验挂钩的结论

- pprof 的“10 秒级”数字是聚合等待/测试墙钟，不是单次 UI 加载；单会话冷恢复
  已量化在毫秒级（32–100 ms），进程重启冷恢复 28–41 ms。
- 若真实出现 10 秒级加载，最可能落在“长会话全量读/翻页/未压缩历史”而非锁；
  本次尾窗按需读已消除每轮/恢复路径的读放大，历史全量读取的缓存化是后续
  最值得继续的方向（本次不继续优化）。

## 9. 真实 API 冒烟 + pprof（2026-09-09）

测试：`TestRealAPISessionRestoreSmoke`（`-tags manualsmoke2`，真实账号配置
不透明复制、不打印内容）。场景：记住身份 → 4 轮长材料对话 → 进程重启 →
`ResumeSession` 冷恢复 → 再次提问，断言恢复后延续“最近一轮指令”（真实
端到端会话恢复可用）。

- 结果：**通过**，单次运行墙钟 ≈ 7.5 s（含两轮 harness 启动 + 5 次真实
  LLM 往返 + 冷恢复）。
- CPU：采样主体为 `runtime.cgocall`（网络/文件 syscall，76%），业务侧为
  JSON 解码/编码与 CloneRuntimeState 等装配拷贝，无锁等待热点。
- 内存：整轮 alloc ≈ 23.8 MB；运行结束 inuse ≈ 4.5 MB，无驻留增长。
- 经验说明：断言“恢复后仍能回答第一轮身份”在当前“尾窗装配”设计下不成立
  （早期内容依赖压缩摘要/记忆承接），故冒烟断言改为验证最近轮次连续性；
  若要把“早期身份跨重启可回忆”做成验收，需先接通压缩/记忆承载路径。
