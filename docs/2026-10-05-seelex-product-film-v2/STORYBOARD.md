# Seelex 产品片 v2 · 分镜脚本（STORYBOARD）

- 网格：**128 BPM × 8 小节 = 15.000 s**（1 拍 = 0.46875 s，1 小节 = 1.875 s，全片 32 拍）
- 素材根：`assets/ui/`（全部为**运行中 Seelex GUI 的真机截图**，1800×1080）+ `assets/teamwork/`（一次真实 6 人团队的原始记录）
- 硬约束自检：**8 场全部有真实 UI 在画面上（≥5 通过）**；**S5 / S6 / S7 三场讲 teammate 工作与协作（≥2 通过）**
- 文案语言：英文；**每拍 ≤ 4 词**（下面每场给 2 拍）

---

## S1 · bar 1 — `00:00.000 – 00:01.875`
- **画面素材**：`assets/ui/01-conversation.png`（真机对话视图，整张）
- **屏幕文案**（2 拍）：`Real agents.` / `Real interface.`
- **镜头运动**：从 1.02× 缓慢推近到 1.06×，落点在对话列（message-235…259 的工具过程行），右栏账户池保持入画

## S2 · bar 2 — `00:01.875 – 00:03.750`
- **画面素材**：`assets/ui/crop-tool-call-rows.png`（裁切件，来自 04-explorer.png）
- **屏幕文案**：`Every tool call.` / `Logged. Timed.`
- **镜头运动**：自下而上匀速平移，扫过四行工具结果（`grep_search OK 2.7 KB` → `read_file OK 1.9 KB`）；每拍强拍定格一次
- **真 UI**：画面即真实的 `工具/grep_search/read_file` 行与字节数

## S3 · bar 3 — `00:03.750 – 00:05.625`
- **画面素材**：`assets/ui/02-trajectory.png`（真机轨迹视图）+ 裁切件 `assets/ui/crop-trajectory-filter-bar.png` 从右滑入
- **屏幕文案**：`Budget-driven context.` / `Pinpoint any turn.`
- **镜头运动**：沿上下文轴横向滑移（简档 / 输入 / LLM / 工具 / 错误 / 系统 泳道），过滤器条停在强拍上

## S4 · bar 4 — `00:05.625 – 00:07.500`
- **画面素材**：裁切件 `assets/ui/crop-account-pool.png`（来自 01-conversation.png）
- **屏幕文案**：`Route by role.` / `One pool. Many models.`
- **镜头运动**：定格 + 1.0→1.06 缓推；三行 `deepseek-flash` 按 `agent-1 / goal-plan-1 / subagent-1` 依次高亮，`openai` 通道常亮
- **真 UI**：账户池面板原文（`账户 3`、`openai`、三条 deepseek-flash 角色通道）

## S5 · bar 5 — `00:07.500 – 00:09.375` ★teammate
- **画面素材**：裁切件 `assets/ui/crop-employee-library.png`（来自 06-employee-library.png，真机员工库）
- **屏幕文案**：`Hire by role.` / `reviewer. techleader. worker.`
- **镜头运动**：推近到员工库四行；随拍点逐行淡入高亮
- **真实数据**（逐字取自 06 画面／记录）：`reviewer（reviewer · agent，只读）`、`techleader（techleader · agent，继承）`、`ADVISOR（tl · agent，读写）`、`worker（worker · agent，读写）`；脚注 `本会话在编名单与员工库有差异：会话里的行点「入库」写回母本`
- **真 UI + 协作**：员工库 = 一个中控按角色雇人

## S6 · bar 6 — `00:09.375 – 00:11.250` ★teammate
- **画面素材**：`assets/ui/07-team-tool-calls.png`（真机对话视图，含 `team_plan / team_work / team_dispatch` 工具过程行）+ `assets/ui/05-conversation-teamwork.png` 叠化
- **屏幕文案**：`Six teammates.` / `One worktree each.`
- **镜头运动**：从 `message-340` 块下摇到 `工具过程 · team_plan / team_work / team_dispatch` 行；右栏员工库保持入画
- **真实数据**（取自 `assets/teamwork/`）：team_id `seelex-multiangle-review`，6 名成员，6 件工作项各占一个 worktree：`seelex/front_auditor-wi-front-code`、`seelex/ux_auditor-wi-front-ux`、`seelex/feature_auditor-wi-feature`、`seelex/arch_auditor-wi-arch`、`seelex/market_researcher-wi-market`、`seelex/artist-wi-film`

## S7 · bar 7 — `00:11.250 – 00:13.125` ★teammate
- **画面素材**：`assets/ui/03-worktable.png`（真机真机工作台面板：`定时任务 0 / 0 项任务`）
- **屏幕文案**：`Milestones gate work.` / `Five audits. One product.`
- **镜头运动**：轻微手持漂移；真实事件时间戳逐条叠上（08:16:26 排活 → 08:21:15 五条全部 accept → 08:21:31 派 a6）
- **真实数据**（取自 `assets/teamwork/events.jsonl`）：里程碑 `m-audit`（5 项）→ `m-product`（`depends_on: m-audit`，1 项）；首次派发 `08:16:28.8097951Z` 到五条全部验收 `08:21:15.5899296Z` = 287 s

## S8 · bar 8 — `00:13.125 – 00:15.000`
- **画面素材**：`assets/ui/04-explorer.png`（真机资源管理器 · `工作区更改 39`）
- **屏幕文案**：`Then it ships.` / `Seelex.`
- **镜头运动**：从 1.06× 拉远到 1.0× 并定格在 `So what shipped` 的落款；定格帧停在 15.000 s
- **真 UI**：`main 未跟踪 39` 与 `docs/2026-10-05-seelex-multi…` 文件行

---

## 落点表（剪辑对齐）

| 场 | 小节 | 入点 (s) | 出点 (s) | 主体素材 | 真 UI | teammate |
|----|------|----------|----------|----------|:----:|:-------:|
| S1 | 1 | 0.000 | 1.875 | 01-conversation.png | ✓ | |
| S2 | 2 | 1.875 | 3.750 | crop-tool-call-rows.png | ✓ | |
| S3 | 3 | 3.750 | 5.625 | 02-trajectory.png + crop-trajectory-filter-bar.png | ✓ | |
| S4 | 4 | 5.625 | 7.500 | crop-account-pool.png | ✓ | |
| S5 | 5 | 7.500 | 9.375 | crop-employee-library.png | ✓ | ✓ |
| S6 | 6 | 9.375 | 11.250 | 07-team-tool-calls.png (+05 叠化) | ✓ | ✓ |
| S7 | 7 | 11.250 | 13.125 | 03-worktable.png | ✓ | ✓ |
| S8 | 8 | 13.125 | 15.000 | 04-explorer.png | ✓ | |

- 真 UI 场次：**8 / 8**
- teammate 工作·协作场次：**S5、S6、S7 = 3**
