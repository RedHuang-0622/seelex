# 作业身份层命名契约（D1 的解法）

状态：**已落地**（2026-10-15 裁决：§7 取"改 Seelex 侧工具名 + 走 Seele 一次小版本"这一档；
Seele 与 Seelex 两侧代码、门禁与文档同步改完；落地结果与两处**与本文原方案不同的判断**见 §8）
范围：D1 为主，D2/D5 与之强耦合，D6–D8 顺带定序；D3/D4 独立成项
输入：用户口径（2026-10-15）——「给 teamwork 和 jobs_manage 做出区分度更大的命名，并给任务命名
定规范：后台 tool 任务 `bt-x`、后台 subagent 任务 `bs-x`、后台 teamwork 任务 `bteam-x`」

---

## 0. 结论

方向对，且是 D1 唯一可能真正修好的那条路：**名字只差一个字母的缺陷，不可能靠措辞修**。

但"前缀命名"单独一条不够，它只堵住"静默命中"。完整的判据是三件套，缺一不可：

1. **句柄自证来源**（前缀命名空间）——两张表的句柄集合从此互不相交，"跨表撞车"在语法上不可能；
2. **跨表调用显式报错并点名对偶工具**——模型拿错工具时，得到的是"该用 X"而不是一次成功的错操作；
3. **管理工具名不可混**——让它一开始就不容易认错（`job_manage` / `jobs_manage` 这类名称本身是缺陷）。

下面 §1 是契约，§2 是工具名，§3 是唯一跨仓项（`bteam-` 必须由 Seele 铸造），§4 是与其他缺陷的先后，
§5 是先红后绿的用例，§6 是逐行改动清单，§7 是待裁决。

---

## 1. 句柄命名空间契约（唯一事实源）

| 来源（派发面） | 谁铸造句柄 | 形态 | 管理工具 |
|---|---|---|---|
| 后台**工具**作业：`bash_bg`（kind=process）、`read_batch`（kind=inline） | Seelex async 登记表 `seelebridge/tools/async_exec.go:314` | **`bt-<n>`** | `bg_manage`（§2 改名） |
| 后台**子代理**作业：`fork_subagents`（kind=subagent） | 同上（同一张表、同一状态机） | **`bs-<n>`** | `bg_manage` |
| **团队**作业：`team_dispatch` → teammate（`jobs.Manager`，kind=worker） | **Seele** `jobs.Manager`（`jobs/manager.go:267`） | **`bteam-<n>`**（现状 `a<n>`，见 §3） | `jobs_manage` |

### 1.1 文法与匹配（这里有真正的坑）

- 句柄文法 = `<token>-<seq>`；token 集合 = `bt` / `bs` / `bteam`，**分隔符是文法的一部分**。
- 匹配**必须带分隔符**：`HasPrefix(h, token+"-")`。裸 token 前缀匹配会把 `bteam-3` 判成 `bt-`——
  `bt` 是 `bteam` 的前缀。这条坑不写下来，下一个人一定会踩。
- `b` = background，三个 token 同源；这也是它们短、好念、好记的原因。

### 1.2 一份判据 = 一处函数

新增**唯一**判据（零依赖契约层，`application/contract/dto`；`seelebridge/tools` 与 `application/core`
都已依赖它）：

```go
// dto：token 表 + 两个纯函数（铸造、识别），调用方只许调这里
JobNamespaceToken(ns JobNamespace) string        // bt / bs / bteam
JobNamespaceOf(handle string) (JobNamespace, bool) // 识别；带分隔符匹配
```

- 铸造点（`async_exec.go`）、跨表守卫（`job_tools.go`）、模型面文案（`work_table.go`、`async_completion.go`）、
  用例——全部调这两个函数，不许各自写字面量。
- **反例就在仓库里**（提醒不要重复）：工作表格行前缀 `async:` 现在有两份字面量副本——
  `application/core/work_table_async.go:24` 与 `seelebridge/tools/job_contract.go:454`，靠注释"同字面量"维持一致。
  这次不许再长一份。

### 1.3 序号与行 ID

- 序号沿用**各表自己的全局 seq**：`bt-1, bs-2, bt-3`。跨 token 编号不连续是**诚实的**（它本来就是派发序），
  且不需要为此新增第二张计数器表。若要求"每 token 从 1 连续"，代价是每 token 一个计数器——不建议。
- 工作表格行 ID **不变**：仍是 `async:<handle>`（→ `async:bt-3`）。`async:` 是**行命名空间**，
  与句柄命名空间正交，本次不动（前端、看板、reducer 因此零改动）。

### 1.4 跨表行为（修复的落点）

| 调用 | 今天（`job_manage` / `jobs_manage`） | 契约后 |
|---|---|---|
| `bg_manage(op=observe, handle="bteam-3")` | 该形态尚不存在 | 显式报错，文本点名 `jobs_manage`（团队作业面） |
| `jobs_manage(op=done, handle="bt-3")` | 该形态尚不存在 | Seele 报"未知句柄"（它只认自己表里的句柄）：**响亮失败，不再静默命中** |
| `job_manage(op=done, handle="a5")` 打到**无关后台作业**（审计实测） | 静默销项，返回成功 | **不可能发生**：`a5` 不再是后台表的形态 |

第三行就是 D1 里会"静默损坏数据"的那一条；它在 §1 落地当天消失（不依赖 §3 的 Seele 版本）。

---

## 2. 工具名：`job_manage` → `bg_manage`

**改 Seelex 这一侧，不改 `jobs_manage`**，理由是分层：

- `jobs_manage` 是**框架 builtin**（`jobs/builtin/manage.go` 的 `ManageToolName`，provider `seele_jobs`），
  它的定位是**产品中立**的通用管理工具；"这是团队作业面"是 **Seelex 的语义**，签名该由 Seelex 自己扛。
- 因而 Seelex 侧改名 = 零跨仓成本、零版本成本；改 Seele 侧 = 一次发版 + Seelex 全仓文档跟着动。

命名取 `bg_manage`：与 `bt-`/`bs-` 同源（b = background），与 `jobs_manage` **无公共前缀、不同词数、不同长度**，
也不再与它共享 `job[s]_manage` 这段骨架。

备选（若你要对称命名）：`local_jobs` / `bg_jobs`；更强的一档是让 Seele 支持 `WithToolName`，由 Seelex 注入
team 味更足的名字（如 `team_jobs_manage`）——**可选，不做也已达到区分度**。

### 2.1 为什么不合并两张表（审计 D1 的备选 2）

审计把"两张表合到一个工具、按句柄前缀路由"列为备选。我建议**不选**，三个理由：

1. 合并只能落在 Seelex 侧（`jobs_manage` 是 Seele builtin，Seelex 要自己再实现一份管理工具），
   那就是"同一件事的第二份实现"（AGENTS §8），并且要主动遮蔽框架 builtin；
2. 两张表**不是同一件事的两份实现**，而是两种作用域模型：`jobs.Scope{Session, Subject}`（框架，
   按会话+主体授权、可带 worktree 回收钩子）vs tools 登记表的 `SessionID`（产品，按会话）。
   合并会把"授权与回收语义不同"这条边界藏进一个按字符串前缀分派的分支里；
3. `docs/2026-10-06-workunit-jobs-port/` 已就"两张表一张都不搬"做过一轮裁决。本次只修**身份层**，
   不动这两条已经站得住的边界。

### 2.2 迁移门禁（复用现成纪律）

改名按仓库既有口径走：把 `job_manage` 加进 `job_migration_residue_test.go` 的 retired 名单，
要求**代码（`*.go`）+ `config/*.yaml` 零残留**，`docs/` 与 `CHANGELOG.md` 作为历史记录豁免。

一处需要写明（实测确认）：`job_manage` 与 `jobs_manage` **互不为子串**（`jobs_manage` 中并不存在
`job_manage` 这串连续字符，反向亦然），所以现成的 `strings.Contains` 判定可以直接用，不需要词边界改造；
要做的是在门禁注释里点明"退役的是 `job_manage`，`jobs_manage` 仍在服役"，防止后人误把两者一起下掉。

---

## 3. `bteam-` 需要 Seele 一次小版本（唯一跨仓项）

### 3.1 为什么不能只改 Seelex

Seele 的句柄由 manager 自己铸造，且**明确拒绝调用方给的值**：`jobs/job.go` 的 `Spec.Handle` 注释写着
"A caller-supplied value is overwritten: the handle is proof of registration, not an input"，
铸造点固定在 `jobs/manager.go:267`。所以 `bteam-` 在 Seelex 侧改不出来。

**不采用**"Seelex 侧把 `a5` 展示成 `bteam-5`、回调用时再翻回去"的别名方案：别名要求
"每一处出口都翻得一致"，而 `jobs_manage` 认的是 Seele 的原文，回执 / `team_items` / 看板 /
提示词 / 错误文案里任何一处漏翻，就退回今天这个"同一个作业两张脸"——**那正是 D1 本身**。

### 3.2 建议的上游改动（加法、不改默认 → 一项小版本）

1. **`jobs.WithHandlePrefix("bteam")`**（manager 选项）。默认仍是 `a`，Seele 自己的用例与文档一字不动；
   Seelex 在装配处传 `dto.JobNamespaceToken(dto.JobNamespaceTeamwork)`，保持"命名表只有一份"。
2. **`jobs_manage` 收 `handles`（一批一次等/一次读）**。现在它是严格解码（`DisallowUnknownFields`），
   于是 `bg_manage` 收 `handles` 而它不收——审计 D1 第 3 条要求的 "schema 对称"就卡在这里。
   这是加法，且 Seelex 侧 `FetchMany`/`StatusMany` 的形状可以直接对照。
3. （可选）`WithToolName(...)`：让产品自己命名管理工具，于是 §2 的命名权完整的留在 Seelex。

### 3.3 过渡期（Seele 未发版前）

teammate 句柄仍是 `a<n>`。守卫把"**非 `bt-`/`bs-`**"一律判为**团队作业面**并点名 `jobs_manage`：
线索从"自证"降级为"排除法"，但**静默损坏那一类伤害已经不在了**。`bteam-` 只是把线索补正。

---

## 4. 与 D2–D8 的先后（哪些必须同批）

| 缺陷 | 与命名契约的耦合 | 处置 |
|---|---|---|
| D1 | 本体 | 本文 §1–§3 |
| D2 验收闸门判"在册" | **强耦合，必须同批**：它的拒绝文案把出路写成 `jobs_manage(op=kill, handle=…)` ——D1 不修，"拒绝 + 请你先做 X"里的 X 不可达 | 闸门改判**作业状态**（只有 `running` 才拒；终态即放行）；`handleAlive` 降级为诊断（"在册但已终态"不再拦）；销项由编排面在 settle 完成时自动做一次（幂等） |
| D5 SKILL↔注册面漂移 | **强耦合**：改了前缀与工具名却不改提示词 = 立刻制造一处漂移 | 加 contract test：① SKILL/提示词里出现的 `team_*`/`bg_manage`/`jobs_manage` 必须在注册面存在；② **提示词里的命名表 == 代码里的 token 表**（从 `dto` 读，不比字面量） |
| D6 尾插/收尾硬化 | 弱 | 独立小项：teammate 系统提示写"完成即 commit"；尾插失败回执给可直接照做的下一步 |
| D7 卡点一格 / D8 拒绝说清"当前是哪种" | 同一判据（编排面同时知道闸门与作业状态） | 建议一次做完：`team_items` 每项带 `gate:`；`team_close` 的拒绝文案点名当前情形 |
| D3 读面"不存在即状态" / D4 实际壳单一事实源 | 无关 | 独立小项：D3 写进 `docs/arch/` 读侧口径 + contract test；D4 装配期解析一次实际壳，提示词与白名单同源 |

---

## 5. 先红后绿（复现用例）

按 MEMORY.md「先复现，再修复」，下面 1–3 条现在就红，修完转绿并留在仓库当回归：

1. `TestAsyncHandleCarriesSourceToken`：`bash_bg` / `read_batch` / `fork_subagents` 各派一条，
   断言句柄形如 `bt-*` / `bt-*` / `bs-*`（今天全红）。
2. `TestJobNamespaceGuardNamesTheOtherTable`：`bg_manage` 收到 `bteam-1`（或过渡期的 `a5`）必须报错，
   且错误文本含 `jobs_manage`；`JobNamespaceOf("bteam-3")` 不得被判成 `bt`（今天无此判据）。
3. `TestRetiredJobToolNameLeavesNoResidue`：`job_manage` 在代码与 `config/*.yaml` 零残留（见 §2.2），
   并断言仍在服役的 `jobs_manage` 不被误伤。
4. （需 Seele 版本才绿）`TestTeamworkHandleCarriesTeamToken`：leader 派发 → 回执句柄以 `bteam-` 开头。

---

## 6. 改动清单（file:line，按本文契约）

**铸造与契约**

- `seelebridge/tools/async_exec.go:314`：`fmt.Sprintf("a%d", g.seq)` → token + seq（按 kind 取 token）。
- `application/contract/dto/`：新增 token 常量 + `JobNamespaceToken` / `JobNamespaceOf`（唯一判据）；
  `dto/async_run.go:22` 的注释（"句柄原文（a3）"）同步。

**守卫与模型面文案**

- `seelebridge/tools/job_tools.go`（`scopedJobManage` 前置校验 + schema/描述）、
  `seelebridge/tools/router.go:117`（注册名）、
  `seelebridge/tools/job_contract.go`（错误文本、`asyncWorkRowPrefixForLine` 段落）。
- `application/core/work_table.go:748`：`async:<句柄>` 的读法说明按 token 分派（`bt-/bs-` → `bg_manage`）。
- `application/core/async_completion.go:278`、`application/core/teamwork_completion_trigger_test.go:57`：
  两侧取回工具的指名与示例句柄。

**提示词与配置（同源更新）**

- `plugins/default/teamwork/SKILL.md`（§2 工具表：补命名表 + 取回工具）、
  `plugins/default/goal/SKILL.md:72-73,114`（现在两工具的区别只写在**另一个** SKILL 里）、
  `plugins/default/README.md:43`。
- `internal/promptassets/assets/system/instructions.md`、`internal/promptassets/planact_harness.go:97-98`。
- `config/seelex.yaml:251-266` 与 `internal/bootseed/assets/config/seelex.yaml`（注释里的工具名）。
- `README.md:66`、`README_EN.md:18,244`、`seelebridge/README.md:243`、`seelebridge/tools/README.md`、
  `application/core/README-work-table.md:32`、`gui/frontend/README.md:704`、`seelebridge/fork/README.md`。
- 文档里的示例句柄（`a3`/`a5`/`a7` 一类）一并改（示例漂移就是 D5 的同类病）。

**门禁**

- `job_migration_residue_test.go`：把 `job_manage` 加进退役名单。
- 新增 D5 的 SKILL/提示词 ↔ 注册面 contract test（含"命名表与 `dto` token 表一致"）。

**不动**：`async:` 行前缀；`bash_bg` / `read_batch` / `fork_subagents` / `team_*` 的名字；
`docs/**` 的历史记录与 `CHANGELOG.md`（作为历史事实保留，只追加新条目）；
`sessionstore/board_test.go:117` 一类**历史 fixture**（它记录的是当时的存档内容，不是铸造口径）。

---

## 7. 待裁决（两条，选完即可开工）

1. **工具名**：`bg_manage`（推荐）／`local_jobs`／`bg_jobs`／要 Seele 支持 `WithToolName` 后取
   `team_jobs_manage`。
2. **`bteam-` 的路径**：走 Seele 一次小版本（推荐，§3.2 三项一起收，一次到位）；还是先只上 Seelex 侧
   （teammate 保持 `a*`，靠 §3.3 的守卫兜住静默损坏），`bteam-` 留到下次发版。

默认建议：**P0 先落 Seelex 自改**（§1 + §2 + §5 的 1–3 条用例，含 D2 同批），
`bteam-` 与 `handles` 对称一张 Seele 需求单等你发版。

---

## 8. 落地记录（2026-10-15）

### 8.1 实际交付

**Seele（本地仓 `G:/Program/go/Seele`，未发版）**

- 新增 `jobs.Namer`（`Name(spec Spec, seq int) Handle`）与 `jobs.WithNamer`；`New` 缺注入即
  `ErrNoNamer`；`manager.register` 不再铸 `Handle("a" + seq)`，改为取注入命名器的答案（仍在锁内
  调用，`seq` 与插入的原子性不变）。
- 新增两条回填校验：空句柄 `ErrInvalidHandle`、已在册句柄 `ErrDuplicateHandle`——**一条身份
  不允许指向两条作业**（这正是"跨表静默命中"的成因，现在在铸造处就挡住）。
- 库内两个调用点（`jobs/manager_test.go`、`jobs/builtin/manage_test.go`）改为测试自带的命名策略；
  `jobs/namer_test.go` 四条用例；`jobs/README.md` 增 Naming 一节；`docs/arch/16-jobs-contracts.md`
  增 §2.2 与 I-3 改写；`CHANGELOG.md` 增 v0.3.5（未发布）段。
- Seelex 侧以 `go.mod` 的 `replace` 接本地仓联调（与 2026-09-15/09-26/09-28/10-05 四次同路），
  **发版后必须删 replace 并抬 require**（见 `go.mod` 里的注记）。

**Seelex**

- 契约层唯一判据：`application/contract/dto/job_namespace.go`（`JobHandle` / `JobNamespaceOf` /
  `JobHandleSeq`；分隔符是文法的一部分）。
- 后台表按 kind 铸 `bt-` / `bs-`（`seelebridge/tools/async_exec.go` 的 `beginJob` +
  `jobNamespaceForKind`）；团队表由 `seelebridge/teamwork_job_namer.go` 注入（`jobs.WithNamer`）。
- 工具改名 `job_manage` → `bg_manage`（注册名收敛成 `bgManageToolName` 常量；代码与 `config/*.yaml`
  零残留由 `job_migration_residue_test.go` 的退役名单把守）。
- 跨表守卫：`requireOwnedJobHandles`（正向白名单，只认本表两个 token），拒绝文案点名 `jobs_manage`。
- D2：验收闸门改判**作业状态**（新增 `jobRunning`；`handleAlive` 保留给"在册"那类问题，两者
  **刻意不并**并把理由写在注释里）；`team_accept` 通过后自动销掉那条作业行（`retireSettledJob`，幂等）。
- D5 门禁：`tool_face_prompt_consistency_test.go`——注册面 → 提示词、提示词 → 注册面、**提示词里教的
  token 集合 == `dto.JobNamespaces`** 三个方向；名单从源码扫描器读，不另写一份字面量。

### 8.2 两处与本文原方案不同的判断（都有证据）

1. **自动销项放在"验收通过"那一刻，不是"尾插完成"那一刻。** 原方案写的是"settle 完成时自动销一把"，
   落地时发现不可行且有害：尾插**先于**终态完成（`seelebridge/teamwork/executor.go` 里 settle 在
   `sink.Complete` 之前），那一刻作业还是 `running`，销项必然失败；更要紧的是**终态行正是下游两条链
   读的证据**——"作业做完自动返回"的触发与看板事件投影都靠它在册才看得见那次终态，提前销掉等于把
   证据一起删掉。验收才是"这件事结清"的时刻（计划销项 + 作业行销项），卸掉的仍是同一条隐性职责：
   leader 不必记得调 `op=done`。
2. **顺手修掉一处同类缺陷（本文未预见）：`handleSeq` 也在猜句柄形状。** 后台探针的派发序排序原来
   靠"首字符是 `a`"，换 token 后每条都解析成同一个键，排序退化成 map 遍历顺序（既有用例
   `TestProbeOrdersByDispatchSequenceNotHandleText` 当场变红）。现在解析统一走 `dto.JobHandleSeq`，
   形状知识仍然只有一处（契约层）。附带把仓库的注册点扫描器从"同文件常量"升级为"同 package 常量"：
   注册名改用跨文件常量后，`TestEveryRegisteredToolDeclaresGroups` 会误报无法静态解析。

### 8.3 仍未做（登记，不含在本次交付）

- `jobs_manage` 收 `handles`（一批一次等）—— schema 对称那一半，属 Seele 的**新能力**，建议与
  下一次 Seele 版本一起收。
- D3（读面"不存在即状态"通则）、D4（实际壳单一事实源）、D6（尾插/收尾硬化）、D7（卡点格）、
  D8（拒绝说清"当前是哪种"）：与身份层无关，仍是独立小项。
