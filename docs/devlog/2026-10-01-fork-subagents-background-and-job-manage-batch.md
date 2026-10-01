# 子代理只走后台作业面 + `job_manage` 一次管一批（2026-10-01）

> **口径**：本文记两件事的**修前事实**、**改法**与**回归证据**：
> ① `fork_subagents` 的"阻塞模式"退场，子代理与 `bash_bg` / `read_batch` 统一成
> **只派发、不等结果**的作业工具；② `job_manage` 支持 `handles` 数组——**一次等一批**。
> 结论分 **Confirmed**（有代码 / 用例 / 命令证据）与 **Hypothesis**（待验证）。

---

## 1. 修前事实（Confirmed）

| # | 位置 | 修前事实 |
|---|---|---|
| 1 | `seelebridge/fork/types.go` 的 `Input.Async` | 作业化派发是**开关**，默认 false。省略 `async` 时 `fork_subagents` 在工具调用内同步跑完整个 `start → N×agent → summary` DAG 才返回 |
| 2 | `seelebridge/fork/tool.go` 的 `Handle` | `if input.Async { dispatchJobs } else { 阻塞 RunPlan }`——两条返回形状（受理回执 / 完成结果），模型侧看到的是两种契约 |
| 3 | `seelebridge/fork/tool.go` 的 `forkReuseResultJSON` | 结果复用走"直接返回已保存输出"的同步短路，第三种返回形状 |
| 4 | `seelebridge/tools/job_contract.go` 的 `jobManager.Fetch` | 一次请求只认一个 `handle`：一批 N 条作业要取回，就是 N 次独立工具调用，且"等最慢那个"要被重复 N 次 |
| 5 | `seelebridge/tools/job_tools.go` 的 `jobManageInput` | 只有 `op` / `handle` / `wait_ms`——没有多句柄入口 |

## 2. 改法

### 2.1 `fork_subagents`：只派发、不等结果

- `Input.Async` 删除；`Handle` 只有一条路：`dispatchJobs`（受理回执 = 本次调用的全部返回）。
- 结果复用不再返回"完成结果"，而是**照样登记作业**、执行体立刻把子代理树里已保存的输出写进
  各自正文并合成 `done`（`dispatchReusedJobs`）——省 token 的效果保留，回执形状不变。
- 每条作业的终态**按它自己的节点**判定（`batchNodeStates` 读 `plan_run` 结果 JSON 的
  `nodes[].{node_id,status}`）：best-effort 批次里一个兄弟失败、其余跑完时，失败那一行必须
  是 `failed`，不能被整批状态抹平。
- 作业面关闭（`limits.async_exec.enabled=false`）时**显式报错**，不静默退化成阻塞执行
  （与 `bash_bg` / `read_batch` 同一条纪律）。
- 工具描述与 `SubagentsContractDescription` 同步：调用只回句柄，产出走
  `job_manage(op=fetch, handle)`。

**为什么不是"默认异步、仍留阻塞开关"**：阻塞形态下模型没有下一次调用，`observe` / `kill`
没有入口——不是缺工具，是缺时机（`docs/arch/teamwork-leader-worker-architecture.md` §B.3）。
留一个半死的同步分支只会让"子代理"和"后台命令"继续长着两套形状。

### 2.2 `job_manage`：`handles` 一次管一批

- `jobManageInput` 增 `handles []string`（与 `handle` 合并去重，顺序保持）。
- `jobManager.FetchMany`（`op=fetch` + 多句柄）：先把预算摊在**等整批**上，再逐条取回增量。
  预算语义是**整批共用一条 deadline**（取各自 `wait_ms` 最大值后按同一套上限归一）——
  否则 8 条各等 60s 会把一次调用钉住 8 分钟。
- `jobManager.StatusMany`（`op=observe` + 多句柄）：一次看一批（只读，不推进游标）。
- `kill` / `done` **保持一条**：kill 会终止整批共用编排（一条影响 N 条），done 是"我确认
  收到这一条"的动作——两者都不该被数组悄悄放大。
- 单条与批量共用同一份实现（`fetchOne` / `polledPayload`），避免两条路径字段漂移。

## 3. 回归证据

```
go build ./...                                  # exit 0
go vet ./seelebridge/... ./application/... ./tui/... ./gui/... .   # exit 0（含测试编译）
go test ./seelebridge/tools/ -count=1
go test ./seelebridge/ -run "Fork|Subagent" -count=1
go test ./application/core/... -count=1
```

用例：

- `seelebridge/fork_job_helpers_test.go`：`forkRun`（派发 → 等终态 → 逐条取回）与
  `newAsyncTestRuntime*`（测试基座显式打开作业面——`RuntimeConfig` 结构零值是关，
  对齐"旧配置缺这段 = 关"的纪律）。
- `fork_tool_test.go` / `fork_smoke_test.go` / `fork_besteffort_test.go` /
  `fork_concurrency{,_repro}_test.go`：断言从"工具返回 completed 结果"改成"作业终态 +
  各自句柄的取回正文"。best-effort 用例现在**逐节点**判终态（失败的那个 failed、
  幸存的那个 done）。
- 两个 summary 窗口用例（2000 汉字不被截断 / 超窗附完整字数）改为**纯函数/节点级**：
  汇总节点不再进模型的返回，它是 summary 节点的性质。
- `seelebridge/tools/job_manage_batch_test.go`：批量取回整批交付恰好一次且终态即销项、
  整批共用预算（4 条在跑 + `wait_ms=1s` 不会被乘成 4s）、批量里带未知句柄报错、
  `observe` 批量只读不销项、`kill`/`done` 拒绝多条。

## 4. 已知边界（诚实标注）

- **异步关闭的部署失去子代理能力**：`fork_subagents` 只走作业面，`limits.async_exec.enabled=false`
  时它报错。出厂配置是 true；这是"关就是关"的直接后果，不是静默降级。
- **summary 节点仍在 DAG 里**（`start → N×agent → summary`）：它的输出是"某个子代理没有可复用
  摘要时"的整批兜底正文。单个子代理的完整产出按自己的句柄取回。
- **fork 门控跟着作业走**：`Runtime.ForkInFlight` 现在额外读"本会话仍在跑的 Kind=subagent 作业"
  （`Router.RunningSubagentJobsFor`）。不这样改，`ErrForkRunningChat`（fork 期间禁止同会话继续
  对话）会在派发返回的瞬间静默失效——子代理还在同一个会话里跑，门却开了。

