# 折叠处厚摘要开关：出厂打开 + 开关两臂补钉子

- 日期：2026-09-29
- 范围：`limits.context_compaction_summary`（`config/seelex.yaml`）+ 该开关的离线用例
- 上游：`docs/2026-09-29-context-compaction-fold-review.md`（P1-B 的残留：接线后"开就生效"，
  但全仓没有一条测试构造 `Enabled: true`）、`docs/devlog/2026-09-29-assembly-fold-frame-push-wiring.md`
- 结论：**出厂配置打开**（代码零值仍是关）；开关两臂与"出厂打开"这一行本身都有带牙用例

---

## 1. 打开的是哪一层

推帧接线（`application/core/context_runtime` 的 `pushCompactionFrame` →
`seelebridge/runtime_compaction_index.go` 的 `PushCompactionFrame` → `MainCompactionDAG`）之后，
`context_compaction_summary` 由"打开也不生效"变成"开就生效"，于是它从"留着开关的休眠能力"
变成"一个真的可以选择的产品口径"。本次选择：**打开**。

口径分两层，必须写清，否则下一个人会把它读成同一件事：

| 层 | 状态 |
|---|---|
| 代码零值（`Limits` 零值 / 整块缺失 / `enabled: false`） | **关**：恒本地确定性折叠（`summary_source=local`），一次付费调用都不发 |
| 出厂文件（`config/seelex.yaml`） | **开**（2026-09-29 起 `enabled: true`） |

打开的代价与收益不变，仍是配置块里那段注释：每次装配层折叠多一次**无人值守的付费调用**
（溢出区超过 `input_tokens` 片预算时按片多次），换来帧 Chapter 2 从"任务台账的元数据投影"
变成真摘要（「错误与修复 / 待办 / 下一步」不再恒 `(none)`），这也是 `search_history`
确定性词法初筛唯一能拿到别名的入口。

## 2. 钉的是什么

`seelebridge/runtime_compaction_switch_test.go`（三条用例，两臂都走**生产入口**，不拿桩替换
被测对象）：真 `Runtime` + 绑定到当前会话的 `SessionContextStore` + `injectScriptedCompleters`
的确定性 completer（无网络；QuickChat 与 `seelexCompressor` 同一条构造路径，不第二次装配）。

| 用例 | 断言 |
|---|---|
| `TestCompactionSummarySwitchClosedKeepsLocalFold` | `compactionSummarizer() == nil`（`chapter2Node` 的显式判据）、completer **零**调用、回执与栈顶 `summary_source=local`，且帧里没有模型回复 |
| `TestCompactionSummarySwitchOpenReplaysPrefixIntoStackFrame` | 摘要器非 nil、**恰好一次**调用、请求形态是前缀重放（会话 system 原字节 → 历史字节原样 → 可见工具面 → 固定压缩指令尾巴）、回执与栈顶 `summary_source=replay`、帧正文原样嵌入摘要 |
| `TestShippedCompactionSummarySwitchShipsOpen` | `seelexctx.LoadLimits("config/seelex.yaml")` 的 `ContextCompactionSummary.Enabled == true`——"打开"只存在于配置这一行，没有这条用例，静默回滚不会有任何红 |

请求形态那几条断言不是装饰：打开的收益全押在**前缀缓存**上，system 被重拼、历史被改写、
工具面不同，都会让这次调用从"几乎全命中缓存"变成"付全价换不来前缀"。

## 3. 判别力（变异验证，跑完已还原）

| 变异 | 期望红 | 实测 |
|---|---|---|
| 出厂配置改回 `enabled: false` | `TestShippedCompactionSummarySwitchShipsOpen` | ✅ 红（"出厂配置应打开折叠处厚摘要"） |
| `compactionSummarizer()` 忽略开关（恒造摘要器） | 关臂 | ✅ 红（"开关关闭时摘要器应为 nil，实际 `*seelexctx.quickChatPrefixReplaySummarizer`"） |
| `compactionSummarizer()` 恒返回 nil | 开臂 | ✅ 红（"开关打开时摘要器不该为 nil"） |

## 4. 验证证据

```powershell
gofmt -l seelebridge/runtime_compaction_switch_test.go          # 无输出
go vet ./seelebridge/                                            # 干净
go test ./seelebridge/ -run Switch -count=1 -v                   # 三条全 PASS
go build ./...                                                   # 干净
go test ./seelebridge/... ./seelexctx/... ./application/core/... ./internal/adapters/... -count=1
```

## 5. 遗留（如实记账）

- **P1-C 仍未闭环**：`-tags compactlive` 三件套的真实 API 复跑需要凭据，本机无凭据、未跑。
  本次改动把出厂开关打开，等于让这条路径在真实环境里**每次折叠都会付费**——复跑的必要性
  比上一批更高（开关两臂的离线证据只证明接线正确，不证明真实 provider 下摘要质量与耗时）。
- 分片重放链（`input_tokens` 片预算 → `SummarizeChunkPlan`）仍只有 fake 单测；本次用例刻意用
  小溢出区，钉的是开关而不是分片。
