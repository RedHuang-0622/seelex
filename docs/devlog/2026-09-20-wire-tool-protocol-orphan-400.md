# wire 出口的 tool 配对协议：孤儿结果不再发到 provider（2026-09-20）
> 日期: 2026-09-20 | 范围: `seelexctx/history_safety.go`、`seelexctx/assembler.go`、
> `seelebridge/node/coordinator.go`、`application/core/history_safety.go`、
> `seelexctx/wire_protocol_safety_test.go`（新）、
> `seelebridge/node/coordinator_tool_protocol_test.go`（新）、
> `application/core/history_safety_test.go`
> 现象: `session loop 15 : seelebridge: stream with account "agent-1": ChatClient stream:
> HTTP 400: {"error":{"message":"Messages with role 'tool' must be a response to a preceding
> message with 'tool_calls'", ...}}`；另一条现场是 `goalplan-1` / loop 22。两条账号角色不同
> ——`agent-1` 是主会话、`goalplan-1` 是 `_` 前缀分支（`RoleForPlanBranch` → `RoleGoalPlan`）
> 的节点子代理会话，**两条走的是两个不同的请求装配出口**，同一条协议在两个面上都被触发。

## 一、这条 400 到底是什么

provider 校验的不是"历史里存在配对"，而是**每条 `role=tool` 消息必须紧跟那条携带其
`tool_call_id` 的 `assistant` 消息**（相邻结果块内，且同一 `call_id` 不许出现重复结果）。
只要历史里出现下面任一种形状，整次请求 400、会话循环当场中断：

| 形状 | 历史长相 | 为什么被拒 |
|---|---|---|
| 孤儿结果 | `… → user → tool(ghost) → …` | 没有任何 assistant 宣告过 `ghost`，这条 tool 没有可回应的前一条消息 |
| 乱序结果 | `assistant(tool_calls c1) → user → tool(c1)` | 声明与结果被 `user` 隔开，结果不在声明的相邻结果块内 |
| 重复结果 | `assistant(tool_calls c1) → tool(c1) → tool(c1)` | 同一 `call_id` 两条结果 |

## 二、复现（先把形状钉死，再改代码）

新增 `seelexctx/wire_protocol_safety_test.go`：`assertProviderToolProtocol` 复刻 provider
的校验规则（声明行 +1 .. +len(tool_calls) 内、无重复），四条用例 + 一条现场链路复现。
改代码前全部 **RED**：

```text
--- FAIL: TestPrepareReplaceHistoryDropsUnownedOrphanToolResult
    msg#2 role=tool tool_call_id="ghost" 没有前一条声明它的 assistant → provider 400 …: [user → assistant → tool(ghost) → user]
--- FAIL: TestPrepareReplaceHistoryRealignsMisorderedToolResult
    msg#3 role=tool tool_call_id="c1" 不在声明行 msg#1 的相邻结果块 [2,2] → provider 400: [user → assistant(tool_calls:c1) → user → tool(c1) → assistant]
--- FAIL: TestAssemblerSanitizesWorkingHistoryToolProtocol
    msg#2 role=tool tool_call_id="ghost" 没有前一条声明它的 assistant → provider 400 …
--- FAIL: TestControllerWindowProjectionKeepsProviderToolProtocol
    msg#1 role=tool tool_call_id="ghost" 没有前一条声明它的 assistant → provider 400: [user → tool(ghost) → user → assistant]
```

最后一条就是现场那条链路：控制器窗口投影**刻意**保留窗口内的非单元消息（孤儿 tool 行
随窗口保留，`projectHistory` 的审计 R3 口径 ⇒ 单元切分时孤儿不构成单元），于是孤儿
可能正好落在**投影后历史的最前面**（`msg#1`，紧跟压缩帧块）——这不只是"顺序不好看"，
是请求的第一条正文消息就是一条无人宣告的 tool 结果。

## 三、根因：同一条协议两处实现漂移 + 两个出口都没有保证

| 位置 | 改前行为 |
|---|---|
| 应用侧 `context_runtime.RepairInterruptedToolChains`（2026-09-17 按实测 400 修过） | 剔孤儿、剔重复、把乱序结果搬回声明之后、补中断占位——协议完整 |
| 框架侧 `seelexctx.repairInterruptedToolChains`（**同名孪生**） | 只补"缺失结果的占位"，**不剔孤儿、不搬乱序**（`toolResultExistsLater` 让乱序行原样留在后面） |
| 请求出口 A（主会话）：`seelexctx.NewAssembler` | 只做块序与占位符解析，对 `WorkingHistory` 不做任何协议规整 |
| 请求出口 B（节点子代理）：`seelebridge/node.ScopeAssembler` | **绕过** seelexctx 装配器，合并块后直接委托 `DefaultRequestAssembler`——出口 A 的规整（即便有）也管不到它 |

包依赖方向是 `application → seelexctx`，两处修复实现无法共用一份，于是"孪生必须同步"
这条纪律一破，框架侧那半就一路把非法历史发到 provider；而两个装配出口都是"上游写什么
就发什么"，没有人对**发出的请求合法**这件事负责。两条现场分别落在两个出口上，正好说明
问题不在某个生产点，而在**出口缺不变量**。

## 四、修法

| # | 位置 | 改动 |
|---|---|---|
| 1 | `seelexctx/history_safety.go` | `repairInterruptedToolChains`（只补缺）→ `repairToolPairing(history, fabricate)`：照应用侧算法补齐协议三件事——孤儿/重复结果剔除、乱序结果搬回声明之后、`fabricate=true` 时补中断占位（占位仍遵守"等原地结果输出后再补"的 `delayedFlush` 规则，避免把合法请求改成 400）。`PrepareReplaceHistory` 因此同时拿到标记清理 + 协议修复 + 空正文修复 |
| 2 | `seelexctx/history_safety.go` | 新增导出出口 `SanitizeProviderToolProtocol(history)`（= `repairToolPairing(history, false)`）：**只剔除与搬运、绝不合成占位**——请求可能发生在工具尚未执行完的活跃 ReAct 中间态，给"即将执行"的调用补占位等于污染历史（与应用侧 `PrepareNewHistoryContentFor` 同一条保守边界） |
| 3 | `seelexctx/assembler.go` | 出口 A：解析窗口后、拼尾部栈块前，`history = SanitizeProviderToolProtocol(history)` |
| 4 | `seelebridge/node/coordinator.go` | 出口 B：`ScopeAssembler.Assemble` 委托默认装配器前，`request.WorkingHistory = seelexctx.SanitizeProviderToolProtocol(request.WorkingHistory)` |
| 5 | `application/core/history_safety.go` | `classifyProviderFailure` 把 provider 的这条措辞归入 `invalid_history`（与"chat content is empty"同类）：请求记录不合法 → 走有界检查点的历史恢复，而不是把会话循环判死。修 3/4 之后这条 400 不该再出现，但这层兜底不该依赖 provider 的措辞与严格度 |

刻意**不动**的两处：

- `projectHistory` 仍保留窗口内的非单元消息（审计 R3：UI 可见的轮次不因窗口/溢出统计
  消失）；协议规整放在"写回历史"（`PrepareReplaceHistory`）与"发往 provider"（两个
  装配出口）上，而不是改投影口径本身。
- 应用侧的 `RepairEmptyHistoryContent` 把工具轮 assistant 正文**归零**（wire 上框架本就
  置 nil，见其注释里的前缀缓存实测），框架侧 `repairEmptyHistoryContent` 仍补
  `toolCallHistoryContent` 占位。这处口径差异涉及前缀字节一致性，本轮不碰（登记在
  §六）。

## 五、验证

```text
# 协议用例（RED → GREEN）
$ go test ./seelexctx -run 'TestPrepareReplaceHistory|TestAssemblerSanitizesWorkingHistoryToolProtocol|TestControllerWindowProjectionKeepsProviderToolProtocol' -count=1
    --- PASS: …DropsUnownedOrphanToolResult / …RealignsMisorderedToolResult
    --- PASS: …LeavesLegalHistoryUntouched / TestAssemblerSanitizesWorkingHistoryToolProtocol
    --- PASS: TestControllerWindowProjectionKeepsProviderToolProtocol
    ok  github.com/RedHuang-0622/seelex/seelexctx

# 节点出口（出口 B）
$ go test ./seelebridge/node/ -run TestScopeAssembler -count=1 -v
    --- PASS: TestScopeAssemblerSanitizesWorkingHistoryToolProtocol
    --- PASS: TestScopeAssemblerLeavesLegalHistoryUntouched

# 恢复分类
$ go test ./application/core/ -run 'TestToolProtocolRejectionsAreHistoryFailures|TestServerFailuresAreRecoverableWithoutAutomaticReplay' -count=1 -v
    --- PASS: TestToolProtocolRejectionsAreHistoryFailures

# 全量
$ gofmt -l application seelexctx seelebridge      （空）
$ go build ./...                                  exit 0
$ go test ./seelexctx/... ./application/... ./sessionstore/... -count=1
    全绿
$ go test ./seelebridge/ -run 'WorkTable|SessionAxis|WindowTail|Context' -count=1
    ok
```

## 六、残留（未在本次取证/修改）

1. **孤儿行从哪来的，没有 runtime 取证**：控制器投影（本文 §二复现的形状）、框架
   `loop` 的中间态、应用侧替换历史都可能是生产点。因此修在出口（不变量：无论谁写坏，
   发出的请求都合法），而不是赌某一个写入点。若要收口生产点，需要带 `-tags` 的实况
   探针在窗口投影那一步 dump `decision.History` 前若干条。
2. **两侧空正文口径不同**（§四末条）：应用侧归零工具轮 assistant 正文、框架侧补占位，
   两者的 wire 字节在"框架历史里那条 assistant 正文非空"时会分叉（前缀缓存代价，非
   400 风险）。统一它需要先确证"框架是否恒置 nil"（现依据是 Seele `session/loop.go:564`
   与其注释引用的实测），不在本轮范围。
3. **出口只有这两处**（已 grep `RequestAssembler` 实现：`seelexctx.NewAssembler` 与
   `node.ScopeAssembler`）；若将来新增装配器，必须同样调用
   `seelexctx.SanitizeProviderToolProtocol`——这是一条**契约**，不是可选项。role-turn
   会话（`RunRoleTurn`）另走角色引擎，不经这两个装配器，本轮未取证其历史来源是否也会
   携带孤儿行。
