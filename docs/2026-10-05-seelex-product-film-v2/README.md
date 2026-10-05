# Seelex 产品片 v2 · 「真 UI + 协作」素材包

本目录是 v2 产品片（重制版）的**素材包**：把「真实产品界面」与「真实团队协作记录」做成 artist 可直接合成的形态。
v1 被用户否决的原因是「没有实际的 ui 出现，全是抽象的概念和字幕」；本包只放**真机截图**与**可追溯的协作数据**，不放任何示意/假造界面。

> 归属：本包由 `asset_scout` 在里程碑 **m-film** 的一轮工作中整理产出。
> 只读来源全部落在本仓库内；明细见 `assets/manifest.json` 与 `assets/teamwork/collaboration-dataset.json`。

---

## 1. 来源（两类）

### A. 真机 UI（`assets/ui/01..07`）
- 出处：**运行中的 Seelex 桌面 GUI（Wails 应用）**，逐张截图后裁到应用窗口。
- 规格：全部 **1800 × 1080**，PNG。
- 说明：每张都是同一次运行、同一条会话在不同面板/滚动位置上的真实画面；`已加载窗口 201 / 会话共 401→419→428 条消息` 的递增说明它们是按时间顺序连拍的。

### B. 真实团队协作记录（`assets/teamwork/`）
- 出处：一次**真实 6 人团队**运行（`team_id = seelex-multiangle-review`）的原始落盘文件：
  `teamwork-plan.json`、`worktrees.jsonl`、`events.jsonl`、`board_team.json`。
- 时间：`events.jsonl` 记录区间 **2026-10-05T08:16:12Z → 08:53:03Z**（该团队从建队、并行派活、到封板的全过程）。

---

## 2. 拍摄时刻

| 文件 | 时刻（本机 +08:00） | 依据 |
|------|--------------------|------|
| `assets/ui/01-conversation.png` | 2026-10-05 17:38:07 | 文件 mtime |
| `assets/ui/02-trajectory.png` | 2026-10-05 17:38:15 | 文件 mtime |
| `assets/ui/03-worktable.png` | 2026-10-05 17:38:30 | 文件 mtime |
| `assets/ui/04-explorer.png` | 2026-10-05 17:38:37 | 文件 mtime |
| `assets/ui/05-conversation-teamwork.png` | 2026-10-05 17:40:18 | 文件 mtime |
| `assets/ui/06-employee-library.png` | 2026-10-05 17:41:02 | 文件 mtime |
| `assets/ui/07-team-tool-calls.png` | 2026-10-05 17:41:25 | 文件 mtime |
| `assets/ui/crop-*.png` | 2026-10-05 17:45–17:48 | 裁切生成时刻 |
| `assets/teamwork/*` 记录区间 | 2026-10-05 16:16:12 → 16:53:03 | `events.jsonl` 的 `at` 字段（UTC+8） |

> 注意：UI 截图（17:38–17:41）比 teamwork 记录（16:16–16:53）晚约 45 分钟，且分属**两条不同的会话**——UI 是 v2 制作会话的画面，teamwork 记录是上一次 6 人审查团队的运行。二者都是真实记录，本包只做并置，不做拼接式伪造。

---

## 3. 每张图证明了什么

| 文件 | 画面 | 证明了什么（可核对的画面文字） |
|------|------|-------------------------------|
| `01-conversation.png` | 对话视图（`对话/轨迹`），右栏 `状态` | 一个跑着的 agent 循环：`message-235/250/259`、`EXEC R1 17:36/17:37/17:38`、`工具过程 N 次 · bash / computer_screenshot`；右栏账户池 `账户 3` = `openai` + 三条 `deepseek-flash` 角色通道（`agent-1 / goal-plan-1 / subagent-1`） |
| `02-trajectory.png` | 轨迹视图 + `上下文轴` | 上下文工程可被看见：泳道 `简档/输入/LLM/工具/错误/系统/通知`，过滤器条计数 `输入1+LLM8+工具74+错误0+系统1+通知0 = 全部84`，`共 84 条 · 成功 82`，逐条带时间戳与字节数的工具结果 |
| `03-worktable.png` | 轨迹 + 右栏 `工作台` | 工作台面板真实存在：`定时任务 0`、`0 项任务 点击查看完整表格` |
| `04-explorer.png` | 轨迹 + 右栏 `资源管理器·工作区更改` | 会话背后的 git 工作树：`工作区更改 39`、`main 未跟踪 39`、逐行 `docs/2026-10-05-seelex-multi…` |
| `05-conversation-teamwork.png` | 对话视图（含团队工具行） | 会话流里出现 `工具过程 7 次 · team_plan / team_work / team_dispatch / bash / computer_focus` —— 团队派活循环在 UI 上可见 |
| `06-employee-library.png` | 对话 + 右栏 `状态` 展开出 `员工库` | 按角色雇人：`员工库 4` 四行 —— `reviewer/只读`、`techleader/继承`、`ADVISOR(tl)/读写`、`worker/读写`；脚注 `本会话在编名单与员工库有差异：会话里的行点「入库」写回母本` |
| `07-team-tool-calls.png` | 对话（团队工具行）+ 员工库 | 同一帧里既有团队派活工具行，又有员工库 —— v2 需要的「teammate 工作/协作镜头」的直接素材 |
| `crop-account-pool.png` | 裁切件 ← 01 | 账户池近景：`openai` + 三条 `deepseek-flash` 角色通道 |
| `crop-trajectory-filter-bar.png` | 裁切件 ← 02 | 轨迹过滤器条 + `共 84 条 · 成功 82` |
| `crop-tool-call-rows.png` | 裁切件 ← 04 | 四条工具结果行（`grep_search OK 2.7 KB`、`read_file OK 1.9 KB` …） |
| `crop-employee-library.png` | 裁切件 ← 06 | 员工库四行近景（teammate 特写镜头用） |

---

## 4. 分镜与数据

- 分镜脚本（128 BPM × 8 小节 = 15.000 s，8 场全带真 UI，3 场 teammate）：见 [`STORYBOARD.md`](../STORYBOARD.md)
- 协作数据集（team / 2 里程碑 / 6 工作项 / 6 worktree / 带时间戳事件线）：见 [`assets/teamwork/collaboration-dataset.json`](teamwork/collaboration-dataset.json)
- 逐图 manifest（像素尺寸 / 逐面板标签文字 / 3–4 个出片子区域归一化裁切框）：见 [`assets/manifest.json`](manifest.json)

---

## 5. 硬规矩自检

- **不许编事实**：manifest 的 `labels` 是从对应 PNG 逐像素读回后转写的；`collaboration-dataset.json` 的每个值都带 `provenance.sources`，缺就省略（例如团队成员只写了记录里有的 `role/tools_policy/plugins`）。
- **不占盘**：只新增 4 张**裁切件**（多则 1060×318，小则 610×106，合计 < 80 KB）；没有把原始大图复制成一堆 frames。
- **只改本目录**：全部新增/修改都在 `docs/2026-10-05-seelex-product-film-v2/` 下。
