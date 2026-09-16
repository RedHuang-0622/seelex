# 2026-09-15 Agent Team 的 team work 接入 computer use（证据面）+ 真实 API 探针

> 日期: 2026-09-15 | 范围: `application/core/goal_work_summary.go`、
> `gui/team_work_computer_use_live_probe_test.go`（新）、
> `application/core/agentteam/README.md`、根 README opt-in 冒烟清单

## 一、问题：EXEC 截了屏，ADVISOR 只看到一个工具名

team work（goal-a2a：EXEC(a) + ADVISOR(b)）里，EXEC 的产出经
`goalTurnWorkSummary` 压成一句有界 Detail → `work.progress` 帧 → 进 ADVISOR 输入。
此前摘要只带「本轮终稿正文 + 工具名去重表」：

```text
runtime_computer.go - seelex - Visual Studio Code | tools: computer_screenshot
```

ADVISOR 是**看证据评审**的角色，这句里它只知道"EXEC 调过截图工具"，不知道
**看到了什么**——画面尺寸、前台窗口、截图句柄全都不在。视觉类工作的评审因此
只能靠 EXEC 的自述，退化成"听汇报"。

## 二、改法：把 computer use 证据抽进 ADVISOR 输入（有界）

`summarizeTurnWork` 增加 `computerUseEvidence`：从本轮工具结果里解析
`computer_screenshot` 的明文返回（`ref` / `width` / `height` / `foreground.title`），
压成一段有界证据，排在工具名之前（截断时先牺牲工具表，保住"看到了什么"）：

```text
runtime_computer.go - seelex - Visual Studio Code
  | screen: media:2483aa2a 1600x900 foreground="runtime_computer.go - seelex - Visual Studio Code"
  | tools: computer_screenshot
```

口径与边界：

- 只取公开字段（媒体句柄 + 尺寸 + 前台窗口标题），**不含像素内容**；
- 最多两份画面、标题截断 80 rune、句柄截断 14 rune——Detail 仍是"有界一句话"；
- 只有 `media:` 引用算屏幕证据（`blob:` 等引用不掺进来）；坏 JSON 直接跳过；
- 非 computer use 的轮次摘要形状不变（既有用例继续通过）。

## 三、真实 API + 真机探针

新增 `gui/team_work_computer_use_live_probe_test.go`（`SMOKE_TEAM_WORK_COMPUTER_LIVE=1`，
走 headless 控制面、与 GUI 同装配）：

1. Submit #1 物化主会话（EXEC = main）→ `goal.begin` 隐式装配 goal-a2a（tl = ADVISOR）；
2. Submit #2 要求 EXEC **真的调用 computer_screenshot** 并照抄前台窗口标题；
3. 三条硬断言（都不依赖模型自由发挥）：
   - 会话媒体分区出现 `screenshot-*.png`（工具真的跑了）；
   - `role.snapshot(tl)` 的 ADVISOR 输入里带 `screen: media:… foreground="…"`；
   - `goal.gov_snapshot` 有裁决原文（ADVISOR 回合确实发生）。

本机实测（2026-09-15）：

```text
[tl] 回合数=1；输入里 screen 证据=true media 引用=true 标题命中=true
[tl] 输入节选：screen: media:2483aa2a 1600x900 foreground="runtime_computer.go - seelex - Visual Studio Code" | tools: computer_screenshot
[gov] active=true seat="advisor-b" peer="advisory_pending"
      last_directive="[verdict_done] P2 结论：goal g-1 的两条 acceptance 均已被帧账本满足。
        (1) EXEC 调用 computer_screenshot：帧 4、5 的 tools 字段均为 computer_screenshot；
        (2) ADVISOR 输入带截图证据：帧 4、5 的 screen 字段携带 media:2483aa2a，分辨率 1600x900，
            foreground 为 \"runtime_computer.go - seelex - Visual Studio Code\"，说明截图证据已随
            work.progress 帧进入 ADVISOR 上下文，而非仅停留在工具调用日志。"
```

截图落盘：`screenshot-20260915-005151.692.png`（`media:2483aa2af67d…`）。

## 四、ADVISOR 自己指出的缺口（下一件事）

同一次实跑的裁决里，ADVISOR 主动写了：

> (b) 至少验证 ADVISOR 能读取媒体内容而非只识别句柄（避免只证明句柄透传）

这是真缺口：ADVISOR 回合是一次**有界 LLM 调用**（`TLEvaluator`，无工具循环），
它拿到的是句柄 + 元数据，不是像素。要让它真的"看图评审"，两条路：

1. **给 b 回合挂图**：复用 `imageattach` 队列（把 EXEC 的截图按媒体 ref 挂到
   ADVISOR 的下一次请求上）——改动小，但要给 TL 会话建立随图通道；
2. **给角色配独立工具循环**：让 ADVISOR 自己调 `computer_screenshot` /
   `read_tool_result`（需要 `agentteam.TurnScheduler` 的生产接线，目前尚未接线）。

现状口径：**证据（句柄 + 元数据）已贯通；像素未贯通**，两条路都还没做。

## 五、验证

```text
go test ./application/core -run "SummarizeTurnWork" -count=1        # 证据抽取与有界性
go build -o tmp/bin/seelex-headless.exe .
$env:SMOKE_TEAM_WORK_COMPUTER_LIVE='1'
go test ./gui -run TestRealAPITeamWorkComputerUseLiveProbe -v -count=1 -timeout 20m
```

## 六、勘误（2026-09-16）

第三节里把「**标题命中=true**」和「screen/media 证据进入 ADVISOR 输入」并列成
"硬断言"，两者的强度其实**不一样**：

- `screen: media:… foreground="…"` 进入 ADVISOR 输入 = **真断言**（summary 抽取
  了工具结果里的句柄与元数据，可逐字核对）；
- `标题命中=true` = **弱断言**：`computer_screenshot` 的工具结果文本里本来就带
  `foreground.title`（`seelebridge/tools/computer/tools_view.go` →
  `tools.go:246`），模型照抄文本即可命中，不构成"看过像素"。

同一问题在 `computer_use_live_smoke_test.go` 里更严重——那条"回答命中"曾被当作
"模型看图回答"的证据。该冒烟已改为以**出站 provider 请求是否携带图像**为准，
详见 `docs/devlog/2026-09-16-computer-use-evidence-correction.md`。

另外，缺 `config/accounts.yaml` 时会静默回退到空 key 的 `gpt-4o` 假账号
（`seelebridge/internal/config/config.go` `fallbackConfig()`）——在这种装配下
任何 computer use 验证都不接真实能力。该行为同见上篇勘误，尚未改。
