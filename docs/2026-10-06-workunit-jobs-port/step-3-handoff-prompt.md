# 交接提示词：步骤③ —— 现场 / 进程树层的重复收口（+ 两个红灯先行的缺陷）

一次性工作包。用法：把下面「提示词正文」整段贴给下一个会话。

## 它是怎么定出来的（不是新发明的计划）

- **①** = 只定接口（作业面四格 + 装配处三格 + 读面定形 + 机械门禁）→ 已落地并被 leader 复核 PASS（`a6f8f2b`）。
- **②** = 并"判据 / 读法 / 容器"（折算三份、阶段编解码两份、恢复说明容器两份、teammate 打点恒单元素）→ 提示词见 `step-2-handoff-prompt.md`。
- **③ = `docs/arch/workunit-duplication-inventory.md` §二「归并建议清单（按收益排序）」里剩下那批低-中代价项**：
  #2 现场清理幂等、#3 脏判定三份、#5 进程树装配序列、#9 在册路径比较口径 + §10.2 红灯先行 + 待决 `classify.go` 哨兵。
- **不入 ③ 的**：#1 恢复名单登记（与 ② 交叠，且涉及"认领先于 Prune"这一条硬顺序）、
  #7 teammate 恢复续跑（触及 leader 闸门，单独一波）、**#8 作业面合表（§12.4 已裁决关闭）**。
- ⚠ 清单是**更早的快照**（写作时 main 头在 `746b00e` 一带），**行号可能已漂移：动手前逐条复核锚点**。

## 现状与目标（字符画 + 大白话）

### 现状

```
③ 现状：同一动作 / 判据在现场与进程树层各有 2–3 份，而且"写法还不一样"

  A. 现场清理（删目录 + 删分支）── 2 份，语义不同
       worktree_manager.go:779  cleanup          非幂等（删不到就报错）
       worktree_manager.go:822  CleanupWorktree  幂等（不在册 = 已释放）
       两处都走到 → 已知缺陷 A：exit status 128 / not a working tree

  B. 「工作区是否脏」判定 ── 3 份
       :749 worktreeDirty(wt) ／ :688 pathDirty(path) ／ runtime_teamwork.go:861 worktreeDirty(root)
       三处都跑裸 git status --porcelain → CRLF 幻影脏有三处暴露面（改一处漏两处）

  C. 进程树装配序列 ── 2 份
       tools/async_run.go:244  startAsync（后台：WithoutCancel + 30min HardCap）
       tools/router.go:554     newScopedCommand（同步：scopedToolTimeout）
       同一套 NewProcessTree → ConfigureProcessTree → cmd.Cancel → Attach 写了两遍

  D. 「现场是否在册」路径比较 ── 2 份口径
       :538 Restore 逐字符 os.Stat ／ :577 sceneRegistered 走 worktreePathEqual 规范化
       命名也两处拼：:371 scenePath ／ :682 isManagedPath 前缀判定

  E. 待决：workunit/classify.go 依赖 worktree 两个哨兵错误（机械门禁里唯一的例外项）

③ 目标：每个动作 / 判据只剩一份；两个已经伤过人的缺陷红灯先行

   现场清理：一份幂等实现（另一处变薄包装或删除）  ← 红灯：连续两次清理不许报错
   脏判定  ：一份 pathDirty                        ← CRLF 幻影脏的修复点只剩一处
   进程树  ：一份 newProcessTreeCommand            ← 只收"装配"，超时策略各留各的（不并）
   在册判定：一份 worktreePathEqual + 一个命名常量 ← 保留"目录不存在不登记"的防幽灵语义
   classify：哨兵搬出 worktree → 契约包不再 import worktree，门禁例外项撤掉

   判据不是"用例全绿"，而是**只剩一份实现的删除清单**（AGENTS.md §8）
```

### 大白话

**现状**：这一层的问题不是"数据存了两份"，而是**同一个动作在两处各写一遍、还写得不一样**——
删现场有两个函数（一个"删不到就报错"，一个"不在就是已释放"），两个都跑到就报
`exit status 128 / not a working tree`；判"工作区脏"有三段几乎一样的 `git status`；
起进程树的四步序列写了两遍；判"这个现场在不在册"还有两种比较口径（逐字符 vs 规范化）。
这就是"漂移温床"：修一处、忘两处。

**目标**：**每个动作、每个判据只剩一处**，并且两个已经真的伤过人的缺陷（清理不幂等、在册比较口径）
**先写红灯再修**（先复现、再改）。验收不看"用例全绿"——用例全绿证明不了"没有第二份"——
看的是**删除清单：旧的那份去哪了**。

## 提示词正文

```
【任务】步骤③：把现场 / 进程树层的重复动作与判据收成一份（含两个红灯先行的缺陷）——**只并本质重复**。

==================== 一、前置（新会话必须知道的事实与环境） ====================
仓库：G:/Program/go/seelex（Windows + PowerShell；go 可用）。
基线：main 头 = 本文件所在提交（含 ① 落地 a6f8f2b、② 口径纠正 2323874）；开工前 `git log --oneline -10` 核对，工作区干净。
上游依赖：②（并判据/读法/容器）的成果**若已落地**，先读它的交付记录；**若还没落地**，本轮请从
  C / D / E（工具链与 worktree 内部，与 ② 不交叠）开始，A / B 也在 worktree_manager.go（同样不交叠），
  只有 #1（恢复名单登记，本轮**不做**）与 ② 交叠。

必须先接受的口径（AGENTS.md §8「复用与单一实现纪律」逐条适用）：
  - 一份判据 = 一处函数：同一判据、同一语义出现第二处，即视为缺陷（不是"更清楚"）。
  - **只并本质重复**：判据相同 + 语义相同 + 会一起漂移并伤到人 → 合并；只是长得像但语义不同的
    （例：两处都起进程树，但超时/取消语义不同）**不合并**，但要写清"为什么像却不并"。
  - **交付必须列"删掉了哪些重复实现"**；只加不改的交付一律退回。
  - 证据是"只剩一份实现"，不是"用例全绿"。
  - 不许自造形状：与文档冲突 → 先回来改文档，不许在实现里私下另立一套。

⚠ 任务书 `docs/arch/workunit-duplication-inventory.md` 是**更早的快照**（写作时 main 头在 746b00e 一带）：
  里面的 `文件:行` 锚点**先复核再动**，行号漂移不要硬套；发现锚点与现状不符时，先改清单并说明理由。

纪律：§0 危险操作铁律（先读 MEMORY.md、先中文预警、先确认）；不得读取或提交 config/accounts.yaml 与 *.local.yaml；
  桌面纪律：不置顶/不覆盖窗口、不抢前台、不合成键鼠；作业输出一次性取全（销项后日志会被删）；
  看板不可信（todo_init 是「并入」语义，表里有历史重复行与一个 interrupted 旧行）。

==================== 二、先读哪些文件（唯一口径，按序） ====================
1. docs/arch/workunit-duplication-inventory.md —— **本轮任务书**：§一 的 1) / 3) / 4)（现状几份 + 锚点）、
   §二 归并建议清单（按收益排序，#2/#3/#5/#9 就是本轮的活）、§四 不确定项（U2/U5/U6 要正面回答）
2. docs/arch/workunit-single-lifecycle-one-implementation.md —— §5「真的只有一份实现的证据（不是用例全绿）」
   四条、§7 工程纪律（派活时逐条带上）、§10「红灯先行的两处已知缺陷」
3. AGENTS.md §8（复用与单一实现纪律）+ §0（危险操作）+ §5（验证命令）
4. 代码现场（只读，先别改）：seelebridge/worktree/worktree_manager.go（cleanup / CleanupWorktree /
   pathDirty / worktreeDirty / Restore / sceneRegistered / scenePath / isManagedPath）、
   seelebridge/runtime_teamwork.go:861、seelebridge/tools/async_run.go:244、seelebridge/tools/router.go:554、
   seelebridge/security/process_tree_windows.go、seelebridge/workunit/classify.go、e2e/workunit_ports_test.go（例外项）
5. 缺陷 A 的原始记载（`exit status 128 / not a working tree`）与 `worktree_merge_kickback` 那波的红灯用例
   —— 先找到既有记录与既有红灯，别自己另起一套复核方式

==================== 三、做到什么程度算完成（判据 + 交付格式） ====================
做这些（清单 §二 里低-中代价的那批）：
  A. 现场清理（删目录 + 删分支）并成**一份幂等实现**（另一处变薄包装或删除）。
  B. 「工作区是否脏」判定 3 份 → 1 份（`pathDirty` 类唯一实现），其余转调。
  C. 进程树装配序列 2 份 → 1 份（`newProcessTreeCommand(...)` 类助手）；**只收树的装配**。
  D. 「现场是否在册」路径比较 2 份口径 → 1 份（`worktreePathEqual` + 命名常量）。
  E. 待决项：`workunit/classify.go` 依赖 worktree 两个哨兵错误 → 把哨兵搬出 worktree 并**撤掉门禁例外项**
     （或写清为什么这轮不搬并给判据）。

判定标准（缺一条不算完成）：
 A. 现场清理：只有一份实现（给出唯一位置 + 旧位置去向）；**红灯用例在仓**——连续两次清理不报错、
    且"目录 / 分支 / 登记任一不在 = 已释放"，并给**先红后绿**的红灯原文。
 B. 脏判定：给出 3 → 1 的删除清单；写清 CRLF 幻影脏的**唯一修复点**在哪一行。
 C. 进程树：给出 2 → 1 的删除清单；并写清**为什么不把超时策略也并进去**
    （后台 `WithoutCancel` + 30min HardCap vs 同步 `scopedToolTimeout`）。
 D. 在册判定：给出 2 → 1 的口径统一；**保留"目录不存在不登记"的防幽灵语义**（清单 §二.9 的约束）。
 E. `grep -rn "worktree\." seelebridge/workunit/` 只剩契约自己声明的记录形状与（若保留）已登记的例外；
    门禁 `e2e/workunit_ports_test.go` 的例外项同步更新或撤掉。
 F. 原始读数：gofmt -l（改动文件）/ go build ./... / go vet ./seelebridge/... / go test ./e2e/ -count=1 /
    go test ./seelebridge/worktree/ ./seelebridge/tools/ ./seelebridge/teamwork/ ./seelebridge/... -count=1。
 G. **删除清单**（文件:行 → 新唯一位置，逐条）——只加不改的交付一律退回。
 H. 未决项：清单 §四 的 U2（teammate 现场是否被重复登记）/ U5（进程树退化路径两条链对外主张是否一致）/
    U6（在跑状态字面量残留点）逐条回答或标记"仍开放"。

非目标（做了即越界）：不做 #1 恢复名单登记（与 ② 交叠，且"认领先于 Prune"是硬顺序）；
  不做 #7 teammate 恢复续跑（要过 leader 闸门/屏障/在编校验，单独一波）；
  **不做 #8 作业面合表**（§12.4 已裁决关闭）；不碰前端读面渲染；不改 Seele 的 jobs 包。

红线（碰到就停手报告，不要硬做）：
 - 若合并会改变对外语义（例如把幂等口径强加给"必须报错"的那条路径）——**先写红灯把语义固定下来**，
   再决定并法；不许"顺手统一"。
 - 若发现任务书锚点与仓库现状冲突——先改清单并说明理由，不许另立一套实现。
 - 若某一条必须改 `worktree` 的公共 API 才能并——停手报告（契约依赖例外单独裁决，别在实现里悄悄改 API）。

【交付格式】1) 改动文件清单；2) **删除清单**（文件:行 → 新唯一位置；旧位置的去向：删除 / 薄包装）；
   3) 每条的红→绿原文（A 必须有）；4) 命令原始读数；5) 「只剩一份实现」的证据（每条：唯一位置 + grep 读数）；
   6) 未决项（U2/U5/U6 逐条）。
```
