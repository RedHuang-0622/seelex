# 并发卡死排障三步走：pprof 观察 → 同链路复现 → 锁序修复

> 日期：2026-09-08 · 状态：方法论沉淀（含一次已落地案例）
> 适用：前端/后端“看起来一直 queued、迟迟不动”的并发问题（fork 子代理、
> 会话切换、详情弹窗数据面等）。先按本方法拿到证据，再动手改代码。

## 1. pprof 观察（先取证，不猜）

GUI 日常构建不带采样端口；排障用带 `pprof` tag 的 GUI 构建：

```powershell
go build -tags "gui,desktop,production,pprof" -trimpath `
  -ldflags "-s -w -H windowsgui `
  -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=pprof-dev `
  -X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui" `
  -o dist\seelex-gui-pprof.exe .
```

从仓库根启动（否则账号配置解析不到），复现卡顿后抓取全量 goroutine：

```powershell
Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:6060/debug/pprof/goroutine?debug=2" -OutFile goroutines.txt
```

分析清单：

1. 枚举所有等待型 goroutine（`sync.Mutex.Lock` / `sync.RWMutex.Lock` /
   `sync.RWMutex.RLock` / `sync.WaitGroup.Wait`），记录每条完整栈；
2. 对每个锁等待，向上找**谁拿着这把锁**（同一把锁地址可从栈顶互斥量判断，
   再找“不等待、正在执行/持有锁”的 goroutine）；
3. 画等待环：A 持 X 等 Y，B 持 Y 等 X → 锁序反转；只有一方等、另一方在
   做正经长任务 → 可能是资源/账号槽/超时问题；
4. 区分“等超时”和“真死锁”：超时只保证最终退出，锁序环会一直挂到超时
   兜底才松开（本仓库案例：fork 有 `limits.fork_timeout` 兜底，但根因是
   `ViewMu ⇄ Session.mu` 环）。

## 2. 同链路复现（把调用链变成最小测试）

不要只靠现象描述。把观察到的锁序拆成最小并发单元：

- 角色 A：持“会话锁”（真实场景 = 节点 `ReActLoop` 持 `Session.mu`）后回调
  需要 `ViewMu.Lock` 的路径；
- 角色 B：前端/投影路径先拿 `ViewMu.RLock` 再读引擎子树（内部要拿会话锁）；
- 用阻塞桩/门闩让两边并发，断言“有限时间内都能完成”。

落地案例：`application/core/subagent_detail_lock_regression_test.go` 的
`TestSubagentDetailNoViewMuSessionLockInversion`——假引擎持 `sessionMu`
模拟节点执行，另一侧调 `SubagentSessionDetail`；修复前成环、修复后 3s 内
完成（红→绿）。

复现测试要求：

- 不依赖真实 LLM/账号；锁序用桩即可复刻；
- 必须能在修复前失败（红），修复后通过（绿）；
- 跑 `-race` 更稳（锁序类问题用确定性门闩，不赌调度）。

## 3. 锁序修复（最小改动 + 回归）

本案例根因与修法：

- 根因：`SubagentDetail` 持 `ViewMu.RLock` 调 `Engine.SubAgentTree()`（内部
  读节点 `Session.History` → `Session.mu`）；节点执行 goroutine 持
  `Session.mu` 时经工具事件回调 `HandleSubagentToolEvent` 要 `ViewMu.Lock`
  ——形成 `ViewMu → Session.mu` 与 `Session.mu → ViewMu` 的环；
- 修法：引擎子树读取移到 `ViewMu` 锁外（只读 actor 面），锁内只读 Plan/
  WorkTable 快照；`RefreshWorkTableSnapshot` 已按同一原则锁外取树；
- 配套：能造成同类重入的会话级控制工具（`switch_plugin`/`switch_mode`/
  `skill_activate`）对子代理隐藏；失败原文透传到详情/工作台，让“卡住/失败”
  在 GUI 可见。

收尾清单：

- 复现测试红→绿（本案例：`go test ./application/core -run TestSubagentDetailNoViewMuSessionLockInversion -count=1`）；
- 跑受影响包与 `go build ./...`；
- 同步模块 README（锁序/边界注释）与本文档；
- 按主题分组提交（代码修复 / 测试与文档），提交后跑 `git diff --check`。

## 4. 经验速查

| 现象 | 优先怀疑 | 处置 |
|---|---|---|
| 一个子代理 done、另一个一直 running/queued | 账号槽被确定性哈希钉死；或会话锁成环 | 按角色分散空闲账号（见 account leastBusy）；锁外取引擎数据 |
| 前端轮询详情/PerfStats 全卡 | `ViewMu` 读锁被等待中的写者堵死 | 抓转储确认环，减少持读锁时的引擎调用 |
| 工具回调里卡住导致“持有者在 main” | 会话控制工具在子代理会话锁内重入 | 子代理隐藏该类工具（nodeScopeExcludedTool） |
| 有 fork 超时仍感觉永久卡 | 超时兜底太长或超时不能传播 | 按任务声明 `timeout_sec`；确认取消级联（AfterFunc） |

提交记录：锁序修复 `3d70836`；fork 超时按任务分配 `36320fa`；账号槽分散
`3a26343`；goal A2A 回合驱动 `11c940c`。
