# test/session（集成用例分卷）

## 生态位

会话生命周期与切换成本：链路基准与切换剖面。

覆盖：`session_*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### session_chain_bench_test.go

- `func TestSessionChainSmokeAndMetricsNew(t *testing.T)` — TestSessionChainSmokeAndMetricsNew 新链路会话冒烟 + 指标采集。
- `func chainStoreMetrics(t *testing.T, root string) (int, int64, map[string]int64, int, int)`

### session_switch_profile_test.go

- `func TestSessionSwitchHotColdProfile(t *testing.T)` — TestSessionSwitchHotColdProfile 应用层多会话冷热切换 + 重启恢复探查。
- `func TestStorageConcurrentSessionLockProfile(t *testing.T)` — TestStorageConcurrentSessionLockProfile 存储层多会话并发读写（锁竞争/热点）。
- `func newEchoProvider(t *testing.T) *httptest.Server`
