# 状态机枚举统一 · 第三波：工具调用视图词（`model.ToolCall.Status`）

- 口径来源：用户指令 **"状态用枚举和 iota 来规范，不要用零散的字符串做硬编码比较"**。
- 上一波（第二波）把总表 §3 的剩余清单逐条判完，只留下两处"刻意不动"，其中一处是
  **工具调用视图词**——当时写的理由是"要连读方清单一起收，登记为下一批"。本波就是那一批。
- 总表（唯一地图）：`docs/arch/state-machine-inventory.md`（§1 已统一 / §2 刻意不枚举 / §3 下一波候选 / §4 怎么继续）。

---

## 1. 先清点：这一格的真实取值面（勘定结论）

**读方/写方逐点清点**（`git grep -n "ToolCall\|Tool\.Status" -- "*.go"` 后逐点判类型）：

| 角色 | 点 | 写/读的是什么 |
|---|---|---|
| 写 | `application/core/tool_hooks.go:49/:53` | 调起时的可见工具行（`dto.ToolEventRunning`） |
| 写 | `application/core/tool_hooks.go:188` | 结果回来后把同一行改写成 `dto.ToolEventSuccess` / `dto.ToolEventError` |
| 写 | `application/core/tool_hooks.go:225` | `tool_result` 行的终态 |
| 写 | `application/core/chat.go`（历史回灌两处） | 冷加载回灌的可见工具行（成功） |
| 写 | `application/core/session_history.go`（`adaptEngineMessage`） | 引擎消息折成可见消息时的工具行（成功） |
| 写 | `application/core/session_runtime/archive.go:344/:354` | 存档投影里的工具行（成功） |
| 写 | `application/core/subagent_view/coordinator.go:409` | 子代理**详情会话**的工具行 —— **写的是 `completed`** |
| 读 | `tui/state.go:34/:47` | 工具行图标（精确判 running/success/error）与耗时是否显示 |
| 读（边界） | `internal/adapters/session_workspace_ports.go:1123` | 存储面 `ConversationToolCall.Status`（词）→ 可见面 |
| 读（边界） | `application/console/backend_console.go:278` | 同一字段的 JSON 从事件 payload 读回（`model.Message`） |

**结论**：这一格的取值面是 **`running | success | error`**——**与"工具事件状态"本来就是同一格**
（`dto.ToolEventStatus`），不是"同词不同格"。总表 §2 与词表门禁头部注释里写的
"工具调用状态（running|completed|failed）"**与代码事实不符**：
写方从没写过 `failed`；唯一写 `completed` 的地方就是上表那一处**漂移写点**。本波按事实收口，
并把两处错误记载改掉（不静默改口径）。

---

## 2. 一处真漂移（不是"风格问题"）

`subagent_view/coordinator.go` 的详情投影给历史工具行写 `Status: "completed"`，这个词**不在本格取值面里**：

- 严格的读方会静默降级 —— `tui/state.go` 的 `switch` 只认三词，`default` 是把图标留成 `"→"`（未知），
  也就是说"跑完并出了结果的工具"会显示成"没有状态"；
- GUI 侧同样靠词兜底（`trajectory.js`：非 error/running 一律当 success；`components.js`：`is-<status>` 类名），
  同一个字段两种词 = 同一个事实在三处各折一次。

修法：写**本格的词**（`dto.ToolEventSuccess`，语义就是"这条历史调用出过结果"），并加用例钉住。

---

## 3. 红 → 绿（三条原文）

### 3.1 真漂移（行为级红）

```
=== RUN   TestSubagentConversationSpeaksTheToolCallCellWords
    tool_call_status_word_test.go:27: 详情页工具行说的是 "completed"，本格只认 "running"/"success"/"error"（契约 dto.ToolEvent*）
--- FAIL: TestSubagentConversationSpeaksTheToolCallCellWords (0.00s)
FAIL	github.com/RedHuang-0622/seelex/application/core/subagent_view	1.279s
```

改完写点后同一条用例绿（`--- PASS: TestSubagentConversationSpeaksTheToolCallCellWords`）。

### 3.2 落盘：**先按最朴素做法跑出红**（反证 + 修法）

这一格是**落盘格**：`model.ToolCall` 既是存档（`model.SessionArchive`）的形状，又是事件 payload 的
JSON 形状，磁盘上的老文件里放的是**词**。把字段直接换成枚举（契约枚举的 `UnmarshalJSON`
"认不得就报错"）——新增的红灯用例立刻给出反证：

```
=== RUN   TestToolCallStatusRecordFold
    toolcall_status_record_test.go:42: 落盘读回不许炸（{"id":"c1","name":"bash","status":""} / 空词同上）：
        dto: "" 不是工具事件状态的词（认得：[unknown running success error]）
--- FAIL: TestToolCallStatusRecordFold (0.00s)
```

（这次的复现手法是把刚写的 `ToolCall.UnmarshalJSON` 临时改名成 `tmpDisabledUnmarshalJSON`，
跑完立刻改回；改名那一笔没有进提交。）

修法 = 本格落盘的**具名转换点** `model.ToolCallStatusOfRecord(word)`：认不得的词与空词都落
`dto.ToolEventUnknown`——**不炸、也不折成"成功"**；`ToolCall.UnmarshalJSON` 只做"按词读进来 + 折一次"，
其余字段与默认解码逐字一致（别名类型避递归）。修完 `--- PASS: TestToolCallStatusRecordFold`。

### 3.3 类型化（编译级红，编译器把点列出来）

```
vet.exe: application\model\toolcall_status_record_test.go:14:51: cannot use dto.ToolEventSuccess
        (constant 2 of uint8 type dto.ToolEventStatus) as string value in struct literal
```

换类型之后 `go build ./...` 逐条把写方/读方点出来（本次依次暴露：`subagent_view/coordinator.go`、
`session_runtime/archive.go`、`e2e/scenario/runner.go`，再由 `go vet ./...` 暴露 6 个包的测试文件）。
"写错词 = 编译不过"这一条由此成立。

---

## 4. 改动清单

**契约**：不新开格 —— 复用 `dto.ToolEventStatus`（`application/contract/dto/subagent_live.go`）。

**`application/model`**（新增 `toolcall_status_record.go`）：
- `ToolCallStatusOfRecord(word string) dto.ToolEventStatus`：本格唯一转换点；
- `(*ToolCall).UnmarshalJSON`：落盘/事件 wire 的宽松读法（上面 3.2）；
- `state.go`：`ToolCall.Status` 由 `string` → `dto.ToolEventStatus`。

**写方（6 处）**：`chat.go`×2、`session_history.go`×1、`tool_hooks.go`×3（含视图回写）、
`session_runtime/archive.go`×2、`subagent_view/coordinator.go`×1（漂移点）。
**读方（1 处）**：`tui/state.go` 的 `switch` 与"是否显示耗时"改成类型比较。
**边界（1 处）**：`internal/adapters/session_workspace_ports.go` 折一次（`model.ToolCallStatusOfRecord`）。
**存储面**：`sessionstore.ConversationToolCallStatusSuccess`（本层不 import 契约包，存词）
+ `json_layout.go` 两处写点改引它。
**门禁**：`statusVocabularyScopes` 加"工具调用视图词"一格（写方 6 文件 + 读方/边界 + 定义面，
取值面 `running|success|error`）；**删掉过期白名单条目**（`节点状态 / subagent_view/coordinator.go / completed`
——那条的理由写的取值面本身就是错的）。
**文档**：总表 §1 加两行（工具事件状态 + 工具调用视图词）、§2 删掉"刻意不枚举"那一行、
§3 改判为已收口、§4 加一条"落盘格同时是 wire 形状时该怎么换类型"。

---

## 5. 删除清单（旧 → 新）

| 旧位置 | 旧形态 | 去向 |
|---|---|---|
| `subagent_view/coordinator.go:409` | `Status: "completed"`（漂移词） | 删除，改写 `dto.ToolEventSuccess`（本格的词） |
| `chat.go`×2 / `session_history.go` / `archive.go`×2 | `Status: "success"` 字面量 | 删除，引 `dto.ToolEventSuccess`（第二份词表） |
| `tool_hooks.go:49/:53` / `:225` / `chat.go` | `dto.ToolEventRunning.String()` / `status.String()` 折算 | 删除折算，字段直接写枚举 |
| `tui/state.go:34/:47` | `dto.ToolEventRunning.String()` 参与比较 | 删除折算，类型比较 |
| `sessionstore/json_layout.go:321/:329` | `Status: "success"` 匿名字面量 | 换成 `ConversationToolCallStatusSuccess`（store 侧唯一名词） |
| 门禁 `allowedStatusLiterals` | `节点状态 / subagent_view / completed` 条目 | 删除（命中消失后它会被门禁自己判"过期"） |
| 门禁头部注释 / 总表 §2 | "running\|completed\|failed" 与"同词不同格" | 改写为事实（本格 = 工具事件状态同格） |

---

## 6. 既有用例的机械适配（只改写法，**不动断言期望值**；逐条列明）

| 文件 | 改法 | 处数 |
|---|---|---|
| `application/core/subagent_view/tool_call_status_word_test.go` | 本波新增用例本身：字面量 → 类型比较 | 1 |
| `application/console/backend_console_test.go` | 夹具 `Status: "running"` / `"success"` → 契约常量 | 2 |
| `application/core/chat_hot_attach_reasoning_repro_test.go` | 同上（"success" → 常量） | 2 |
| `application/core/session_archive_test.go` | 同上 | 2 |
| `application/core/session_runtime/archive_test.go` | 同上 | 2 |
| `application/core/context_cache_divergence_probe_test.go` | "running" → 常量 | 1 |
| `application/core/service_snapshot_test.go` | `== "success"` → `== dto.ToolEventSuccess` | 1 |
| `gui/bridge_test.go` | 夹具里的 `.String()` 折算去掉 + 比较改类型 | 2 |
| `gui/tool_full_chain_test.go` | 比较改类型 | 2 |
| `manual_smoke_test.go` | 比较改类型（`%q`/`%s` 的格式化不用改） | 4 |
| `job_live_smoke_test.go` / `job_backfill_live_smoke_test.go` | 收进 `[]string` 的位置改成 `.String()`（读数是"词"，断言不变） | 2 |
| `teamwork_headless_smoke_test.go` | 同上（append 到 `[]string`） | 1 |
| `tool_full_chain_live_test.go` | 比较改类型 | 3 |
| `tool_full_chain_test.go` | 比较改类型 + 表格用例改成 `.String() == status`（表里存的仍是词） | 3 |
| `e2e/scenario/runner.go` | 场景脚本给的是词，比较改成 `Status.String() == step.Status` | 1 |

**没有一条期望值被改动**（唯一改语义的是 3.1 那个漂移点，它有独立红灯用例）。

---

## 7. 命令原始读数

```
gofmt -l <本波改动文件>            → 空（既有未格式化文件 sessionstore/board_test.go、
                                     sessionstore/teamwork_items.go 本波未触碰，按纪律不顺手格式化）
go build ./...                     → 空
go vet ./...                       → 空
go test ./... -count=1             → exit 0 / 85 行 / 73 ok / 12 no test files / 0 FAIL
  ├ 根包（真实装配 headless 冒烟） ok github.com/RedHuang-0622/seelex 41.137s
  ├ application/contract/dto       ok  1.684s（含 13 格 wire 用例 + 老形状读回）
  ├ e2e（两张词表/类型门禁）        ok  3.130s
  ├ seelebridge（全族）             ok 59.396s；tools 21.587s；worktree 46.606s
  └ sessionstore                   ok 51.283s
go test ./e2e/ -run TestStateEnumNeverCastToString|TestStateEnumFieldCastInDeclaredFiles → PASS
go test ./e2e/ -run TestSubagentStatusVocabularyGate -v
                                   → PASS（阴性对照 5 形态全绿）
读数：词表门禁 scope **12 → 13 格**；白名单条目 **4 → 3 条**（删的是那条过期且写错的）
```

**性能量级**（新增，只报量级、不主张快慢；`_logs` 内可复跑）：

```
BenchmarkToolCallRecordDecode/with_fold-8     120274   10246 ns/op   752 B/op   15 allocs/op
BenchmarkToolCallRecordDecode/plain_alias-8   160401    6677 ns/op   592 B/op   13 allocs/op
```

判读：落盘读回多折一次的代价是 **+2 次分配 / 约 +1.5×**（绝对量 10 µs 级，每次解码一条工具行）。
它只发生在"存档装载 / 事件 payload 解码"这两条读路上，不在热循环里；换来的是
**写错状态词 = 编译不过**（字段类型）与**老文件不炸**（宽松读法）。若日后会话装载成为热点，
可把宽松折法下沉到只有一个 `Status` 字段的探针结构上再省一次整结构解码。

---

## 8. 对外形状的一处明说（不静默）

- **读**：老文件/老事件里的 `""`、`completed`、任何没见过的词 → `ToolEventUnknown`（以前是原样带在字符串里）。
- **写**：从来没填过 `Status` 的 `ToolCall` 现在写出去是 `"status":"unknown"`，**以前写的是 `""`**。
  本格三个已知词的 wire 逐字不变（`"status":"success"` 等），前端与老工具链不需要改；
  变的只有"没有值"这一种情况的字面：`""` → `"unknown"`。
  这与本波的口径一致（**没有值不等于成功**）——前端 `tool.status || "success"` 那类兜底会因此
  从"绿"变成"中性"，是**有意的语义修正**，而不是形状漂移。

---

## 9. 未决项（登记，不顺手做）

1. **todo 三态**（`dto.TodoItemStatus`）——总表 §2 里仅剩的一处"刻意不枚举"，等移除窗口；
2. **前端 JS 侧仍是字符串映射**：`trajectory.js` / `components.js` 把"非 error/running 一律当 success"，
   本波没有动前端（本波只保证 Go 侧一格一词、wire 词不变）；要不要把这一层也收成一张映射表是独立一批；
3. **store 的会话投影把"记录存在"折成"成功"**（`sessionstore/json_layout.go`）：本波只给它起了名字
   （`ConversationToolCallStatusSuccess`）并互锁了词，**没有改它的语义**——它表达的是"这条工具记录
   已经在库里"，重新判定成功与否不在这一层的能力范围内；
4. 真 API 冒烟（`SEELEX_LIVE_SMOKE=1` + `config/accounts.yaml`）与 GUI 手工点按冒烟仍是既有缺口
   （桌面纪律：只读检查、不合成输入）。
