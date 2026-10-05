# 步骤② + ③ 交付记录：同一判据重复收成一份（现场层 / 进程树层 / 读面折算）

一次性工作包（`docs/2026-10-06-workunit-jobs-port/`）。任务书 = `docs/arch/workunit-ports-and-assembly.md` §5②③；
`③` 的权威口径 = `docs/2026-10-06-workunit-jobs-port/step-3-goal.md`；`②` 的权威口径 = `step-2-handoff-prompt.md`
＋读面勘定 `docs/arch/workunit-progress-read-surface.md`。盘点来源 = `docs/arch/workunit-duplication-inventory.md`。

**基线**：本波开工头 = `49455b5`（步骤① 的 `a6f8f2b` 与两张作业表都在它之下），开工时工作区干净。

**一句话结论**：现场层四条判据（释放 / 脏 / 在册比较与命名 / 清单解析）与工具链层的进程树装配、
读面的折算 / 编解码 / 预览上界 / 恢复说明容器，各自收成**一处实现**；**两张作业表一张未搬**
（§12.4 裁决），现场层与契约层的依赖方向从「契约 → 实现」正成「实现 → 契约」。

| 判据 | 收口前 | 收口后唯一位置 |
|---|---|---|
| ③A 现场释放（幂等） | 2 份（组件 `w.cleanup` 非幂等 / 包级 `CleanupWorktree` 幂等） | `worktree.cleanupWorktreeWith` `worktree_manager.go:929` |
| ③B 工作区是否脏 | 3 份（`w.pathDirty` / `w.worktreeDirty` / 编排面包级 `worktreeDirty`） | `worktree.pathDirtyWith` `worktree_manager.go:787` |
| ③D 现场命名 / 在册比较 | 2 份命名拼法 + 2 份比较口径 + 2 份清单解析 | `sceneDirName/sceneDirPrefix` `:386/:391` + `worktreePathEqual` `:996` + `parseWorktreeList` `:736` |
| ③C 进程树装配 | 2 份（同步链内联 / 后台链内联） | `tools.newProcessTreeCommand` `router.go:563` + `startWithProcessTree` `:577` |
| ③E 契约对 worktree 的依赖 | 契约 import 实现包（门禁例外 1 项） | 哨兵进 `workunit/classify.go:28/33`；门禁改零命中硬判据 |
| ②① 记录 → 进度折算 | 3 份手写 | `workunit.ProgressOf` `progress.go:157`（＋`UnitReader` `:195`） |
| ②② 阶段载荷编解码 | 2 份 | `workunit.EncodeStages` `:68` / `DecodeStages` `:91` |
| ②③ 阶段预览上界 | 4 份实现、2 个数、2 种计量单位 | `workunit.StagePreviewLimit` `:53` + `ClipPreview` `:116` |
| ②④ 恢复说明容器 | 2 份同形 map | `resumeNotes` `seelebridge/resume_notes.go:25` |

---

## 1. 改动文件清单（8 次提交，基线 `49455b5`）

`git diff --stat 49455b5..HEAD` → **25 个文件，+1348 / −343**。

| 提交 | 标题 | 文件 |
|---|---|---|
| `ddb256e` | ③A 现场释放收成一份判据 | `worktree/worktree_manager.go`、新增 `worktree/worktree_release_judgment_test.go` |
| `f54fe60` | ③B 脏判定收成一份判据 | `worktree/worktree_manager.go`、`runtime_teamwork.go`、新增 `worktree/worktree_dirty_judgment_test.go` |
| `d657b95` | ③D 命名与在册比较收成一份口径 | `worktree/worktree_manager.go`、新增 `worktree/worktree_scene_naming_test.go` |
| `d2b26f2` | ③C 进程树装配收成一份 | `tools/async_run.go`、`tools/router.go`、新增 `tools/process_tree_assembly_test.go` |
| `bfd6c7e` | ② 判据 / 读法 / 容器各自一份 | `workunit/progress.go`、`workunit/progress_test.go`、`workunit_assembly.go`、`workunit_team_records.go`、`runtime_subagent_resume.go`、`runtime.go`、`internal/telemetry/stage_hook.go`、新增 `resume_notes.go`、新增 `stage_preview_judgment_test.go` |
| `e537aed` | ③E 哨兵错误搬进契约 + 撤门禁例外 | `workunit/classify.go`、`worktree/worktree_manager.go`、`e2e/workunit_ports_test.go`、`workunit/contract_test.go`、`workunit/README.md`、`docs/arch/workunit-ports-and-assembly.md` |
| `cd1cb64` | 右腿：一次账本读 + 性能量级读数 | `workunit_assembly.go`、`workunit_team_records.go`、新增 `workunit/progress_bench_test.go`、新增 `worktree/worktree_judgment_bench_test.go` |
| `889527b` | ③U6 部分回答 | `runtime_subagent_recovery.go`、`stage_preview_judgment_test.go` |

**未动（逐条核查过）**：两张作业表（见 §6）、`seelebridge/workunit_parent.go`（基类，本轮零改动）、
`workunit/contract.go` 的作业面签名、`workunit/contract_test.go` 之外的所有既有用例、
`worktree` 对 `workunit` 之外的公共 API、前端渲染、Seele `jobs` 包。

**唯一一处既有用例的机械适配（已声明）**：`workunit/contract_test.go`（`e537aed` 内）把夹具里的
`worktree.ErrUncommittedChanges` 改用包内哨兵名——不换就会形成 `workunit → worktree → workunit`
的测试 import 环。**同一个错误值，断言语义未改**。

---

## 2. 删除清单（文件:行 → 旧位置去向 → 新唯一位置）

行号均为**该提交的父提交**里的实测行号（`git grep -n <父提交>` 读数）。

### 2.1 ③A 现场释放（`ddb256e`，父 = `ddb256e^`）

| 旧位置 | 旧位置去向 | 新唯一位置 | 证据 |
|---|---|---|---|
| `worktree/worktree_manager.go:822` `w.cleanup(root, wt)` 的**实现体**（裸 `worktree remove --force` → `branch -D`，无「已释放」判据） | 实现体**删除**，函数留一行转调 | `cleanupWorktreeWith` `:929` | 组件入口转调 `:876-878` |
| `:865` 包级 `CleanupWorktree(root, wt)` 的**实现体**（与上者逐字同形，但带幂等判据） | 实现体**删除**，留一行转调 | 同上 | 包级转调 `:908` |
| `:898` `gitWorktreeRegistered(root, path)`（人读格式 `git worktree list` 自己解一遍） | 解析**删除** → 薄包装 `worktreeRegisteredWith`（`parseWorktreeList` + `worktreePathEqual`） | `parseWorktreeList` `:736` | `gitWorktreeRegistered` `:975` → `worktreeRegisteredWith` `:980` |
| `:695` `listWorktrees(root)`（porcelain 解析体） | 解析**删除** → 薄包装（留名，调用点零改动） | 同上 | `listWorktrees` `:726` 只剩 `return parseWorktreeList(out), nil` |

**幂等口径的唯一实现**：判据 =「git 登记不在（或 `remove` 成功）= 已释放」；「目录不在」「分支不在」
两种形态由 `remove` 的失败原文 + 登记查询一起兜住；`remove` 真失败时把 runner 的 stderr（git 原文）带进报错。

### 2.2 ③B 脏判定（`f54fe60`，父 = `f54fe60^`）

| 旧位置 | 旧位置去向 | 新唯一位置 |
|---|---|---|
| `worktree/worktree_manager.go:742` `w.pathDirty(path)` 实现体 | 实现体**删除** → 转调 `pathDirtyWith(w.git, path)`（`:766-768`） | `pathDirtyWith` `:787` |
| `:803` `w.worktreeDirty(wt)` 实现体（与上者**逐字相同**） | 实现体**删除** → 转调 `w.pathDirty(wt.Path)`（`:848-849`） | 同上 |
| `runtime_teamwork.go:860` 包级 `worktreeDirty(root)` 实现体（第三份拷贝） | **整个函数删除**；调用点 `:809` 改 `worktree.PathDirty(path)` | 包级 `PathDirty` `:774` |

**CRLF 幻影脏的唯一修复点**：`seelebridge/worktree/worktree_manager.go:788`
（`pathDirtyWith` 里的 `out, err := git(path, "status", "--porcelain")`；`:780` 起有 ⚠ 注释指明"只在这里改一处"）。
全仓 `status --porcelain` 的**判定调用**只剩这一处（其余命中全是 fake git 的桩、用例断言与文档；见 §5.2）。

### 2.3 ③D 命名与在册比较（`d657b95`，父 = `d657b95^`）

| 旧位置 | 旧位置去向 | 新唯一位置 |
|---|---|---|
| `worktree/worktree_manager.go:376` `w.scenePath` 里手拼 `fmt.Sprintf("%s-seelex-%s", ...)` | 拼法**删除** → `scenePath` 只调 `sceneDirName`（`:396`） | `sceneDirName` `:386` / 常量 `sceneNameInfix` `:383` |
| `:736` `w.isManagedPath` 里手拼 `prefix := filepath.Base(root) + "-seelex-"` | 拼法**删除** → 调 `sceneDirPrefix`（`:763`） | `sceneDirPrefix` `:391` |
| `:586` `w.Restore` 用 `os.Stat` 判「目录是否存在」后**逐字符**比登记键 | 比较口径**删除** → 一律走 `worktreePathEqual`（`:645-653`） | `worktreePathEqual` `:996` |
| `:700` `Prune` 的主工作区比较 `entry.path == root`（逐字符） | 改为 `worktreePathEqual(entry.path, root)` | 同上 |

**保留的语义（明写不许被顺手删掉）**：`Restore` 的「目录不存在不登记」防幽灵判据（`os.Stat` +
`!info.IsDir()`）原样保留——D 只统一**路径比较口径**，不取消「目录不存在不登记」。

### 2.4 ③C 进程树装配（`d2b26f2`，父 = `d2b26f2^`）

| 旧位置 | 旧位置去向 | 新唯一位置 |
|---|---|---|
| `tools/async_run.go:245` `tree := security.NewProcessTree()`（后台链内联装配起点） | **整段删除**（`245`–`285` 的装配：`NewProcessTree` → `winhide.Apply` → `cmd.Dir` → `ConfigureHiddenCommand` → `ConfigureProcessTree`(`:252`) → `cmd.Cancel = tree.Terminate()`(`:255`) → `WaitDelay`） | `newProcessTreeCommand` `router.go:563` |
| `tools/async_run.go:276` `_ = tree.Attach(cmd.Process.Pid)`（内联 Start/Attach） | **删除** → 走同一份 | `startWithProcessTree` `router.go:577` |

**为什么不把超时策略也并进去**（判据 C 的必答项）：装配（树 + 起命令位 + 取消换整组终止 + WaitDelay）
没有理由分家；**超时/取消策略有意不并**，两者语义不同：

- 后台（`startAsync`，`async_run.go:245`）：`context.WithTimeout(context.WithoutCancel(ctx), asyncHardCap)`
  ——受理回执一返回，本次工具调用的 ctx 就失效，沿用它会把刚起的命令连带杀掉；
- 同步（`executeScopedBash`，`router.go:599`）：`context.WithTimeout(ctx, r.scopedToolTimeout(...))`
  ——受本次工具调用预算约束、可被「停止」取消。

理由写进了 `router.go:557-562` 的函数注释（`runCtx 由调用方给——超时/取消策略有意不并`）。

### 2.5 ② 折算 / 编解码 / 预览 / 容器（`bfd6c7e`，父 = `bfd6c7e^`）

| 旧位置 | 旧位置去向 | 新唯一位置 |
|---|---|---|
| `workunit_assembly.go:357` `(p *hostPorts) recordFor`（自己列账本 + 线性查找一条记录） | **删除** | `UnitReader.Record` `progress.go:238` |
| `workunit_assembly.go:373` `(p *hostPorts) sessionRecords`（自己列账本） | **删除** | `UnitReader.Records` `progress.go:229` / `List` `:255` |
| `workunit_team_records.go:348` `teamUnitRecords` 的手写折算（List + 按名单过滤 + 自判 InFlight） | 折算体**删除** → `teamUnitReader` + `ProgressOf`（`:249`） | `ProgressOf` `progress.go:157` |
| `runtime_subagent_resume.go:535-542` 用**匿名结构**自己解 `StagesJSON` | 匿名结构**删除** → `workunit.DecodeStages`（`:546`） | `DecodeStages` `progress.go:91` |
| `workunit_team_records.go:112` `teamUnitStages(stage, preview) []byte`（`[]map[string]string` 自编） | **删除** → `workunit.EncodeStages`（`:85`） | `EncodeStages` `progress.go:68` |
| `workunit_team_records.go:129` `clipTeamPreview` + `:126` `const teamUnitPreviewLimit = 240` | **删除** → `workunit.ClipPreview`（`:84/:116`） | `ClipPreview` `progress.go:116` |
| `internal/telemetry/stage_hook.go:18` `const stagePreviewMax = 200` + `:66-67` **按字节**裁 `preview[:200]` | **删除** → `workunit.ClipPreview`（同上） | `StagePreviewLimit = 240`（rune）`progress.go:53` |
| `runtime.go:230` `teamResume teamResumeState` + `workunit_team_records.go:201` `type teamResumeState struct` + `:207 setTeamResumeNote` / `:221 consumeTeamResumeNote` | 结构体与方法**删除** → 同一个容器类型 | `resumeNotes` `resume_notes.go:25`（`set/take/peek/clear` `:31/49/65/80`） |
| `runtime_subagent_resume.go:51` `notes map[string]string`（子代理侧同形第二份） | 字段**换类型** → `notes resumeNotes`（`:52`） | 同上 |

**语义变化（唯一一条，已声明）**：子代理阶段预览 **200 字节 → 240 rune**。
判据 = 「同一件事不许有两个数、两种计量单位」；副作用是按字节裁会把多字节字符切断
（落盘/恢复说明里出现非法 UTF-8），这正是 §3.2 红灯原文里抓到的那条。

### 2.6 ③E 契约依赖方向（`e537aed`，父 = `e537aed^`）

| 旧位置 | 旧位置去向 | 新唯一位置 |
|---|---|---|
| `worktree/worktree_manager.go:60` `var ErrUncommittedChanges = errors.New("worktree finish protocol not executed")` | **定义删除** → 同名 var 变成**引用**（`:64`） | `workunit/classify.go:28` |
| `worktree/worktree_manager.go:76` `var ErrMergeBlockedByMain = errors.New(...)` | 同上（`:83`） | `workunit/classify.go:33` |
| `worktree/worktree_manager.go` 的两个 `Is*` 判据实现 | 变成一行转调（`:70` / `:86`） | `workunit/classify.go:37/40` |
| `e2e/workunit_ports_test.go` 的「契约包里 import worktree 只允许 `classify.go`」**例外表** | **删除** → 改**零命中**硬判据（门禁本体 `:207-222`，`:210` 是那次扫描调用） | 判据本体 + 阴性对照（临时假契约包目录 `:285-320`） |

公共 API 名字不变（`worktree.ErrUncommittedChanges` 等照旧可用），因此既有调用点
（`runtime_teamwork.go:814`、worktree 自己的 3 处、5 个既有测试文件）**零改动**。

### 2.7 右腿的一次读（`cd1cb64`，父 = `cd1cb64^`）

| 旧位置 | 旧位置去向 | 新唯一位置 |
|---|---|---|
| `workunit_team_records.go:337` `teamUnitProgressMap(list)` + 调用点 `:244`（把清单再索引一遍） | **删除** → 一次 `reader.Records()` 同时供「在跑判定」与「恢复说明的原始记录」（`:245`） | `UnitReader.Records` `progress.go:229` |
| `workunit_assembly.go` 的 `Readout` 走 `List()` + `Record()` **读两遍账本** | 第二次读**删除** → 一次 `Records()` 取条数与那一条（`:307`） | 同上 |

---

## 3. 红 → 绿原文（真跑，非转述）

方法：把**收口前的那个源文件**换回工作区（其余一切不动）→ 跑新用例（红）→ 还原 → 再跑（绿）。
两步都真跑，跑完 `git status --porcelain` 为空（工作区复原）。

### 3.1 ③A 旗舰红灯：两条入口两个结论（本次真跑）

工作区 = `ddb256e^:seelebridge/worktree/worktree_manager.go`（旧的 `w.cleanup` / `CleanupWorktree`），
用例 = `worktree_release_judgment_test.go`（判定当次引入）。

```text
=== RUN   TestSceneReleaseJudgmentIsSingleAcrossEntries
=== RUN   TestSceneReleaseJudgmentIsSingleAcrossEntries/①_同一现场连续释放两次：第二次必须是「已释放」
    worktree_release_judgment_test.go:145: [组件 w.cleanup（收尾自动释放）] err = git [worktree remove --force ...\002-seelex-wi-release-judgment]: exit status 128，wantErr = false
    worktree_release_judgment_test.go:153: 两条入口结论不一致（判据不是一份）：包级 CleanupWorktree（验收释放） → <nil>；组件 w.cleanup（收尾自动释放） → git [worktree remove --force ...]: exit status 128
=== RUN   TestSceneReleaseJudgmentIsSingleAcrossEntries/②_目录已被上游收走_=_已释放
    ...（同上两条：包级 → <nil>；组件 → exit status 128）
=== RUN   TestSceneReleaseJudgmentIsSingleAcrossEntries/③_分支已被上游删掉_=_已释放
=== RUN   TestSceneReleaseJudgmentIsSingleAcrossEntries/④_目录还在但_git_登记没了（prune_/_手工移走）=_已释放
    ...（同上两条）
=== RUN   TestSceneReleaseJudgmentIsSingleAcrossEntries/⑤_目录还在且仍登记、remove_失败_→_照旧返回_git_原文（不许被幂等口径抹掉）
    worktree_release_judgment_test.go:148: [包级 CleanupWorktree（验收释放）] ⑤ 必须把 git 原文交回调用方（应含 lock），得到：git [worktree remove --force ...]: exit status 128
    worktree_release_judgment_test.go:148: [组件 w.cleanup（收尾自动释放）] ⑤ 必须把 git 原文交回调用方（应含 lock），得到：git [worktree remove --force ...]: exit status 128
--- FAIL: TestSceneReleaseJudgmentIsSingleAcrossEntries (16.06s)
    --- FAIL: .../① (3.48s)   --- FAIL: .../② (2.71s)   --- PASS: .../③ (3.00s)
    --- FAIL: .../④ (2.97s)   --- FAIL: .../⑤ (3.90s)
FAIL	github.com/RedHuang-0622/seelex/seelebridge/worktree	17.725s
red_exit=1
```

**绿**（还原后，同一用例）：

```text
ok  	github.com/RedHuang-0622/seelex/seelebridge/worktree	11.845s
green_exit=0
```

读数结论：①/②/④ 三种「已释放」形态下，**包级入口说 `nil`（已释放）、组件入口说 `exit status 128`**——
同一份现场、两条入口两个结论，这就是「判据有重复」的可观测代价；⑤ 另抓到「失败只剩 `exit status`、
git 原文丢了」这一半（幂等口径把报错原文一起吃掉）。③（分支被摘掉）当时本来就一致，红/绿两态都 PASS。

### 3.2 ② 跨层红灯：同一件事在两层解出不同的打点（本次真跑）

工作区 = `bfd6c7e^:seelebridge/internal/telemetry/stage_hook.go`（**按字节**裁到 200 的旧实现，其余一切不动），
用例 = `stage_preview_judgment_test.go`（判定当次引入）。

```text
=== RUN   TestStagePreviewBoundIsOneAcrossProducers
    stage_preview_judgment_test.go:83: 同一份收尾在两层解出的打点不相同：subagent={Stage:turn Preview:很长的预览…很��…} teammate={Stage:turn Preview:很长的预览…（240 rune）…}
    stage_preview_judgment_test.go:86: 阶段钩子产出的预览不是合法 UTF-8（按字节裁切断了多字节字符）："很长的预览很…很\xe9\x95…"
--- FAIL: TestStagePreviewBoundIsOneAcrossProducers (0.00s)
=== RUN   TestResumeNotePreviewNeverSplitsARune
--- PASS: TestResumeNotePreviewNeverSplitsARune (0.00s)
FAIL
FAIL	github.com/RedHuang-0622/seelex/seelebridge	0.242s
red2_exit=1
```

**绿**（还原后，同一批用例）：

```text
ok  	github.com/RedHuang-0622/seelex/seelebridge	0.175s
green2_exit=0
```

读数结论：红灯同时钉住**两件**事——(a) 同一份收尾在 subagent 链（钩子侧 200 字节）与 teammate 链
（契约 240 rune）解出**不相等**的 `Stage`；(b) 按字节裁**切断了多字节字符**，落盘进记录/恢复说明的是
非法 UTF-8（原文里的 `\xe9\x95`）。

### 3.3 其余红灯的原文位置

| 红灯 | 形状 | 原文位置 |
|---|---|---|
| ③B 脏判定两条入口两份函数体 | `worktree_dirty_judgment_test.go` 的「两次判定期间除 `status --porcelain` 之外不许有别的 git 调用」 | 提交 `f54fe60` 信息 + 用例头注释 `:1-16` |
| ③D 换写法的现场被判成不在册 | `worktree_scene_naming_test.go` 的 `TestSceneRegistrationAgreesAcrossPathSpellings` | 提交 `d657b95` 信息 + 用例头注释 `:1-10` |
| ③C 两条链装配分家 | `process_tree_assembly_test.go`（四条不变式两条链逐条比） | 提交 `d2b26f2` 信息 + 用例头注释 `:1-12` |
| ③E 契约 → 实现的反向依赖 | 门禁阴性对照（假契约包目录喂同一扫描函数） | `e2e/workunit_ports_test.go:233`（对照组）、`:285-320`（假目录 + 干净目录两侧） |

这四条的红灯形状**当时未经"换回旧文件"复跑**（只做了"先写用例 → 看它红 → 再改实现"），原文记在各自的
提交信息与用例头注释里；本轮补跑的是 **③A（旗舰，缺陷已伤过人）** 与 **②（跨层不一致）** 两条。

---

## 4. 命令原始读数

环境：Windows 10/11 + PowerShell，`go version go1.25.8`，`cpu: 11th Gen Intel(R) Core(TM) i5-1155G7 @ 2.50GHz`，
仓库 `G:/Program/go/seelex`，`HEAD = 889527b`。

```text
=== ① gofmt -l（本波改动面）===
$ gofmt -l seelebridge/worktree seelebridge/tools seelebridge/workunit seelebridge/internal/telemetry e2e \
    seelebridge/resume_notes.go seelebridge/runtime.go seelebridge/runtime_subagent_resume.go \
    seelebridge/runtime_subagent_recovery.go seelebridge/runtime_teamwork.go seelebridge/workunit_assembly.go \
    seelebridge/workunit_team_records.go seelebridge/stage_preview_judgment_test.go
（无输出）
exit=0

=== ② go build ./... ===
（无输出）
build_exit=0

=== ③ go vet ./... ===
（无输出）
vet_exit=0

=== ④ go test ./... -count=1 ===
85 行读数：73 行 ok / 12 行 [no test files] / 0 行 FAIL
ok  	github.com/RedHuang-0622/seelex	43.082s
?   	.../application	[no test files]
ok  	.../application/approval	0.926s
ok  	.../application/console	0.346s
?   	.../application/contract	[no test files]
ok  	.../application/contract/dto	1.063s
ok  	.../application/core	28.662s
ok  	.../application/core/agentteam	1.566s
ok  	.../application/core/chat	1.001s
ok  	.../application/core/context_control	2.128s
ok  	.../application/core/context_runtime	1.978s
ok  	.../application/core/goal	7.143s
ok  	.../application/core/input_router	1.462s
?   	.../application/core/internal/limits	[no test files]
?   	.../application/core/internal/state	[no test files]
ok  	.../application/core/prompt_layer	1.433s
ok  	.../application/core/resume	1.282s
ok  	.../application/core/session_runtime	2.209s
ok  	.../application/core/subagent_view	0.284s
ok  	.../application/core/task_context	1.401s
?   	.../application/core/view_state	[no test files]
ok  	.../application/core/worktable	1.024s
ok  	.../application/event	1.136s
ok  	.../application/model	1.034s
ok  	.../application/prompt	1.184s
ok  	.../e2e	0.881s
ok  	.../e2e/scenario	0.358s
ok  	.../gui	3.941s
ok  	.../gui/terminal	2.532s
ok  	.../internal/adapters	1.015s
ok  	.../internal/bootseed	1.138s
?   	.../internal/buildinfo	[no test files]
ok  	.../internal/frontmatter	1.335s
ok  	.../internal/promptassets	1.426s
?   	.../internal/testutil	[no test files]
?   	.../internal/winhide	[no test files]
ok  	.../mcpstack	1.865s
ok  	.../mcpstack/config	1.290s
ok  	.../plugin	2.064s
ok  	.../seelebridge	55.843s
ok  	.../seelebridge/account	3.173s
ok  	.../seelebridge/attachment	1.349s
ok  	.../seelebridge/fork	0.274s
ok  	.../seelebridge/fs	1.150s
ok  	.../seelebridge/imageattach	0.287s
ok  	.../seelebridge/internal/actor	0.964s
ok  	.../seelebridge/internal/config	1.174s
ok  	.../seelebridge/internal/docker	5.001s
?   	.../seelebridge/internal/mapper	[no test files]
?   	.../seelebridge/internal/model	[no test files]
ok  	.../seelebridge/internal/stream	0.873s
ok  	.../seelebridge/internal/telemetry	1.131s
ok  	.../seelebridge/mcp	1.352s
ok  	.../seelebridge/multimodal	2.296s
ok  	.../seelebridge/node	0.275s
ok  	.../seelebridge/plan	0.378s
ok  	.../seelebridge/plugin	1.187s
ok  	.../seelebridge/scheduler	4.030s
ok  	.../seelebridge/search	4.236s
ok  	.../seelebridge/session	0.818s
ok  	.../seelebridge/task	1.803s
ok  	.../seelebridge/teamwork	1.608s
ok  	.../seelebridge/tools	20.280s
ok  	.../seelebridge/tools/computer	1.614s
ok  	.../seelebridge/tools/computer/mcp	1.579s
ok  	.../seelebridge/tools/websearch	3.283s
ok  	.../seelebridge/worktree	38.993s
ok  	.../seelebridge/workunit	1.740s
ok  	.../seelexctx	2.538s
ok  	.../seelexctx/compactor	1.487s
ok  	.../seelexctx/lifecycle	2.246s
ok  	.../seelexctx/memory	1.311s
ok  	.../seelexctx/merger	1.207s
ok  	.../seelexctx/provider	1.713s
ok  	.../seelexctx/search	2.183s
ok  	.../seelexctx/snapshot	1.479s
ok  	.../seelexctx/tokens	1.860s
ok  	.../session	1.839s
ok  	.../sessionstore	52.315s
ok  	.../skill	1.341s
?   	.../tmp/plugin-assembly-probe	[no test files]
ok  	.../tui	0.409s
?   	.../tui/splash	[no test files]
ok  	.../workspace	27.721s
test_exit=0

（完整 85 行结果 = 73 行 ok + 12 行 [no test files]；上面省略号处为 `github.com/RedHuang-0622/seelex/`）

=== ⑤ 机械门禁（③C/③E 的两条硬判据 + 阴性对照）===
=== RUN   TestWorkunitPortGate
--- PASS: TestWorkunitPortGate (0.00s)
=== RUN   TestWorkunitPortGateCatchesViolations
--- PASS: TestWorkunitPortGateCatchesViolations (0.01s)
PASS
ok  	github.com/RedHuang-0622/seelex/e2e	0.182s
gate_exit=0

=== ⑥ 工作区状态 ===
$ git status --porcelain
（无输出 = 干净）
```

> 旁注（与本轮无关、不需处置）：`go test ./...` 的包清单里出现 `.../tmp/plugin-assembly-probe [no test files]`
> （即 `<repo>/tmp/plugin-assembly-probe`）。`<repo>/tmp/` 是基线就存在的**本地草稿目录**，被 `.gitignore:85`
> 的 `tmp/` 规则整体忽略（里面还有 `build` / `guardtest` / `lock-contention-gate` 与一批历史 `*.txt` 扫描输出）；
> `plugin-assembly-probe` 是 2026-10-05 的一次性取证探针（HEAD 里只有前端测试注释提到它，
> `gui/frontend/dist/plugin-source.test.mjs:48`）。本轮未创建、未改动该目录（`git status` 为空即证）。

---

## 5. 「只剩一份实现」的读数（判据 = 只剩一份 + 编译器钉住）

### 5.1 唯一位置（现状行号）

```text
seelebridge/worktree/worktree_manager.go:929  func cleanupWorktreeWith(git gitFn, root string, wt *NodeWorktree) error
seelebridge/worktree/worktree_manager.go:787  func pathDirtyWith(git gitFn, path string) (bool, error)
seelebridge/worktree/worktree_manager.go:774  func PathDirty(path string) (bool, error)          ← 包级入口，转调
seelebridge/worktree/worktree_manager.go:736  func parseWorktreeList(out string) []worktreeEntry
seelebridge/worktree/worktree_manager.go:996  func worktreePathEqual(a, b string) bool
seelebridge/worktree/worktree_manager.go:383  const sceneNameInfix = "-seelex-"
seelebridge/worktree/worktree_manager.go:386  func sceneDirName(repoBase, nodeID string) string
seelebridge/worktree/worktree_manager.go:391  func sceneDirPrefix(repoBase string) string
seelebridge/tools/router.go:563               func newProcessTreeCommand(...) (*exec.Cmd, *security.ProcessTree)
seelebridge/tools/router.go:577               func startWithProcessTree(cmd *exec.Cmd, tree *security.ProcessTree) error
seelebridge/workunit/progress.go:53           const StagePreviewLimit = 240
seelebridge/workunit/progress.go:68           func EncodeStages(stages []Stage) []byte
seelebridge/workunit/progress.go:91           func DecodeStages(payload []byte) []Stage
seelebridge/workunit/progress.go:116          func ClipPreview(text string) string
seelebridge/workunit/progress.go:157          func ProgressOf(kind Kind, record sessionstore.NodeSessionRecord) Progress
seelebridge/workunit/progress.go:195          func NewUnitReader(...) *UnitReader   （Read:209 / Records:229 / Record:238 / List:255）
seelebridge/resume_notes.go:25               type resumeNotes struct            （set/take/peek/clear:31/49/65/80）
seelebridge/workunit/classify.go:28/33        ErrUncommittedChanges / ErrMergeBlockedByMain（Is*:37/40）
```

### 5.2 旧符号残留（`git grep -w`，判定「实现体真的没了」）

```text
clipTeamPreview → 0 命中 · stagePreviewMax → 仅 1 处注释（progress.go:113「此前四份」） · clipPreview → 0
teamUnitStages → 仅文档/注释 · teamUnitPreviewLimit → 0 · teamUnitProgressMap → 0
teamResumeState → 仅文档/注释 · buildStageLogs → 0
worktreeDirty → only 转调者 w.worktreeDirty:848 + 注释 + 文档（编排面那份已整函数删除）
recordFor / sessionRecords → 仅 progress.go:177 的历史注释（说明它们被谁取代）+ 无关的 application/core 归档命令
```

`status --porcelain` 的**判定调用**只剩 `worktree_manager.go:788` 一处（`:778` 是它自己的注释，`:780` 是
⚠ 唯一修复点标注）；其余命中全是 fake git 桩（`fake.reply["status --porcelain"]`）、用例断言与文档。

生产代码里 `security.NewProcessTree` / `ConfigureProcessTree` 只剩 `router.go:564/569` 一处；
`async_run.go` 里只剩注释（`:239/:241` 说明为什么两条链同一份装配）。

### 5.3 ③E 的零命中读数

```text
$ git grep -n -F 'seelebridge/worktree"' HEAD -- seelebridge/workunit
(0 命中)
$ git grep -n -F 'worktree.' HEAD -- seelebridge/workunit
seelebridge/workunit/contract.go:55     //    worktree.WorktreeManager.Prune）；…      ← 注释
seelebridge/workunit/session.go:17     // 3. 认领先于 Prune：… worktree.Prune …     ← 注释
seelebridge/workunit/README.md:76/84   （文档）
```

判据 E 成立且比原要求更强：**契约包里 `worktree.` 代码引用 0 处**，只剩 2 行注释 + 文档
（原先还允许 `classify.go` 一个例外，现在例外表也撤了，见 §2.6）。

### 5.4 编译器钉住（不靠"用例全绿"）

| 钉住什么 | 位置 |
|---|---|
| 作业面实现 = `jobs.Manager` | `workunit/contract.go` `var _ Jobs = (jobs.Manager)(nil)` |
| tools 自建表与作业面同形的那一格 | `tools/async_exec.go:197` `var _ workunit.JobSignals = (*asyncRegistry)(nil)` |
| 生命周期实现 = 契约那份 | `workunit_parent.go:51` `var _ workunit.Lifecycle = (*lifecycleHost)(nil)` |
| 装配处三格端口各有实现 | `workunit_assembly.go:106-108` |
| 跨入口只有一份判据（③A/B/D） | `worktree_*_judgment_test.go` 的「同一形状两条入口同一结论」 |
| 跨层只有一份折算/编解码（②） | `stage_preview_judgment_test.go` 的跨层一致性 |
| 记录词 = wire 词（③U6 部分） | `stage_preview_judgment_test.go:120` `TestSubagentStatusVocabularyAgreesWithTheWire` |

---

## 6. 两张作业表「一动没动」的证据

口径：`docs/arch/teamwork-leader-worker-architecture.md` **§12.4**（2026-10-01 裁决）已否决「迁 `jobs.Manager`」
——`event.Sink` 必须构造期定死 + 框架 `event.Recorder` 单例的全局序号与 Seelex 按会话追加的事件库不兼容。
**不并表、不迁 Seele、不反向合一**。本轮 ② 只并「判据 / 读法 / 容器」，不碰任何一张表。

```text
$ git log --oneline 49455b5..HEAD -- seelebridge/tools/async_exec.go seelebridge/jobs_events.go
（空 —— 本波 8 次提交，没有一次针对这两张表）
$ git diff --stat 49455b5..HEAD -- seelebridge/tools/async_exec.go seelebridge/jobs_events.go
（空 —— 内容逐字节未变）

$ git log -1 --format="%h %ad %s" --date=short -- seelebridge/tools/async_exec.go
a6f8f2b 2026-10-06 seelex/step-1: 作业端口扩成完整作业面（提交/读数/控制/信号四格+合成，编译期钉住）+ …
$ git log -1 --format="%h %ad %s" --date=short -- seelebridge/jobs_events.go
aa2fd26 2026-10-04 feat(teamwork): 阶段口径整条退场 + 自动返回/看板回执/这件事的会话三条链收口
```

两张表最后一次改动分别是 **步骤①（本波之前）** 与 **2026-10-04**；③C 只改了**启动装配**
（`async_run.go` 内联 → `router.go` 唯一实现），表本体零改动；§5.4 的两条编译期断言行仍在原处
（`tools/async_exec.go:197`、`workunit/contract.go`）——本轮 `tools/` 的 diff 只是 `async_run.go` / `router.go`。

---

## 7. 性能热点量级（收口后的唯一实现各一次多少时间 / 分配）

```text
=== workunit（go test -run TestNoSuchTest -bench Benchmark -benchmem -count=1）===
pkg: github.com/RedHuang-0622/seelex/seelebridge/workunit
BenchmarkClipPreview-8        	   68494	     18651 ns/op	    5648 B/op	       4 allocs/op   ← 输入 1000 rune
BenchmarkEncodeStages-8       	 2210329	       565.1 ns/op	     176 B/op	       5 allocs/op
BenchmarkDecodeStages-8       	 1219623	      1093 ns/op	     392 B/op	      10 allocs/op
BenchmarkProgressOf-8         	 1000000	      1101 ns/op	     392 B/op	      10 allocs/op
BenchmarkUnitReaderList100-8  	    9110	    139089 ns/op	   64095 B/op	    1004 allocs/op   ← ≈1.39 µs/条
ok  	.../seelebridge/workunit	8.751s

=== worktree（go test -run TestNoSuchTest -bench Benchmark -benchmem -count=1）===
pkg: github.com/RedHuang-0622/seelex/seelebridge/worktree
BenchmarkJudgmentPathDirtyWith-8        	 6759714	       207.5 ns/op	     153 B/op	       2 allocs/op
BenchmarkJudgmentWorktreePathEqual-8    	 3008283	       401.5 ns/op	     336 B/op	       7 allocs/op
BenchmarkJudgmentParseWorktreeList-8    	  681384	      1902 ns/op	    2144 B/op	       6 allocs/op   ← 16 条清单
BenchmarkJudgmentCleanupWorktreeWith-8  	    2847	    543099 ns/op	    1539 B/op	      15 allocs/op   ← 幂等路径，Windows FS 动作
ok  	.../seelebridge/worktree	7.827s
```

pprof top（`-benchtime 400ms -cpuprofile`）：

```text
workunit  Duration 2.72s / 2850ms samples：
  270ms  9.47%  runtime.decoderune
  150ms  5.26%  strings.FieldsFunc          （ClipPreview 的 fields 摊平）
  110ms  3.86%  encoding/json.checkValid
   90ms  3.16%  runtime.stdcall2
   90ms  3.16%  runtime.stringtoslicerune
worktree  Duration 2.53s / 3010ms samples：
  790ms 26.25%  runtime.cgocall           （真实 git 子进程/CreateProcess 边界）
  130ms  4.32%  runtime.procyield
  130ms  4.32%  runtime.stdcall3
  110ms  3.65%  runtime.typePointers.next
```

**量级判读**：`pathDirtyWith` 207 ns/op 是纯字符串判空 + 一次注入式 git 调用，处在「纳秒级判据」档；
被 ④ 收掉的 `UnitReader.List`（100 条）1.39 µs/条，相对它读账本（JSON 解码）的代价可忽略；
`cleanupWorktreeWith` 543 µs/次全部落在 Windows 文件系统与 `git worktree remove` 的 `cgocall` 上，
与收口前同一量级（收口前是两条各自实现，基准读数只存在于读数里，本波**只加了基准、没有优化**）。
`ClipPreview` 18.7 µs（1000 rune）由 `runes` 转换 + `FieldsFunc` 主导，是全表最贵的一项，
但它一次收尾只调一次、且替代的是**四份**各自裁一遍的实现。

---

## 8. 冒烟（逐入口）

| 入口 | 操作 | 读数 |
|---|---|---|
| CLI 启动 | `dist/dev/seelex.exe -version` | `dev`，`smoke_exit=0` |
| 同步工具链（③C 的一条链） | 本会话数十次 `bash`（`go build`/`go vet`/`go test`/`gofmt`/`git …`） | 全部正常返回；`build/vet` exit 0、`test` exit 0 |
| 后台工具链（③C 的另一条链） | 本会话 2 次 `bash_bg`（句柄 `a50`/`a51`）+ 增量 fetch | `a50`：build/vet/test 三段全绿、`test_exit=0`；`a51`：全量 `go test ./...` `test_exit=0` |
| GUI 包入口/桥接 | `go test ./...` 覆盖 `gui` / `gui/terminal` | `gui 3.941s ok` / `gui/terminal 2.532s ok`（全量读数内） |
| 前端渲染面 | `node --test`（`gui/frontend/dist`，65 个 `.test.mjs`） | `tests 651 / pass 651 / fail 0`，`node_exit=0` |
| 桌面（只读检查） | `computer_windows` | 顶层窗口 5 个；前台是用户自己的 `Seelex` 窗口（可见、未最小化）。**按纪律未做任何焦点/输入操作** → GUI 手工点按列为**未覆盖项** |

---

## 9. 未决项逐条

### 9.1 ② 的 U（读面勘定 §4）

| 项 | 结论 | 证据 |
|---|---|---|
| **U2**｜`Progress.StartedAt/EndedAt` 对 teammate 恒零值会让读面「缺一格」吗？ | **仍开放**（本轮未接线消费方）。落地事实：`ProgressOf` 不编造记录里没有的事实，teammate 记录的这两个字段就是零值；时间的事实源仍是计划的 `item.StartedAt/FinishedAt`，看板消费方要从**计划**取、不从 `Progress` 取 | `workunit/progress.go:157`（`ProgressOf` 逐一对应记录已有字段）；`workunit/progress_test.go` 的 teammate 用例 |
| **U3**｜写侧兜底转调 `ProgressOf` 会不会改落盘语义？ | **已可判定为"不改"**：`ProgressOf` 是**读**侧折算，写侧兜底（`running`/`queued`）留在写方（`subagent_sessions.go:515` 的 `else if _, running := s.sessions[nodeID]`），两者没有并成一个分支 | `workunit/progress.go:139` 注释明写「`Status` 是记录里的**原词**，终态由写方按自己的语义定名」；`workunit/session.go:57-61` 明写「终态词表**不进契约**」；护栏：`progress_test.go:105`「记录里的原词必须原样交回」、`:125`「空记录折成零值读数（不编造）」、`:196` `TestUnitReaderCannotBranchOnKind`（读面不据 `Kind` 分支），以及 `seelebridge/session` 四个用例全绿（`TestSubagentSessionsLifecyclePersistConclusionDelete` / `RestoreRebuildsDetail` / `PersistFailedOutcome` / `TestSubagentStageLogsAndSemanticResult`）。勘定里提议的「空 `Status` 的旧记录读回不额外改写」这条**没有单独写**——`TestUnitReaderCannotBranchOnKind` 与空记录零值那条合起来已覆盖同一读数 |
| **U4**｜`teamUnitStages` 单元素 → 时间线，会改回灌文案吗？ | **本轮不做扩展**（只收编解码，不改时间线粒度）：teammate 打点仍恒单元素，`EncodeStages` 收的是「同一串载荷的编成一件事」，不是「累积多个打点」。因此 `RecoveryNote` 的「已到阶段（打点）」一行文案不变 | `workunit_team_records.go:85`（每次仍编一个 `Stage`）；护栏：`workunit_team_test.go:425`（`len(record.StagesJSON) > 0`）与 `TestTeamUnitRecoverMarksInterruptedAndInjectsTheNoteOnce` |
| **U5**｜`UnitReader.List()` 该不该按 `Kind` 过滤？ | **已回答**：不过滤，读面返回**带 `Kind` 的清单**，归属过滤由调用方给（`NewUnitReader` 的 `owned` 参数；nil = 这张账本只装这一层）。不把「猜归属」塞进读面 | `workunit/progress.go:195`（`owned` 入参）；三个生产装配点各自给归属：`workunit_assembly.go:387`（nil，本层专账本）、`runtime_subagent_resume.go:302`（nil）、`workunit_team_records.go:338`（teammate 名单过滤） |
| **U6**｜统一实时事件（第二批）的粒度上限 | **仍开放**（进第二批）：`assistant` 正文增量只有子代理侧有，teammate 侧（`role_tool_activity.go`）只做工具。先写「事件载荷字段表」（每字段两层是否都有值）再定 schema | `docs/arch/workunit-progress-read-surface.md:307`（U6 原文） |

### 9.2 ③ 的 U（任务书 §7 H）

| 项 | 结论 | 证据 |
|---|---|---|
| **U2**｜teammate 现场是否会被重复登记？ | **不会**（已核实）。`beginNamed` 首判「同一 nodeID 已有在册现场 → 直接返回既有的，绝不重建」；注册表以 `nodeID` 为键（`w.worktrees[nodeID] = wt`）。命名只按 nodeID（`<repo>-seelex-<nodeID>` / `seelex/<nodeID>`），跨会话、跨批次同名会指向**同一个目录**——所以重建前那句 `worktree remove --force` 会把正在用的现场删掉，这正是幂等首判要挡的 | `worktree_manager.go:287`（`if existing := w.worktreeFor(nodeID); existing != nil { return existing }`）、`:242`（注册表读）、`:403`（注册表写）、`:354`（`Adopt` 侧同判） |
| **U5**｜进程树退化路径两条链对外主张是否一致？ | **装配口径一致，对外读数不一致**（如实报，未顺手改）。装配侧：两条链共用 `startWithProcessTree`，`Attach` 失败都**不放弃执行**（派发已发生），`tree.Degraded()` 说得出差别。读数侧：后台链有探针（`async_probe.go` 的退化读数），同步链**没有任何退化读数**——即同步链"只杀直接子进程"这件事对调用方不可见 | 转调点：`async_run.go:256`、`router.go:603`、`router.go:646`（docker 重试）；`router.go:575-576` 注释「挂不上不放弃执行…差别由 `tree.Degraded()` 说得出」。**仍开放**：同步链的退化读数要不要补（是真缺口，但会改对外读数，不在"收重复"范围内） |
| **U6**｜「在跑」状态字面量残留点 | **部分回答**（本波修掉本包最后一处，其余标仍开放）。已收：`runtime_subagent_recovery.go:97` 的 `record.Status == "failed"` 改用 `subagentNodeStatusFailed`，并新增 `TestSubagentStatusVocabularyAgreesWithTheWire` 把「记录词表 ↔ wire 词表（`dto.SubAgent{Queued,Running,Done,Failed}`）」逐条互锁。**仍开放**：跨包的两批字面量——**写**点 `node/coordinator.go:132`（`status := "done"`）/`:135`（`status = "failed"`）、`node/agent_node.go:305`（`return "failed"`）、`session/subagent_sessions.go:208`（`subagentOutcome{status: "done"}`）；**读**点 `session/subagent_tree.go:422`（`case status == "done"`）/`:424`（`"failed"`）。契约侧的 `StatusQueued`/`StatusRunning`（`workunit/session.go:63/64`）是有意保留的唯一词表 | 见上锚点；`subagentNodeStatusDone/Failed` 定义在 `runtime_subagent_resume.go:353/354` |

### 9.3 其余残留（记录、不顺手改）

1. **`worktree/README.md:97` 的「已知风险」段**仍写着旧函数名 `worktreeDirty`——它是**文档**，不改代码；
   但该段与 §2.2 的唯一修复点（`worktree_manager.go:788`）应对齐，属文档债。
2. **CRLF 幻影脏本身没修**：判据只收到一处（`:788`），语义原样（`status --porcelain` 判非空）。
   改成 CRLF 不敏感判定是一次**语义变更**，需要自己的红灯用例——本轮明确不做，理由写在 `:780-786` 注释里。
3. **同步链无退化读数**（见 U5）。
4. `<repo>/tmp/plugin-assembly-probe` 在 `go test ./...` 包清单里的残留（§4 旁注）。

---

## 10. 未覆盖项与残留风险

| 项 | 说明 |
|---|---|
| GUI 手工点按 | 桌面只读检查发现前台是用户自己的 `Seelex` 窗口，按纪律**没有抢焦点/合成输入**，因此「逐入口手点一遍」这一层由 e2e/gui 自动化用例（全绿）与 `-version` 冒烟替代，**未做人手点按** |
| ③B/③C/③D/③E 的红灯 | 只有 ③A 与 ② 两条做了"换回旧文件复跑"的真跑复现；其余四条的红灯形状记在提交信息与用例头注释（§3.3 已逐条指位置），本轮未补复跑 |
| 真机团队跑一轮 | 本会话 teamwork 已被判定不可用（`goal` 看板 finding @1791228046），整队 close、改 leader 串行；因此 `cleanupWorktreeWith` / `pathDirtyWith` 等**只在用例与工具链冒烟里跑过，没有在真实团队派活-收尾链路上跑过** |
| `UnitReader` 的消费方（看板/详情 UI） | ② 只接线了三条**读路**（宿主记录读面 / 子代理恢复定位 / teammate 会话级读回）；前端把 `Progress` 渲染出来（U2 的时间字段）不在本轮 |

---

## leader 复核（2026-10-06，verdict：**PASS**）

按 `step-3-goal.md §7` 的 A–H 与 `step-2-handoff-prompt.md` 的 A–E 对**源码**逐条复核（不引用本文件自述）：

| 判据 | 结论 | 复核锚点 |
|---|---|---|
| ② A 折算/编解码有**生产调用点** | ✔ | `workunit_assembly.go:307/387`、`runtime_subagent_resume.go:302/315/546`、`workunit_team_records.go:85/249/338` |
| ② B 三处旧体删除或转调 | ✔ | §2.5（`recordFor`/`sessionRecords`/`teamUnitStages`/`clipTeamPreview`/匿名解码 全部 `git grep -w` 零命中） |
| ② C 恢复说明容器只剩一处 | ✔ | `resume_notes.go:25`；`runtime.go:231`、`runtime_subagent_resume.go:52` 都换成同一类型 |
| ② D 既有用例一条不改 | ✔ | 除 `workunit/progress_test.go`（只加文件头注释 + 追加一条 `Record` 用例）与 `workunit/contract_test.go`（③E 的机械适配，已声明），无既有用例被改 |
| ② E 未并表 | ✔ | §6（两张表区间零 diff + 断言行仍在） |
| ③ A 一份幂等实现 + 红灯 | ✔ | §2.1 + §3.1（本条红灯是本轮真跑复现的） |
| ③ B 3→1 + CRLF 修复点指到行 | ✔ | §2.2 + `worktree_manager.go:788` |
| ③ C 2→1 + 为什么不并超时 | ✔ | §2.4 + `router.go:557-562` |
| ③ D 2→1 + 保留防幽灵语义 | ✔ | §2.3 + `worktree_manager.go:645-653`（`worktreePathEqual`）与 `Restore` 的 `os.Stat` 判据保留 |
| ③ E 契约不 import worktree + 撤例外 | ✔ | §5.3（`grep 'seelebridge/worktree"' seelebridge/workunit` → 0 命中）+ 门禁 `e2e/workunit_ports_test.go` 零命中判据与阴性对照 |
| ③ F 原始读数 | ✔ | §4（gofmt / build / vet / `go test ./...` 85 行 / 门禁） |
| ③ G 删除清单 | ✔ | §2（逐条：旧位置 → 去向 → 新唯一位置） |
| ③ H U2/U5/U6 | ✔ | §9.2（U2 回答、U5 回答并标出真缺口、U6 部分回答 + 残留点清单） |
| 基类体检：`workunit_parent.go` 本轮零改动 | ✔ | `git diff --stat 49455b5..HEAD -- seelebridge/workunit_parent.go` 为空 |

**一处与目标书写法的偏差（明说，不静默）**：`step-3-goal.md` §7 的「基类体检」原文是
「`workunit_parent.go` / `workunit_assembly.go` 本轮零改动（`git diff --stat` 空）」。实际落地：
`workunit_parent.go` **零改动**（`git diff --stat` 为空），而 **`workunit_assembly.go` 被改了 72 行**。
理由是 ② 的「记录 → 进度」收口必须落在宿主端口那一格——`hostPorts` 的 `Status`/`Readout`/`Clear`
原来在那里自己列账本 + 线性查找（`recordFor`/`sessionRecords`，§2.5），而那一格就住在
`workunit_assembly.go`；右腿又在同一文件里把 `Readout` 改成一次账本读（§2.7）。
两处改动都是**转调**（折算交给 `ProgressOf`、定位交给 `UnitReader`），`Lifecycle` 面与
`lifecycleHost` 的签名一个字节未动。该条验收应读作：**"基类（生命周期实现）不被撑大"成立；
`workunit_assembly.go` 作为装配处按设计被②接管**。

**两处必须写清的口径（不许被"合并"抹掉）**：

1. **幂等只覆盖「释放动作」**，不覆盖「收尾段拿不到现场」——旧用例
   `worktree/worktree_vanished_scene_repro_test.go` 钉住「现场被对端收走时 `Finish` 必须报错」，它与 ③A 的
   幂等口径并存（全量读数里 `worktree 38.993s ok` 即包含这两条同时在）。
2. **超时策略有意不并**（后台 `WithTimeout(WithoutCancel(ctx), asyncHardCap)` vs 同步
   `WithTimeout(ctx, scopedToolTimeout)`）——§2.4 给了理由与锚点。
