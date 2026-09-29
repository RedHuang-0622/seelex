# dev 包配置漂移：开关打开了，运行中的 GUI 却读着旧档

- 日期：2026-09-29
- 症状：`limits.context_compaction_summary.enabled` 已在仓库改成 `true` 并提交（`ae941a0`），
  重启 GUI 后折叠产出**依然是本地确定性摘要**——「LLM 内容」一节仍然空（六节里三节恒 `(none)`）
- 范围：`scripts/build-dev.sh`（补配置同步步）；受影响的运行物是 `dist/seelex-gui-dev/`
- 结论：**配置改动没到运行进程手里**。仓库与包内各有一份 `config/seelex.yaml`，二进制按
  CWD 相对路径读**包内那份**；dev 构建只换 exe、不换配置，包内那份冻结在 09-26

---

## 1. 症状与现场

用户报「LLM 内容依然没有」。这里的"LLM 内容"指折叠帧 Chapter 2 的**前缀重放厚摘要**
（`summary_source=replay`）——它是这一整条线唯一的模型产物；关着开关时帧走本地确定性折叠
（`summary_source=local`），Chapter 2 是任务台账的元数据投影。

现场帧（会话 `session-7834e40e49e594bc` 的 `compact.jsonl`，`compressed_at`
`2026-09-29T18:00:08+08:00`）逐条对得上**本地折叠**的四枚指纹：

| 帧里的样子 | 出处 |
|---|---|
| `### 目标 (Goal)` = `(none)` | `localGoal`：`TaskStack` 空 |
| `### 关键概念 (Key Concepts)` = `(none)` | `localKeyConcepts`：`PlanStack` 空 |
| `### 文件与代码 (Files and Code)` = `- 工具: bash_read` … | `localFilesAndCode`：从溢出单元提工具名 |
| `### 错误与修复 (Errors and Fixes)` / `### 待办 (Pending)` / `### 下一步 (Next Step)` = `(none)` | `LocalChapter2WithCarry` 里**硬编码** `compactEmptySection`，与内容无关 |
| `### 当前工作 (Current Work)` = `溢出轮次: 73 个完整协议单元` | `localCurrentWork` 的计数行 |

第三、四、五行是决定性的：`(none)` 在这三节不是"没抽到内容"，而是本地折叠根本不写这三节
（`seelexctx/frame.go`）。所以这一帧不是"模型写砸了"，而是**压根没调用模型**。

## 2. 排查链

排除法一路走到"配置"这一层：

1. **开关自身对不对？** 有钉子：`context_compaction_summary.enabled: true` 落在 `limits:` 下，
   由 `TestShippedCompactionSummarySwitchShipsOpen` 钉住，实际加载后 `Enabled == true`。不是键名/嵌套写错。
2. **折叠走的哪条路？** 帧里那句「会话没有在飞回合（冷加载或刚清空），已按会话级显式压缩立即执行」
   出自 `application/core/context_compact.go` 的 `compactionRecordNote`——即 `/compact` / `compact_context`
   那条**装配层**折叠。它推帧走 `pushCompactionFrame` → `Runtime.PushCompactionFrame` →
   `MainCompactionDAG`，而 `MainCompactionDAG` **是注入 Summarizer 的那一份**（控制器那份故意不注入）。
   所以这条路径不受"回合内路径不注入"的限制。
3. **推帧素材够不够？** `ReplayHistory: existing`（上一次真实请求的引擎历史）非空；`chapter2Node`
   的前缀重放前提在这条路径上是满足的。
4. **那模型为什么没被叫？** 只剩一种可能：`compactionSummarizer()` 返回了 `nil`。它有三个 nil 出口，
   前两个（开关关、QuickChat 装配失败）都不报错、静默落到本地折叠。于是回看**运行进程读的是哪份配置**。

## 3. 根因（证据链）

| 时间 | 事实 |
|---|---|
| 09-26 18:19:50 | `dist/seelex-gui-dev/config/seelex.yaml` 最后一次被写（7067 字节） |
| 09-29 17:23:55 | 仓库 `config/seelex.yaml` 改成 `enabled: true`（17232 字节） |
| 09-29 17:25:28 | 提交 `ae941a0` |
| 09-29 17:25:36 | post-commit 钩子重建 `dist/seelex-gui-dev/seelex-gui.exe` |
| 09-29 17:59:50 | GUI 进程启动（PID 152，`dist/seelex-gui-dev/seelex-gui.exe`） |
| 09-29 18:00:08 | 它折叠出的帧仍是 `summary_source=local` |

两处口径咬合：

- **二进制按 CWD 相对路径找配置**：`main.go:525` `runtimeConfigPath := firstExisting("config/seelex.yaml", "seelex.yaml")`，
  紧接着 `seelexctx.LoadLimits(runtimeConfigPath)`。GUI 从包目录启动，于是读的是**包内那份**。
- **dev 构建只管 exe、不管配置**：`scripts/build-dev.sh`（post-commit 钩子入口）只跑两条 `go build`。
  包内 `config/` 只有 `build-gui.ps1` 会刷（它第 74–75 行 `Copy-Item` `seele.yaml` / `seelex.yaml`），
  而这次提交走的是钩子，不是 `build-gui.ps1`。

而包内那份 7067 字节的旧档里**连 `context_compaction_summary` 这个键都没有**（它连
`retain_tokens` / `context_soft_percent` 这一代键也还没有）——`LoadLimits` 读到"整块缺失"，
`ContextCompactionSummary.Enabled` 取零值 `false`。**仓库里打开多少次都不会生效。**

顺带查出同一类漂移的第二处：包内 `config/seele.yaml` 停在 09-16，缺 `computer_scroll_targets: ask`
这条规则（默认动作兜住了，没有安全洞，但规则面同样在漂）。

## 4. 修复

`scripts/build-dev.sh` 增加 `sync_package_config`：把仓库 `config/seelex.yaml`、`config/seele.yaml`
同步进 `dist/seelex-gui-dev/config/`。三条口径：

- **幂等**：内容相同就跳过（用 `cmp -s` 比字节，不比 mtime），不产生无意义的写；
- **只同步这两个文件**：`accounts.yaml` 是本地凭据（构建时由 `-LocalConfigPath` 显式指定，包内那份
  可能已被用户在界面上改过），脚本一律不碰；
- **无条件执行**：即使 `SKIP_BUILD_GUI=1`（CLI-only 快提交）也同步，因为"包内配置落后于 HEAD"
  本身就是缺陷，与这次要不要重建 GUI 无关。

同步落地点写在脚本注释里（含本次事件的坐标），避免下一个人再把它当"没生效"重新排查一遍。

## 5. 验证

```powershell
# 同步前：包内 7067 字节、无该键；仓库 17232 字节
$env:SKIP_BUILD_GUI="1"; bash scripts/build-dev.sh
#   [build-dev] config: seelex.yaml -> dist/seelex-gui-dev/config/
#   [build-dev] config: seele.yaml -> dist/seelex-gui-dev/config/
# 同步后：包内与仓库**逐字节相同**（SequenceEqual=True），且含 enabled: true
# 再跑一次：config 行 0 条（幂等）
# accounts.yaml LastWriteTime 仍为 2026/9/16 20:53:07（未被触碰）
```

## 6. 遗留（如实记账）

- **GUI 需重启**：配置在启动时读一次（`main.go` 的 `LoadLimits`）。本机那次 GUI 进程又正好是
  承载当前会话的进程，不能替用户重启——所以"重启后帧是否变成 `replay`"这一条要用户侧确认。
- **静默降级的可观测性**：`compactionSummarizer()` 的三个 nil 出口都不留痕（`windowsgui` 构建下
  `log.Printf` 也无处可看）。这次能定位靠的是"配置根本没有这个键"这一实物证据；若将来是
  QuickChat 装配失败，现场同样安静。回执里的门禁 `index=… source=local` 是唯一线索。
- **`dist/dev/`（CLI 包）根本没有 `config/`**：从包目录直接跑 CLI 时，limits 与权限规则全走代码默认值，
  连 `config/seelex.yaml` 都不读。本次没有一并处理（CLI 还缺 accounts，包内自成一套语义），
  仅在此记账。
