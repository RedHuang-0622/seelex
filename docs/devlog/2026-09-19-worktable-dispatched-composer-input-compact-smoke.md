# 工作表格「实发」轴 + composer 输入不再被吞 + compact 真实 API 冒烟（2026-09-19）

> 日期: 2026-09-19 | 范围: `gui/frontend/dist/work-table.js`、
> `gui/frontend/dist/composer-input.js`（新）、`gui/frontend/dist/app.js`、
> `compact_live_smoke_test.go`（新）、`docs/gui/modules/*`

本轮三件事：① 把「台账」与「实发块」在表格上做出区分；② 修掉前端偶发吞输入；
③ 压缩（compact）链路做一次真实 API 冒烟并看结果。三者共同的前置是上一轮把
工作表格做成**跨会话台账**（全局读面）+ 会话轴（`session_id` 列与「仅本会话」
筛选），那份改动与本轮同批落地。

## 一、工作表格「实发」轴：台账 ≠ 实发块

### 1.1 两个概念的边界（后端本来就是两条数据面）

| | 台账（工作表格） | 实发（请求尾部打点块） |
|---|---|---|
| 数据面 | `Runtime.TaskSnapshot()`：实时注册表 + 各会话分区合并，**跨会话全量**（含终态历史） | `serviceState.workTableTraceBlockFor(sessionID)`：**只含该会话 scope 未终态任务** |
| 去向 | GUI 表格 / `worktable.changed` / `runtime.work_table` | `context_runtime.prepareExecutionContextFor` 前置进 `currentInput`（`coordinator.go:230`）——真正进请求、进模型上下文 |
| 粒度 | 项目/全局 | 会话级、仅 active |

后端一直是分开的（上一轮"全局化"刻意保留这个约束）；没分开的是**呈现**：表格
只有「会话」列 + 「仅本会话」筛选（终态历史行也照列），用户看不出"哪些行此刻真的
被送进了模型"。

### 1.2 改法（纯前端，不动数据契约）

- `work-table.js`：`TERMINAL_STATUSES`（completed/failed/done）+ `isDispatchedRow`
  = `row.session_id == 当前视图会话 && 状态非终态`，与后端打点块规则同口径
  （`workTableTraceBlockFor`：会话作用域 + 仅未终态）。
- 行级标记：`data-work-dispatched="1|0"` + 「实发」徽标（`work-sent-chip`），只挂在
  实发行上。
- 筛选：工具栏新增「实发」**开关**（`state.sentOnly`，`data-work-sent-filter`，
  `aria-pressed`）；开启后只列实发行，且计数、类型计数、批次页签、分页按同一
  scope 重算（`scopeRows`/`scopedRows`/`batchesInScope`/`dispatchCount` 共用口径，
  避免"计数说 5 条、列表只显示 3 条"）。
- 有意**不**加后端字段：判定所需的 `session_id` 与 `status` 行上已有；实发是
  视图语义，加字段会让契约承担展示口径。

回归：`gui/frontend/dist/work-table.test.mjs` 新增 4 个用例（计数只算本会话未终态、
行徽标/属性只标实发行、开关 scope 收窄计数与批次页签、开关是纯 UI 态）。

## 二、composer 偶发吞输入：三个坑与规则集中

用户报告"前端会偶发吞掉一些输入的内容"。逐个排出可造成**输入丢失**的机制，
把规则抽成纯函数 `gui/frontend/dist/composer-input.js`（`app.js` 只接 DOM 事件）：

1. **提交清空整框**（`app.js` 提交处理器）：`await invoke("Submit", text)` 是异步
   RPC，成功后 `elements.prompt.value = ""` 会把往返期间用户新敲的字一起抹掉。
   → `clearSubmittedText(current, sent)`：值等于原文才清空；以原文开头则只切掉
   前缀（保留追加的新输入）；已被改写成别的则**不动**（宁留原文也不吞新输入）。
2. **草稿回填覆盖**（`restoreComposerDraft`）：整份快照渲染时把后端草稿正文灌回
   输入框，原来只挡了"聚焦中"。若输入框失焦且本地输入还没落盘（防抖 300ms 内），
   后端副本更旧 → 覆盖即吞字。→ 新增 `composerDirty`（本地未落盘输入）并由
   `shouldRestoreDraft` 统一判定：非草稿/无正文/聚焦/脏 任一成立都不回填；落盘
   成功且期间没再变才清脏。
3. **输入法合成的 Enter**：原守卫只有 `event.isComposing`；WebView2 偶发不在该次
   Enter 上带它。→ `isComposingEnter(event, composing)` 三重判据：本地
   `compositionstart/end` 跟踪 + `event.isComposing` + `keyCode === 229`。

附带把"程序化写入输入框"的路径也标脏（`acceptSuggestion` 接受内联建议、
`recallQueuedInput` 召回排队消息），否则这些内容会被当成"没编辑过"而被快照回填
覆盖。

回归：`gui/frontend/dist/composer-input.test.mjs` 9 个用例（提交只切前缀 / 不覆盖
改写内容 / 空提交 no-op / 回填只在干净未聚焦且不同时才做 / IME 三重判据）。

**残留不确定性**：三条都是代码级可复现机制，但本机没有 Wails webview 的自动化
复现环境——哪一条是用户实际遇到的那一次，无法用浏览器取证；三条都堵住是当前
边界内的最强修复。

## 三、compact 链路真实 API 冒烟

新增 `compact_live_smoke_test.go`（`-tags compactlive`，opt-in）：

```text
$env:SEELEX_SMOKE_ACCOUNTS='config/accounts.yaml'
go test -tags compactlive . -run TestCompactLiveSmoke -count=1 -v -timeout=15m
```

- 先把账号副本的上下文预算调小（`defaults.context_window: 40000` /
  `max_tokens: 2048`，删掉各账号覆盖）——默认 200k 窗口要攒 12.5 万 token 才触发，
  冒烟又慢又贵。改写只发生在 `t.TempDir()` 的副本上，源文件只读、不解析、不打印。
- 有界循环"攒到压缩发生"（≤4 轮，每轮投一段固定材料）：单轮是否越过阈值受模型
  自己是否多打工具影响（材料固定、工具噪声不定），不能赌单轮尺寸。
- 断言：出现 `context_budget*` 压缩记录；压缩后的回合**有模型回答**（证明折叠形态
  被 provider 接受）；`/compact` 给出压缩相关结论；手动入口之后普通回合仍可用。

### 3.1 冒烟结果（真实 provider，PASS 238s）

```text
第 1 轮 结束：压缩记录=[]                                             ← 还没到阈值
第 2 轮 结束：压缩记录=[{Version:2 Reason:context_budget_autonomous
               EstimatedTokens:58109 ...}]                          ← 触发折叠
压缩后回答：继续                                                       ← 折叠形态被 provider 接受
/compact 提示：当前上下文估算 12642 tokens，未达压缩阈值 24714，无需压缩（接近上限时框架会自动压缩）。
压缩后回答：链路正常                                                   ← 手动入口之后链路照常
session=sess_1789829034211517400
--- PASS: TestCompactLiveSmoke (238.28s)
```

读出来的结论：
- **自动压缩正常**：装配层在逼近上限时折叠为有界 checkpoint（reason
  `context_budget_autonomous`），压缩后的上下文被真实 provider 接受，模型照常作答；
- **压缩确实"变小了"**：自动压缩后手动 `/compact` 报的估算是 **12642 tokens**
  （阈值 24714）——从触发时的 ~58k 降到 12.6k，且如实判定"无需再压"（不伪造压缩）；
- **手动入口与工具同一落点**：`/compact` 与 `compact_context` 都走
  `CompactContextNow`，两种结论（已压 / 未达阈值）都如实回报，不报故障；
- **未知压缩原因（观测）**：`model.TaskState.ContextCompactions` 按 `RequestID`
  作用域，下一轮的快照里看不到上一轮的压缩记录（冒烟末轮 `压缩记录数=0`）。即
  GUI 的"Context compression"面板只在该轮可见，历史轮次不留档；是否要跨轮保留
  属产品口径，本轮不改。

另附前置探针 `TestCompactLivePreflight`（一次极小真实回合），用来把"provider 通不
通/快不快"与"压缩链路对不对"分开——它在 1.1s 内通过且**无**压缩记录（证明默认
小回合不会误压，阈值判定不是恒真）。

## 四、验证

```text
go vet -tags compactlive .                                          # 通过
go test ./application/core -run "Compact|ContextBudget" -count=1     # ok
go test -tags compactlive . -run TestCompactLivePreflight -count=1 -v -timeout=6m   # PASS 1.11s
go test -tags compactlive . -run TestCompactLiveSmoke -count=1 -v -timeout=15m      # PASS 238.28s
node --test gui/frontend/dist/*.test.mjs                             # 366+9 pass
go test ./...                                                        # 见本轮验证记录
```

## 五、未做 / 风险

- composer 三条机制无法用 webview 自动化取证，只有代码级证据（见 §二）。
- `todo:<n>` 的 ID↔清单索引契约仍不成立（上一轮 §7.5 的已知问题），本轮未动。
- 压缩记录的跨轮可见性（§3.1 第四条）是观测，不是结论；要不要改属产品决策。
- 冒烟的两个真实回合都会让模型尝试调用工具（尽管提示里要求不要调），因此单轮
  token 量有抖动；这正是"有界循环攒到压缩"而不是"赌单轮尺寸"的原因。
