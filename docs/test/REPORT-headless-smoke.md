# Headless 真实运行冒烟报告（2026-09-07）

> 配套：`docs/2026-09-07-goal-domain-techleader/techleader-mvp.md`（goal Part II MVP 装配
> headless 调教接口后的验收冒烟）、`tmp/headless-smoke/README.md`（9-06 GUI headless 冒烟历史）。
> 本文档记录 **Goal + TechLeader Part II 的 headless 冒烟三科目与 race/pprof 分析**：
> 在真实 GUI headless 控制面（`gui/headless.go` HTTP RPC）+ 真实 DeepSeek API 上驱动
> `-race` / `-tags pprof` 两种二进制，验证 首提交/新会话/回复质量/工具链/竞态/性能与上下文载荷。

## 1. 被测对象与构建

| 构建 | 文件 | 说明 |
|---|---|---|
| race | `tmp/headless-smoke/seelex-race.exe`（~87MB） | `go build -race`，盯数据竞争 |
| pprof | `tmp/headless-smoke/seelex-pprof.exe`（~50MB） | `go build -tags pprof`，内置 `127.0.0.1:6060` 采样口（`SEELEX_PPROF_ADDR` 可覆盖） |

装配内容（本次会话 09-07 16:33–16:36 落盘并重新构建）：
`gui/headless.go` dispatch 新增 `CreateWorkspace / BindWorkspace / UnbindWorkspace` 三个 RPC，
支撑代码场景的 project-scope 绑定（与 9-06 版相比为新增工作区控制面）。

驱动 harness：`tmp/headless-smoke/scenario_test.go`（`TestRealAPIScenarios`，SMOKE_REAL=1 启用）。
本次会话修复 harness 三处缺陷后重跑：
1. CPU profile 抓取时机（原先在进程 kill 后必然 refused，改为对话活跃期并行采样，末尾兜底补帧）；
2. 报告/采样落盘路径相对 go test 包目录导致嵌套 `tmp/headless-smoke/tmp/...`（改为 repo 根绝对路径）；
3. race 构建场景对无 pprof 端口的目标不再产生 refused 噪音；新增 `SMOKE_CODE_TARGET` 支持
   code 场景在 race 二进制下复跑（收集“完整工具链 + race 检测”数据点）。

## 2. 场景与结果总表（2026-09-07 运行，真实 API）

产物：`tmp/headless-smoke/reports/scenarios-{45648,76160,40120,51760}.jsonl`（逐场景 JSONL，含轮次/工具/PerfStats）。
历史首轮（含路径缺陷）归档于 `tmp/headless-smoke/reports/archive-20260907-firstrun/`。

| 场景 | 构建 | wall | boot | rounds | 工具调用 | task | 末轮 reply | race | workspace | snapshot_bytes |
|---|---|---|---|---|---|---|---|---|---|---|
| race-probe（首提交 + get_time） | race | 3.2s | 0.30s | 7 | 2（get_time） | completed | 30 runes | 无告警 | – | 11.7K |
| math（高考导数恒成立） | pprof | 8.0s | 0.31s | 2 | 0 | completed | 940 runes | – | – | 13.6K |
| code（palindrome + 测试 + go test） | pprof | 50.2s | 0.30s | 35 | 22 | completed | 1052 runes | – | true | 56.3K |
| code（同上，race 复跑） | race | 54.1s | 0.30s | 38 | 24 | completed | 1110 runes | 无告警 | true | 95.9K |
| essay（美学/政治多轮长文） | pprof | 48.4s | 0.30s | 4 | 0 | completed | 432 runes | – | – | 29.2K |

要点：
- 全部场景 `timed_out=false`、`chat_error` 空、`final_task_status=completed`，无 CancelChat 触发（护栏未越界）。
- boot 恒定 ~0.30s（4 场景 303–305ms）→ 进程启动到 headless 探活稳定，无抖动。
- race-probe 额外跑过第二遍（`scenarios-40120.jsonl`，stderr 157B，仍无 DATA RACE），结论可复现。

### 2.1 冒烟1 · 数学题（race 探针 + pprof 数学）

- **race 探针**（首提交探测 + 新会话语义 + get_time 工具）：连续 2 次运行，7 轮消息、2 次
  `get_time` 全部 success、无 DATA RACE；回复含推理字段（`has_reasoning=true`）。
- **数学高考题** `f(x)=e^x − a·x − a ≥ 0 恒成立求 a 的范围`：直接作答 940 runes（含推导结构，
  全程无工具），wall 8.0s。**注**：答案正确性未机器校验（非本冒烟目标），质量指标为
  结构完整 + 长度合理 + 单轮收敛。

### 2.2 冒烟2 · 代码题（pprof 构建，真实工具链）

- code 场景在绑定仓库根 workspace（`workspace_bound=true`）下由模型自主完成：
  `glob→bash→read_file→bash→write_file(palindrome.go)→write_file(palindrome_test.go)→bash(go test)→task_complete`。
- 工具本地执行耗时全为亚秒级：glob 26–63ms、bash 283–443ms、read_file 0–2ms、write_file ~1ms、
  get_time ~0ms（`tool_calls` 记录，含重复计数为 UI 冗余，唯一调用以轮次为准）。
- 事后产物核对：`tmp/smoke-code/palindrome.go` 与 `palindrome_test.go` 均已落盘（notes 记录），
  `go test ./tmp/smoke-code/...` 通过（bash tool_result 1549 runes 输出），末轮汇报 1052 runes。
- CPU/内存采样：见 §4。

### 2.3 冒烟3 · 美学/政治探讨（多轮长文 + PerfStats 载荷增长）

- 两轮长文对话（1688 + 471 runes，均含推理），第二轮以“政治宣传美学边界 ≤400 字”约束收敛，wall 48.4s。
- PerfStats：`snapshot_bytes` 29.2K、`conversation_chars` 4.4K、`revision` 245；
  无 `truncated_outputs`、无归档（archived_bytes=0），`history_window=200` → 长文会话载荷受控、无截断。

## 3. 数据竞争分析（race 二进制 + goroutine 快照）

**结论：本轮冒烟路径无数据竞争、无死锁/goroutine 泄漏迹象。**

- race 二进制 2 个真实场景（首提交探针 ×2 + 完整工具链 ×1）stderr 均仅 157B、无
  `WARNING: DATA RACE`（jsonl `data_race=false`）。
- goroutine 快照 ×4（math/code 每场景 1 份，pprof 构建抓取）：每份恰 20 goroutine，
  18 个 `runtime.gopark`（I/O 等待）、1 采样协程、1 notetsleepg——数量恒定无堆积；
  `go tool pprof` 未现锁竞争/死锁栈。
- 覆盖边界说明：冒烟为单会话串行路径（并发会话切换/热切换的竞态由单元层
  `go test ./application/core/goal/... -race`（36 用例全绿）与仓库既有
  `session_*_race_test.go` 承担；9-06 GUI headless 冒烟曾暴露的 fork 恢复缺陷已由
  `e901a51` 修复并回归）。

## 4. 性能瓶颈分析（阶段耗时 + pprof top + 上下文载荷）

**结论：Go/GUI 主进程为 IO/网络绑定，本地无 CPU/内存热点；wall 时长主要由 provider 推理
与 SSE 流等待构成。**

### 4.1 阶段耗时拆解
- boot：恒定 ~0.30s（4 场景），进程启动开销可忽略。
- code（pprof）wall 50.2s ≈ 工具本地执行 <1s（22 次，见 2.2）+ LLM 推理/流等待 ~46s +
  快照轮询；race 版 54.1s（`-race` 使进程内检测开销小幅放大本地段）。
- 轮次间隙（rounds delta_ms）显示模型决策间隙 2–10s 为主（如 `get_time` 前 8979ms、
  `task_complete` 前 5848ms），非本地处理。

### 4.2 pprof top（活跃期 6s 采样，进程存活时抓取）
- code 活跃期：Total samples 3.73s/6s；`runtime.cgocall` flat **97.05%**，cum 路径
  `bytes.(*Buffer).ReadFrom` **63.27%**（HTTP SSE 响应体读取）+ `agent.Dispatch` / `bridge.RegistryRuntime.Dispatch`
  **33.78%**（LLM 流读取调用链）→ 采样窗口几乎全落在等待 LLM SSE 数据上；本地 json/map 均 <1%。
- math 活跃期：Total samples 180ms/6s（2.99%）——纯问答等待期 CPU 占用极低；top 分散于
  `runtime.stdcall1`、TLS 读、json 解码零星开销，无热点。
- 采样文件：`reports/profile_seconds_6-165522-76160.pprof`（code）、
  `profile_seconds_6-165509-76160.pprof`（math）；空闲期对照
  `profile_seconds_6-165{015,113}-45648.pprof`（采样率 0.17%，佐证等待为主）。

### 4.3 上下文/载荷增长（PerfStats）
| 场景 | snapshot_bytes | conversation_messages | conversation_chars | revision | 截断/归档 |
|---|---|---|---|---|---|
| race-probe | 11.7K | 7 | 392 | 26 | 0/0 |
| math | 13.6K | 2 | 1531 | 88 | 0/0 |
| code (pprof) | 56.3K | 44 | 36.2K | 249 | 0/0 |
| code (race) | 95.9K | 38 | – | – | 0/0 |
| essay | 29.2K | 4 | 4.4K | 245 | 0/0 |

载荷随轮次/工具链线性受控增长（code 场景工具结果回读贡献大头），`history_window=200`、
`truncated_outputs=0` → 上下文治理（压缩/窗口策略）在真实载荷下未触发截断，revision 持续
推进说明快照/事件流正常。

## 5. 复现命令

```powershell
# 1) 构建两种二进制（仓库根）
go build -race  -o tmp/headless-smoke/seelex-race.exe .
go build -tags pprof -o tmp/headless-smoke/seelex-pprof.exe .

# 2) 跑真实 API 冒烟（4 场景；需 config/accounts.yaml 内 DeepSeek 凭据）
$env:SMOKE_REAL=1
go test ./tmp/headless-smoke -run TestRealAPIScenarios -v -count=1 -timeout 8m

# 可选 env：
#   SMOKE_PPROF_TARGET / SMOKE_RACE_TARGET / SMOKE_CODE_TARGET  目标二进制（默认见上表）
#   SMOKE_CODE_TARGET=seelex-race.exe  → code 场景在 race 下复跑（多一个 race 数据点）
#   SMOKE_IDLE_BUDGET_SEC=120          Submit 单轮空闲护栏（默认 120s，越界自动 CancelChat）
#   SMOKE_KEEP_STORE=1                 保留临时 session store 现场

# 3) profile 分析
go tool pprof -top tmp/headless-smoke/reports/profile_seconds_6-165522-76160.pprof
go tool pprof -top tmp/headless-smoke/reports/goroutine-165601-76160.pprof
```

## 6. 结论与残余风险

- **通过**：真实 API 冒烟三科目全部收敛（completed、无超时/错误）；race 二进制路径无竞态；
  Go 侧无性能热点（IO 绑定）；上下文载荷受控。
- **残余风险**：
  1. math 答案正确性与 essay 观点质量未做机器断言（人工可复查 `scenarios-*.jsonl` 的回复全文
     在临时 store 已清理时不可回看，需 `SMOKE_KEEP_STORE=1` 保留）。
  2. race 冒烟仅覆盖单会话串行主路径；并发/热切换竞态依赖单元层 `-race` 套件。
  3. `tool_calls` 记录的 ms 含重复计数（UI 消息冗余字段），精确耗时以轮次 delta 为准。
