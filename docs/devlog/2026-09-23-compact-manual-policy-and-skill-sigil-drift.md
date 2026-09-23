# 手动压缩不再被阈值挡 + 服务发现四处漂移（2026-09-23）

> 触发：用户在真实会话里连打三次都没能把上下文压下去，并把几条 system 文案贴了回来：
>
> 1. `当前会话没有进行中的任务执行，未做压缩；上下文接近上限时会自动压缩（硬阈值 90%）。`
> 2. `未知命令: compact_context。输入 /help 查看可用命令。`
> 3. `当前上下文估算 129409 tokens，未达压缩阈值 118962，无需压缩（接近上限时框架会自动压缩）。`
>
> 第 3 条自相矛盾（129409 > 118962 却说"未达"），第 2 条是死路（命令名与工具名不一致且无提示），
> 第 1 条在本会话（129k tokens、刚冷加载）场景下等于"什么都不做"。
> 本文记：根因、改法、以及顺带做的输入前缀服务发现审计。

## 1. 根因：三个不同的量被当成一个，外加一条不该有的前提

### 1.1 判据量与展示量混用（`context_compact.go` 的 default 分支）

旧代码：

```go
default:
    return ContextCompactionResult{
        EstimatedTokens: outcome.EstimatedTokens, // = state.TokenAudit.EstimatedPromptTokens
        Note: fmt.Sprintf("当前上下文估算 %d tokens，未达压缩阈值 %d，无需压缩（…）。",
            outcome.EstimatedTokens, outcome.SoftThreshold),
    }
```

- 决策用的量是 `rawTokens`（`prepareExecutionContextFor` 里"全量累积 context / 引擎缓存峰值"的请求估算）；
- 展示的量是 `state.TokenAudit.EstimatedPromptTokens` = **装配后估算**（真正发给 provider 的大小）；
- 两者可以一大一小（保留段 + 摘要 + plan + 当前输入都只出现在装配后那一侧），于是"未达阈值"这句话可以配
  一个**比阈值还大**的数字——文案断言了一个它从未比较过的关系（硬编码，不是判断）。

### 1.2 显式路径仍以软阈值为前提

`forceCompact` 当初只绕过"每个 progress epoch 只压一次"的节流：

```go
newCheckpoint := (rawTokens >= budget.SoftThreshold || hardCompact) && (options.forceCompact || …)
compacting    := rawTokens >= budget.SoftThreshold || hardCompact
```

用户明确要求压缩时，"还没到线"不是理由——这正是"手动调用不该看上限"的直接来源。

### 1.3 "没写记录"被当成"没压缩"

`RecordContextCompactionLocked` 要求 `state.Status == StatusRunning`，回合收尾后必然 false；
而旧 `CompactContextNow` 用"记录数没增加"反推结论 → 报 `CompactBelowThreshold`。
实际上 transcript **已经折叠**、引擎历史已换成有界 checkpoint、checkpoint 也已按会话落盘。
于是用户看到的是"无需压缩"，而上下文其实被压了（或反过来说：他说不清到底发生了什么）。

### 1.4 没有执行纪元 → 直接拒绝

`prepareExecutionContextFor` 在 `state == nil || state.RequestID != requestID` 时按设计直接返回；
冷加载 / 刚 `/clear` 的会话没有 `RequestID`，于是 `/compact` 只能回"没有进行中的任务执行"。
本轮不伪造纪元（伪造会把"有人在跑这个会话"写进状态），改为**登记**（见 §2.3）。

## 2. 改法

### 2.1 折叠判据加第三条（显式）

```go
fold := rawTokens >= budget.SoftThreshold || hardCompact || options.forceCompact
newCheckpoint := fold && (options.forceCompact || hardCompact || state.CompactedEpoch != state.ProgressEpoch)
compacting := fold
```

显式路径的硬前提只剩一条：**该会话有匹配当前 request 的执行纪元**。

### 2.2 结果面改成"事实面"

- `context_runtime.compactDecision`（出参，`prepareOptions.decision`）回填 `Folded / Recorded /
  Version / ComparedTokens / AssembledTokens / SoftThreshold / HardThreshold / NoEpoch`；
- `CompactResult` 四种分类：`compacted`（折叠 + 落记录）/ `folded_without_record`（折叠了，但
  回合已收尾 → 记录不产生）/ `scheduled`（无纪元，已登记）/ `below_threshold`（兜底）；
- `ContextCompactionResult` 的文案由事实拼出：判据量、软/硬阈值、装配后估算各说各的名字，
  不再出现"用 A 的数字说 B 的结论"。

### 2.3 无纪元 → 登记，下一条消息兑现

`Coordinator.ScheduleForceCompact(sessionID)` 记一件待办（独立 mutex，不与 `Core.ViewMu` 嵌套）；
`prepareExecutionContextFor` 在最前面取走它并转成显式路径 → 下一条消息**先压后发**。
`/compact` 在无纪元会话上回："已登记：下一条消息组装上下文前立即压缩"。

## 3. 顺带做的服务发现审计（用户问"是不是有问题"）

查了四处"名字 → 行为"的映射：命令注册表（`commands.All()`，`/help` 动态生成）、建议面板
（`Suggestions`，`/` 聚合命令 + 工具 + Skill）、Skill（`$`）、Plugin（`#`）、输入路由
（`input_router` 五条 route）。确认四处漂移，全部有代码证据：

| # | 现象 | 证据 | 处置 |
|---|---|---|---|
| D1 | `/` 面板把**工具**（如 `compact_context`）列为候选、选中即"未知命令" | `completion.go` `Suggestions` 的 `/` 分支 append `toolSuggestions()`；`input.go` `submitCommand` 只查 Skill 与命令，无工具分支；`input_router/router.go` 五条 route 里没有工具 | 未知命令提示区分工具（"模型侧工具，不能从输入框执行"）并指向同名命令入口；`/help` 写明同一条口径；文档 `docs/gui/modules/shell-and-interactions.md` 补"列出 ≠ 可执行" |
| D2 | system 里的技能目录提示让模型教用户 `#<name>`（`#` 现在是插件前缀） | `prompt_layer/skill_catalog.go:19` 写死 `#<name>`；`4594e1e`（2026-09-17 一字符一含义）改了 sigil 表与 completion.go，**没改这句** | 前缀改为调用方注入（`prompt_layer.Deps.SkillSigil` ← `core.SigilSkill`），单测 + 端到端断言钉住 |
| D3 | `plugins/default/plugin.md` 写着"通过 `#plan` 注入默认 Plan Skill"，README 还叫它"Plan Plugin" | 两处文档；`manual_smoke_test.go` 也跟着 `#goal`/`#plan`（该冒烟自 2026-09-17 起就点不到技能） | 文档与冒烟改 `$plan`/`$goal`；新增守卫 `TestPluginDocsDoNotUsePluginSigilForSkills`（插件文档不得用 `#<skill>`，对历史文本 `#plan` 会红） |
| D4 | 根包分卷索引自 2026-09-17 起**静默失效**（生成器拒绝刷新） | `scripts/gen_core_readme_index.py` 的 `verify_coverage` 报 `durable_queue_wire_test.go` 未归卷 → SystemExit；README-* 索引因此停在旧版本 | 归入 `input` 卷并刷新索引（顺带把 `041dc22` 之后新增文件补进索引） |

## 4. 有牙证明（红 → 绿）

| 测试 | PROBE（改回旧行为） | 结果 |
|---|---|---|
| `TestCompactManualFoldsBelowThreshold` | `fold` 去掉 `\|\| options.forceCompact` | 红：`显式压缩必须在低阈值下也折叠并留记录` |
| `TestCompactCommandRegisteredAndSharesPath` | 同上 | 红：`/compact 提示 = "压缩判据未命中…"` |
| `TestCompactContextWithoutTaskExecutionSchedulesNextAssembly` | 同上 | 红：`登记的强压未兑现：kept=2 want<2` |
| `TestCompactManualReportsFoldWithoutRecord` | 旧分类（按记录数反推） | 该分支旧代码文本即 `未达压缩阈值`（已读代码确认），断言禁止该串 |
| `TestPluginDocsDoNotUsePluginSigilForSkills` | 把 `plugin.md` 改回 `#plan` | 红：`plugins\default\plugin.md 用 \`#plan\` 引用 Skill…` |

## 5. 验证（本机、本轮）

```text
go build ./...                                   ok
go vet ./...                                     ok（含 -tags redprobe ./sessionstore）
go test ./application/core/... -count=1          ok（14 个包；新增/改写用例见 §4）
go test . -run TestPluginDocsDoNotUsePluginSigilForSkills   ok（PROBE 见 §4）
python scripts/gen_core_readme_index.py           ok（此前 SystemExit）
gofmt -l / git diff --check                       干净
```

## 6. 未做 / 待办

- **`/` 面板的工具候选是否应当“可执行”**：本轮只把死路改成"说清楚 + 指路"。若希望
  `/read_file x` 这类真的直接执行工具，需要新语义（工具调用协议 + 权限门 + 参数解析），
  是产品决定，不在本轮夹带。
- 压缩记录的 `Status != Running` 限制本身是否该放宽（收尾后也留记录）没有动：本轮只保证
  "折叠了就说折叠了"，不改变记录的产生条件。
- 历史归档文档（`docs/2026-07-*`、`docs/2026-08-22-*`）里的旧前缀描述保留原样——那是当时的记录，
  不做追改；只有会进入 system prompt / 用户提示 / 插件文档的文本做对齐。
- **既有 flake（未修，不属于本轮改动）**：`repro_session_race_test.go` 的
  `TestWorkspaceSwitchConcurrentWithBackgroundPersist` 在结束前不等应用 idle，
  `t.TempDir` 清理撞上后台落盘时失败（`TempDir RemoveAll cleanup: … directory is not empty`）。
  本轮全量跑里出现过两次，随后 4 次复跑全绿、HEAD 同跑法 3/3 绿——按"负载敏感的既有测试卫生问题"
  记在待办，不在本轮夹带修（要修就是"收尾等 idle"，与压缩/前缀两条线无关）。
