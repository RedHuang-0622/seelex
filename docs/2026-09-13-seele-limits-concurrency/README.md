# Seele issues：限流与并发「路数装配」

一次性工作包。**已实现并已验证**（含真实 provider 冒烟 + seelex 联调）。

- 目标：在 Seele 里做出限流（RPM/TPM）与并发（加权在途）的可装配层，
  参数可用 Set 方法在运行时调整；再用 seelex 的 `replace` 指向本地 Seele 联调，
  产出功能测试、边界测试与真实 API 冒烟。
- Seele 仓库：本地检出（`G:\Program\go\Seele`，分支 `main`），改动**未提交**。
- seelex 侧：联调用的 `replace` 已**还原**（见 §6），生产仍依赖 `Seele v0.1.3`。

---

## 1. 交付物

### Seele 新包 `limits/`（限流与并发的路数装配）

| 文件 | 内容 |
| --- | --- |
| `params.go` | `Params` 参数模型（YAML/JSON tag 齐全）、`RetryPolicy`、`DefaultParams`、`Normalize`/`Validate`、`Duration`（同时接受 `30s` 与 `30` 秒） |
| `mutators.go` | `WithEnabled/WithConcurrency/WithRate/WithBurst/WithRetry/WithQueueTimeout/WithImageWeight/WithPerKey`（纯函数，值语义） |
| `bucket.go` | 单调令牌桶：运行时改速率/容量、透支记账、超容量吸收 |
| `gate.go` | `Gate`：加权并发 + 请求桶 + 令牌桶 + 排队预算；`Acquire`/`Permit`/`Settle`/`Stats` + 全部 Set 方法 |
| `assembly.go` | `Assembly`：key → gate 装配、`Wrap`/`WrapFor`/`WrapAll`/`WrapComplete`/`WrapAllComplete`、`SetParams`/`SetKeyParams`/各维度 Set、`Snapshot` |
| `wrapper.go` | 装饰器：准入 → 调用 → usage 结算 → 429/Retry-After 重试分类；`CompleteOnly` 窄接口适配 |
| `retry.go` | `StatusOf`/`RetryAfterOf`/`IsRetryable`/`RetryDelay`（可单测，抖动可注入） |
| `errors.go` | `ErrInvalidParams`、`ErrConcurrencyExceeded`、`ErrRateLimited`、`ErrQueueTimeout`（均可 `errors.Is`） |
| `cost.go` | `Cost`/`CostEstimator`/`DefaultEstimator`（CJK≈1 token/字、ASCII≈1/4），图片 token 位已在 `Cost.Images` 预留 |
| `README.md` | 与 `accountpool` 的分工、公共接口表、参数语义、准入顺序、边界行为表 |

### Seele 其他改动

| 文件 | 改动 | 原因 |
| --- | --- | --- |
| `types/httperror.go`（新增） | `HTTPStatusError{StatusCode, Body, RetryAfter}`：`Error()` 文本与旧的 `fmt.Errorf("HTTP %d: %.512s", ...)` **逐字节相同**，`RetryAfter` 解析秒数/HTTP-date | 让重试层能分类失败（`errors.As`）而不必匹配字符串 |
| `agent/core/api/client.go`（补丁 2 处） | ① 非流式 `Complete` 增加非 200 检查；② `openStream` 改用 typed 错误 | **非流式路径原本完全不检查状态码**：429 会被 `ParseResponse` 当成协议解析失败，重试层永远拿不到状态码与 `Retry-After` |

### 测试

| 文件 | 覆盖 |
| --- | --- |
| `limits/params_test.go` | 派生容量、非法参数表、YAML 往返、修改器值语义 |
| `limits/bucket_test.go` | 假时钟下的补充/封顶/不限/透支/改参夹紧 |
| `limits/gate_test.go` | 并发上限、加权图片、`QueueTimeout=0` 立即拒绝、排队超时、调用方取消、速率/令牌等待、超容量吸收、运行时 Set、`Settle` 结算与幂等、40 并发压测（`goleak` 检查无泄漏） |
| `limits/assembly_test.go` | per-key/共享 gate、全局 vs 单 key Set、无效 Set 不改现状、注入时钟驱动桶、估算器覆盖、`WrapComplete` 真流式/回退单块 |
| `limits/wrapper_test.go` | 三次尝试重试、用尽即返回、不重试 4xx、`Retry-After`、取消优先、被拒不打 provider、流式已输出不重放、事件流同规则、旁路仍重试 |
| `types/httperror_test.go` | 文本兼容、512 字节截断、可重试状态集合、`Retry-After` 两种格式 |
| `test/limits_live_smoke_test.go`（tag `livesmoke`） | 真实 provider 冒烟 6 个场景 |

---

## 2. 参数模型（默认值）

| 键 | 默认 | 语义 |
| --- | --- | --- |
| `enabled` | true | 总开关；关闭时放行但仍计数，**重试分类照旧生效**（重试是传输层关切） |
| `max_concurrency` | 4 | 加权在途上限；**0 = 不限** |
| `image_weight` | 0.5 | 每张图折算的在途权重；0 = 不计权重；负值非法 |
| `requests_per_min` | 60 | 请求速率；0 = 不限 |
| `tokens_per_min` | 120000 | 令牌速率；0 = 不限 |
| `burst` / `token_burst` | 派生（10s 量） | 0 = 按速率派生；显式设置则钉住 |
| `queue_timeout` | 30s | **整个准入**的预算；**0 = 不排队，立即失败** |
| `retry.max_attempts` | 3 | 含首次，1 = 不重试 |
| `retry.base_delay` / `max_delay` / `jitter` | 1s / 60s / 0.2 | 抖动指数退避；`Retry-After` 受 `max_delay` 夹住 |
| `retry.ignore_retry_after` | false | 默认尊重 provider 的 `Retry-After` |
| `per_key` | true | true = 每个 key（账号）独立 gate；false = 全部共享 `default` |

**原则**：`Normalize` 只补派生值，绝不替调用方发明上限，也不静默修正负数
（负数一律 `ErrInvalidParams`）；推荐值只在 `DefaultParams` 里。

## 3. 准入顺序与失败语义

```
Acquire(ctx, cost)
  1) 加权在途槽位   ← 先占本地槽位，避免「排队时白扣共享令牌」
  2) 请求速率桶     ← 失败则退还槽位
  3) 令牌速率桶     ← 失败则退还槽位 + 退还请求令牌
```

- 失败一律可 `errors.Is`：`ErrQueueTimeout` 统一包住 `ErrConcurrencyExceeded`/`ErrRateLimited`；
  调用方自己的 ctx 取消/超时**原样透传**（绝不伪装成排队超时，也绝不重试）。
- 单次请求 token 估算 > 桶容量：**吸收为透支**（后续请求按速率补齐）并累加 `Stats.Oversized`，
  不会永久排队；`Set` 时会夹紧负水位（即债务被清）。
- 流式：已吐出增量后失败**不重试**（避免重复输出）；流式按**估算**计费（Seele 当前 SSE 不暴露 usage）。

## 4. 验收记录（全部实跑）

### 4.1 Seele 单元/边界测试

```
go -C G:\Program\go\Seele test ./limits/... ./types/... -race -count=1
ok  github.com/RedHuang-0622/Seele/limits   5.7s
ok  github.com/RedHuang-0622/Seele/types    3.5s
gofmt -l（新文件）→ 空
go vet ./limits/... ./types/... → 空
go build ./... → 0
```

测试发现并修掉的真实缺陷（都是先写测试再暴露的）：
① `QueueTimeout=0` 时槽位等待会**死等到 ctx 超时**（挂 10 分钟）→ 改为不排队立即拒绝；
② 负 `queue_timeout`/`burst` 被 `Normalize` 静默钳位 → 改为 `Validate` 拒绝；
③ `Acquire` 先归一化再校验，导致负 cost 被当成 0 放过 → 改为先校验。

### 4.2 真实 provider 冒烟（Seele 侧，deepseek-v4-flash）

```
$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
go -C G:\Program\go\Seele test -tags livesmoke ./test/... -run TestLiveLimitsSmoke -count=1 -v
--- PASS: TestLiveLimitsSmoke (7.24s)   6/6 子场景通过
```

| 场景 | 观测 |
| --- | --- |
| 真实两轮调用 + usage 结算 | 回复均为 `"pong"`；usage 43/82/125 与 43/23/66；`estimated=36 actual=191 in_flight=0 peak=1` |
| 速率限流可见 | `rpm=60 burst=1` 时第二次调用耗时 **1.04s**，`RateWaited=1` |
| 并发上限生效 | 两个并发真实请求 `peak_in_flight=1`、`queue_wait=753ms`、两条都成功 |
| 边界：槽位满 + `QueueTimeout=0` | `ErrQueueTimeout` 且**立即返回**（未打 provider） |
| 边界：真实 HTTP 429 + `Retry-After: 1` | 走通 api 客户端 → typed 错误 → 重试 1 次 → 返回可分类 429；服务端命中 2 次 |
| 边界：连接失败 | 判定为可重试，`in_flight=0`（无槽位泄漏） |

### 4.3 seelex 联调（replace 到本地 Seele）

```
go mod edit "-replace=github.com/RedHuang-0622/Seele=G:/Program/go/Seele"
go build ./...                                  # 0
go test ./seelebridge/... -count=1              # 20/20 包全绿
go test . ./application/... ./internal/... ./seelexctx/... ./sessionstore/... -count=1   # 33/33 包全绿
$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
go test -tags limitslive ./seelebridge/ -run TestSeelexLimitsLiveSmoke -count=1 -v
--- PASS: TestSeelexLimitsLiveSmoke (2.63s)
```

联调冒烟（`seelebridge/limits_live_smoke_test.go`，tag `limitslive`）验证的是**装配形状**：
seelex 自己的 `config.LoadTolerant` + `account.ClientFor` → `limits.Assembly.WrapComplete`
逐账号装饰 → `accountpool` 租约 → 真实调用：

| 子场景 | 观测 |
| --- | --- |
| 并发真实调用走两层装配 | 两条 `"pong"`；`admitted=2 completed=2 peak=1 actual=277`（真实 usage 结算） |
| 运行时 Set 在装配后生效 | 全局 `SetConcurrency(3)`/`SetRate(120,120000)`/`SetQueueTimeout`/`SetRetry` 生效；`SetKeyParams` 单账号覆盖为 5/240；`SetConcurrency(-1)` 报 `ErrInvalidParams` 且不改变现状 |
| 边界：零 API 成本快速拒绝 | 槽位占满 + `QueueTimeout=0` → `ErrQueueTimeout`，耗时 0s |

**联调暴露的接口问题（已修）**：seelex 的 `account.ClientFor` 返回的是
`agent.Completer`（只保证 `Complete`），而 `limits.WrapFor` 要求 `types.ChatCompleter`。
修法是新增 `CompleteOnly` + `WrapComplete`/`WrapAllComplete`：具体值实现完整接口时走**真流式**，
只有真缺流式能力才回退为「一次 `Complete` + 单块回调」（与 Seele `agent.NewWithComponents`
的 `composedClient` 行为一致）。这样调用方不必放宽自己的接口。

### 4.4 预存在问题（非本次改动引入）

Seele `test/TestTraceReal` 失败：`HTTP 401 ... Your api key: ****b086 is invalid`。
它读取 `config/account-openai.yaml`（本地陈旧凭据，仅在文件缺失时才 skip），
且断言必须有非空回复——无论有没有本次补丁都无法通过；补丁只是把「JSON 解析失败」
换成更准确的 `HTTP 401`。**结论：环境凭据问题，与本次改动无关。**

## 5. 改动清单（Seele 本地检出，未提交）

```
 M agent/core/api/client.go        # 非流式状态检查 + typed 状态错误（+6/-1）
?? limits/                         # 新包（9 个源文件 + README，62 个测试函数）
?? types/httperror.go
?? types/httperror_test.go
?? test/limits_live_smoke_test.go  # tag: livesmoke
```

Seelex 侧只有两处新增，`go.mod` **未被改动**（联调用的 replace 已还原）：

```
?? seelebridge/limits_live_smoke_test.go   # tag: limitslive
?? docs/2026-09-13-seele-limits-concurrency/
```

迭代用的脚手架留在 `seelex/.seelex/stage-limits/`（该目录被 `.gitignore` 忽略）：
其中是上述 Seele 文件的作者副本 + `apply.go`。重新落盘到 Seele 检出的命令：

```powershell
go run ./.seelex/stage-limits/apply.go                      # 默认 G:\Program\go\Seele
go run ./.seelex/stage-limits/apply.go -root <其它检出路径>   # 幂等，可重复执行
```

只需在 Seele 仓库继续演进时（源以 Seele 检出为准），可以直接删掉该脚手架目录。

## 6. 联调步骤与还原

```powershell
# 开启联调
go mod edit "-replace=github.com/RedHuang-0622/Seele=G:/Program/go/Seele"
go mod tidy    # 可选

# 冒烟
$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
go test -tags limitslive ./seelebridge/ -run TestSeelexLimitsLiveSmoke -count=1 -v

# 还原（保持 seelex 构建可移植：其它机器没有本地 Seele 检出的路径）
go mod edit "-dropreplace=github.com/RedHuang-0622/Seele"
```

`seelebridge/limits_live_smoke_test.go` 带 `//go:build limitslive`，默认构建/测试**不编译**它，
所以带着 `replace` 或去掉 `replace` 都不影响常规 CI。

## 7. 后续建议（未做）

1. **Seele 出 tag（如 v0.1.4）后**，seelex 才能正式依赖 `limits`；在那之前生产链路不应引用新包。
2. **接进 seelex 运行时**：`seelebridge` 的账号装配处（`account.RegisterAccounts`）按账号
   `WrapComplete`，参数从 `config` 的账号段读取——注意 Seele 旧 `AccountEntry` 里已有
   `max_rpm`/`max_concurrency` 字段（`max_rpm` 现标 Deprecated），可作为迁移来源。
3. **流式 TPM 精确计费**：需要 Seele 在 SSE 末帧暴露 usage（当前 `completeStreamInternal`
   不返回 usage），或在事件流里由产品侧调用 `Permit.Settle`。
4. **与图片/多模态联动**：`Cost.Images` 与 `Params.ImageWeight` 已就位，
   等 G13 的 `Message.Parts` 落地后，`DefaultEstimator` 是唯一需要补 tile 计价的地方。
