# 会话切换并发模型：AB 链路测试与路线选择

> 性质：一次性工作包决策记录（2026-09-04）；以代码与测试为准。
> 相关实现：`gui/bridge.go`（switchMu）、`session/domain_actor.go`
> （Domain actor / SetActive）、`application/core/session_runtime`
> （transition manager）、测试 `application/core/session_switch_ab_test.go`。

## 1. 背景与问题

现象：忙时会话（工具/LLM 回合执行中）切换“费劲/切不到”。审查结论分三层：

1. **core 热切换不慢**：引擎忙时切换只移视图指针，探针实测 0s（
   `TestSwitchDuringToolInvocation` / `TestSwitchDuringLLMStreaming`）。
2. **真正的等待源在链路其它段**：冷加载是同步磁盘 I/O + context 装配 +
   plan restore，且在视图过渡 key 上串行；GUI 在高频事件下全量刷新/渲染。
3. **数据竞争与锁粒度不是一回事**：mutex 防止竞争；锁粒度大只会造成
   等待/串行/吞吐下降，不会造成数据竞争。

此外，GUI 并发点击多个会话时多次 `resubscribe` 会交错，需要串行保护。

## 2. 保留的设计：switchMu

- 位置：`gui.Bridge.switchMu`，只串行化“视图会话切换类”命令
  （BeginNewSession / ResumeSession / ActivateSession / ForkSessionLatest）。
- 定位：**一致性保护**（防 resubscribe/relay 交错），不是忙时切换的性能
  修复。用户确认保留该设计。
- 佐证测试：`TestBridgeSwitchCommandsSerialize`。

## 3. AB 链路测试（选择路线的依据）

测试文件：`application/core/session_switch_ab_test.go`
（`TestSessionSwitchABLockVsActor`）。

### 3.1 方法

- 模型 A：全局 mutex 同时保护“当前视图”与忙写（高粒度锁串行）。
- 模型 B：actor（单 goroutine + channel）串行处理忙写与切换（无共享锁）。
- 公平负载：两个“忙会话”按相同频率注入镜像写（避免 actor 队列被写风暴
  排挤造成假差距）；4 个调用方 × 80 次切换 × 3 轮取中位数。
- 两个维度：
  - 数据安全：请求无丢失、active 合法、`-race` 下无数据竞争；
  - 速度：median 总耗时、median 最长排队、吞吐（switches/s）。

### 3.2 实测结果（示例输出，非承诺值）

| 模型 | median 总耗时 | median 最长排队 | 吞吐 | 数据安全 |
|---|---|---|---|---|
| A：全局 mutex | ~0.5 ms / 轮 | 0–0.5 ms | ~57–60 万 switches/s | 无丢失、active 合法、-race 干净 |
| B：actor 通道 | ~2.2–2.4 ms / 轮 | ~0.6 ms | ~13–14 万 switches/s | 无丢失、active 合法、-race 干净 |

（本机示例；CI 环境不稳定，测试不做硬性能断言，只保留计量输出与正确性
断言。）

### 3.3 解读

- 纯内存微模型下，**mutex 吞吐更高**（省去消息往返/排队）；actor 低约
  4 倍，但差距在微秒级，远低于真实 UI 切换频率。
- actor 的收益不体现在这个数字里，而在**结构性**：切换/写变成单
  goroutine 串行消息，无大临界区、无跨会话锁等待扩散、故障可隔离。

## 4. 路线选择与结论

1. **保留 `switchMu`**（并发点击一致性保护），不因“actor 更优雅”而全面
   替换 Bridge 命令面。
2. **保留 `session.Domain` actor 作为视图指针唯一持有者**：视图切换的
   “移指针”操作本身原子无锁化；不把整套热/冷切换搬进 actor（会引入无谓
   的消息往返与队列阻塞）。
3. **数据竞争检测仍以 `-race` 门禁 + 不变量断言为准**（生产 Go 无法运行时
   检测 race；`fault_guard` 负责 panic 后的降级退出）。
4. **忙时“切不动”的下一步不是“再 actor 化切换”，而是异步冷加载 + 切换
   进度呈现**：先给目标会话空壳/进度，后台完成装载后再发布基线，消除
   同步 I/O 在视图过渡 key 上的串行等待。

本 AB 链路作为“选择路线依据”保留在仓库：后续若再评估切换并发模型
（例如整体 actor 化或进程隔离），应复用该测试对比数据安全与速度两个维度。
