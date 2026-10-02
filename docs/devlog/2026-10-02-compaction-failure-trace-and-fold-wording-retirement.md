# 压缩失败只留痕：口径收敛到「压缩」+ 失败不再折上下文、不再中断会话 + 状态页出 seq 条目

- 日期：2026-10-02
- 范围：`application/model`、`application/core/{context_compact*,context_runtime/*,task_context}`、
  `seelexctx/*`、`seelebridge/runtime_*`、`internal/adapters/*`、`gui/frontend/dist/*`、
  `config/seelex.yaml` 与**现行**文档（README/arch）；历史留痕（CHANGELOG、docs/devlog、
  docs/2026-*、docs/research）不动。
- 现场（用户原话收敛）：
  1. 「只有压缩的口径没有折叠的口径……把折叠的口径都改成压缩失败」——「折叠」这个词
     同时被用来指"压了"和"没压成"，读者无从分辨；
  2. 「压缩失败之后留痕但是上下文照旧，不做上下文的折叠」；
  3. 「压缩成功之后在状态中出一个压缩的 seq 条目」（组件能找到就复用）；
  4. 现场报错：`ERROR … provider context exceeds the safe token budget: estimated=281424
     budget=163616` ——聊天被这句内部错误**中断**，而用户要的旅程是
     「压缩失败 → 留下失败记录 → 原始上下文继续存在 → 模型仍然直接看到原来的上下文，
     **不中断继续工作**」。

## 1. 口径：一个词，「折叠」退场

「折叠」在这套系统里本来就不是一个独立概念，它是压缩流程里的一步（把窗口外轮次收成
帧）。问题出在**失败路径也借用了这个词**：一次没压成的压缩被写成"折叠了但没落记录"，
于是"折叠"既像成功又像失败。本轮把它统一成两句话：

| 这件事 | 现在怎么写 |
|---|---|
| 流程 / 记录 / 帧 / 进度 | **压缩**（compaction）——压出窗口、压缩帧、压缩记录、压缩栈 |
| 判据命中但这次压不下去 | **压缩失败**（`compact_failed`）——只留痕，什么都不动 |

落地面：Go 标识符（`foldHistory`→`sessionHistory`、`replaceFoldHistory`→
`replaceSessionHistory`、`ineffectiveFold`→`ineffectiveCompact`、`commitFold`→
`commitCompaction`、`LocalFold*`→`LocalCompact*`、`ZoneFolded`→`ZoneCompacted`、
`CompactFoldedUnrecorded`→`CompactUnrecorded`… 共 60 条规则 / 40 个文件）、协议字面量
（`folded_without_record`→`compacted_without_record`、`ineffective_fold`→
`ineffective_compact`、证据 ref 前缀 `fold-local:`→`compact-local:`、四区 kind
`"folded"`→`"compacted"`、帧正文 JSON 键 `folded`→`compacted`）、现行文档与前端文案
（「以上 … 已被折叠」→「以上 … 已被压缩」、表头「栈/折叠/帧」→「seq/压缩/帧」…）。

**边界（有意为之）**：`折叠` 是共用词，界面收起/展开那一层继续叫折叠（折叠区、折叠头、
默认折叠、展开/折叠、`is-collapsed`、粘贴折叠、TOML folded scalar、`strings.EqualFold`）。
替换用受控工具（`_scratch/repl`，按字节读写 + 保护短语哨兵）做，跑完按"剩下的折叠是不是
全是界面语义"逐条核对，并回修了 9 处误伤（`是否折叠`→曾变`是否压缩` 等）。
**历史留痕不改**：CHANGELOG、docs/devlog、docs/2026-*、docs/research 里的「折叠」是当时的
记录，追改等于篡改历史；改名这件事本身记在本文。

## 2. 压缩失败：只留痕，不动上下文，且不再中断会话

### 2.1 留痕（此前只活在 6 秒的瞬态进度条里）

失败痕进与成功记录**同一个列表**（`ContextCompactions`），只是形状受四个恒等式约束：
`Failed=true` 时区间（MessageFrom/To、EventFrom/To）与 FrameRef **恒空**。因此它
不参与保留窗口起点推导（`RetainedFromForCompactions` 只认 EventTo）、不画对话区分界
（`compactionFrontier` 只认 message_to）、也不占压缩栈的一格——它不是一个压缩，是
"这次没压成"的证据。原因写进 `Note`（字面量 + 数字事实），两步幂等：**同一上下文版本 +
同一原因只留一条**（失败会一直持续到某次压缩真的成功，不然一会长会话每轮都在追加）。

### 2.2 上下文照旧：装配上限换成 provider 真实窗口

失败路径原先仍按**内部安全线**（budget = 窗口 − 输出预留 − 安全预留）装配，于是
一条**本来装得进 provider 窗口**的旧轮次被静默截在窗口外——模型看不见上文、界面不留痕，
这正是"偷偷把旧消息折叠掉"。现在失败轮的装配上限取 `budget.Window`（模型物理上能收下的
最大量）：安全线以下那点余量本来就是"留给下一轮压缩"的，这次压不成，余量也就没有意义。

### 2.3 不中断：安全线不再锁死会话

装配后仍越安全线时，旧代码直接返回 `ErrProviderContextBudgetExceeded`——用户现场那句
`provider context exceeds the safe token budget: estimated=281424 budget=163616` 就是它。
现在分两种情形：

- **压不下去（compactionFailure 非空）且不可压缩的那部分自己还装得下**（system 指令 +
  工具 schema + 当轮输入 + plan ≤ 预算）→ best-effort 发出，并把真实数字写进失败痕
  （`estimated=… budget=… window=… overhead=…`）。这是唯一还能让会话继续的选择：安全线是
  我们自设的余量，估算器又是保守的（校准因子向上取整），"超安全线"不等于"provider 收不下"；
- **其余情形**（不可压缩部分自己就超预算，或这次压缩没有失败）→ 照旧拒绝。那时任何压缩
  都救不了（例如 system 指令自身就超出窗口），发出去必然被 provider 拒，
  `TestPrepareExecutionContextCountsActiveSystemPrompt` 继续钉住这条。

### 2.4 出口口径

`CompactOutcome` 新增 `compact_failed`（替代 `skipped_no_summary`：旧的取值只覆盖"没有
读后感"一种成因，另一种"换不来余量"以前被误报成"判据未命中"）。回执、进度终局、门禁
Detail、失败痕四处写**同一个**原因字面量（`no_model_summary` / `ineffective_compact`），
前端 `compactionOutcomeLabel` / `compactionFailureText` 只翻字面量那一段，数字原样带出。

## 3. 状态页：每次成功压缩出一条带 seq 的条目

右栏「状态 / 概要」的压缩条目每行加一枚 **seq 徽标**（大号数字 + 小字单位），组件不在
压缩面板里另写一份——它就是 goal 看板 active seq 那一枚（`components.js renderSeqBadge`
的同一个纯件）。`goal-board-view.js` 改为调它，两处共用一份 DOM 与一份配色；压缩那一档
只换色调与字号（右栏表第一列只有 34px，26px 的数字会把行撑成两行）。

失败痕渲染在同一张表里、排在栈下方：失败色 + 「失败」标记 + 一句原因（跨整行，因为原因里
有一串不能断行的数字事实）+ 动作列写「上下文原样」。表头从「栈 / 折叠 / 帧」改成
「seq / 压缩 / 帧」（列里的东西本来就没变，是表头一直没跟上）。

## 4. 先红后绿（两条复现）

新增 `application/core/context_compact_failure_trace_repro_test.go`：

| 用例 | 红灯形态（改前实测） | 改后 |
|---|---|---|
| `TestCompactionFailureKeepsWholeContextAndLeavesTrace` | `压缩失败后最旧的轮次被折出了 provider 历史（模型失去原始上下文）：装配上限被收到安全线上，旧轮次被静默截掉。compared=169046 assembled=128030` | 最旧轮次仍在 provider 历史；留一条 `Failed` 记录（快照可见面也有）；幂等（再压一次仍是一条） |
| `TestCompactionFailureDoesNotInterruptOverBudgetSession` | `压缩失败时不得用内部安全线中断会话（用户现场那句 ERROR 就是它）：provider context exceeds the safe token budget: estimated=179998 budget=166808` | 不返回错误；失败痕带数字事实；下一次装配不再被安全线拒绝 |

两条各自把行为改回去跑过一遍复现红灯（`if false && budget.Window > target`、
`if true || compactionFailure == ""`），确认不是"恰好通过"。

## 5. 回归证据

```text
go build ./...            → exit 0
go vet ./...              → exit 0
go test ./... -count=1    → 全绿（无 FAIL；含 application/core、context_runtime、
                            seelebridge、seelexctx、sessionstore、gui、tui、mcpstack）
go test ./application/core/ -run 'TestCompactionFailure' -v     → 2/2 PASS
go test ./application/core/ -run 'TestCompactionFailure|TestCompactionFailureLeavesContextAndStackUntouched|TestCompactionFailureWithFailedReadbackLeavesContextUntouched|TestCompactionWithReadbackStillCompacts' -v
                          → PASS
node --test gui/frontend/dist/*.test.mjs                        → 622 pass / 0 fail
                          （基线 618 + 新增 4：失败痕两行、失败不参与分界、失败文案）
gofmt -l <改动文件>       → 干净
```

**性能热点（profile，不是"感觉更快"）**：压缩路径与预算判据一起跑 3 遍，
`go test ./application/core/ -run 'TestCompactionFailure|TestContextBudget|TestCompaction|TestCompact|TestFold|TestTranscriptPrefix' -count=3 -cpuprofile`：

```text
Duration: 9.90s, Total samples = 9.08s (91.68%)
flat%   cum%    函数
51.32%  67.62%  unicode.Is
16.19%  16.19%  unicode.is16
11.56%  95.26%  seelexctx/tokens.Count          ← 热点：逐 rune 的类型判定
10.68%  78.30%  seelexctx/tokens.isCJKRune
 5.18%   5.18%  unicode.IsLetter (inline)
```

两个结论：① 这条路线的成本几乎全在 **token 估算的字符扫描**（`tokens.Count` cum 95%），
与压缩本身的簿记无关；② 本轮新增的失败留痕在同一次采样里**一次都没出现**——它是
O(1) 的"看一眼上一条 + 追加一条小记录"，没有引入新的热点。（顺带记一条不在本轮范围内的
观察：`tokens.Count` 的 rune 扫描是整条装配链最贵的一段，值得单独优化。）

界面证据：

- **静态原型（真渲染件 + 真样式 + 真浏览器）**：`gui/frontend/dist/compaction-preview.html`
  （`go run ./_scratch/static` 起 http 源 → 浏览器打开）——一次成功压缩 / 多次（栈顶 +
  更早）/ 失败留痕 + 成功 / 换不来余量 / 门禁进度五格，截图见本轮交付。
- **暂存二进制**：构建成功（`-tags gui,desktop,production`），但见 §6 的失败记录。

## 6. 冒烟与未做

| 项 | 结果 |
|---|---|
| 压缩条目静态原型（右栏 360px 实测宽度，真渲染件 + 真样式） | ✅ 五格渲染正常：seq 徽标 / 栈顶标记 / 更早的降灰 / 失败行（失败色 + 原因跨整行 + 「上下文原样」）/ 门禁进度 + 逐关耗时（截图两张，其中一张为失败行排版回修前后） |
| 暂存二进制起一个独立数据根实例（`_tmp/seelex-gui-smoke.exe -store _tmp/smoke-data/.seelex/sessions`） | ❌ **窗口没出现**：日志走到 `[WebView2] Environment created successfully` 之后进程即以 0 退出（`lock.owner` 留在数据根里），window 列表里始终只有本会话所在实例那一个窗口。本机不能并行开第二个 GUI 窗口，因此"逐入口点检（打开/点按/输入/切换/滚动/关闭）"**未做**，不拿静态原型冒充它 |
| 真 API 的压缩（成功出条目 / 失败留痕） | ❌ 未做：需要一次真实长会话 + 付费模型调用（读数闸本身就是一次付费调用），本轮不烧；由 Go 用例 + 静态原型覆盖 |
| 失败路径的 live 复核（账号限额/网关 4xx 造成 replay 失败） | ❌ 未做：留痕形状已由用例钉住（`reason=no_model_summary source=… note=<真实报错>`） |

**判定**：本轮的三层证据里，单元/集成（红→绿 + 全量回归）与性能热点（profile 量级）
**齐**；全局 computer-use 冒烟**缺**（卡在"本机不能并行开第二个 GUI 窗口"，理由与观察
写在上表）。收口按"缺层要说明"处理，不写成"冒烟通过"。

## 7. 未做 / 边界（如实）

1. **重启后压缩记录仍不落盘**（既有问题，本轮未碰）：`record.Execution.Task.ContextCompactions`
   所在的 record 通道在现行存储布局（v8/S20）已退役，冷恢复只从它读——于是重启后右栏
   压缩条目（含本轮的失败痕）与保留窗口起点为空，而压缩栈帧与帧正文照常在。这是
   `docs/research/2026-10-02-compaction-record-and-failure-trace-survey.md` §4.2 的实测结论，
   修它要把压缩事实写进事实源（message/帧通道），属另一条改动线。
2. **提交**：按仓库规范**不自动提交**——改动留在工作区（含本文件、新增用例
   `application/core/context_compact_failure_trace_repro_test.go`、静态原型
   `gui/frontend/dist/compaction-preview.html`），提交信息已按仓库格式备好，等用户点头。
3. **全局 computer-use 冒烟缺层**（§6 表）：不把它写成"冒烟通过"。
