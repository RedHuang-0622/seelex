# Teamwork 缺陷审计（2026-10-15）

状态：**审计结论 + 最小修法建议**（未改代码；按优先级排，等裁决）
范围：teamwork 编排面（`seelebridge/teamwork`、`seelebridge/runtime_teamwork*`、
`plugins/default/teamwork|goal` 的 SKILL）与它依赖的两张作业表。
方法：三份证据交叉——**代码锚点**（file:line）、**真实会话轨迹**（
`dist/seelex-gui-dev/.seelex/sessions-json`，137 份分片）、**headless 实调**
（2026-10-15，真实模型 + 真实控制面）。

---

## 结论（一句话）

teamwork 的**语义层**（里程碑屏障 + Work Item DAG + 一件事一套隔离）站得住；
真正在伤人的是**身份层**：同一概念在两个实现里各长一半（两个作业表、两套句柄、两个
只差一个字母的工具名），以及**把"谁负责收尾"寄存在调用方自觉上**（验收闸门、尾插失败、
销项）。下面 8 条按严重度排，前 2 条会造成**静默的数据/状态损坏**。

---

## D1（P0）两个作业表共用一套句柄名，两个工具名只差一个字母

**现象**：同一个会话里 `bash_bg` 返回 `a1`，`team_dispatch` 也返回 `a1`；两个工具
（`job_manage` / `jobs_manage`）**都能"成功"**，各自返回**不同**的作业。

**证据**

- Seelex 后台作业面造句柄：`seelebridge/tools/async_exec.go:314`
  `handle := fmt.Sprintf("a%d", g.seq)`。
- Seele teammate 作业面造句柄：`!seele@v0.3.4/jobs/manager.go:267`
  `handle := Handle("a" + strconv.Itoa(m.seq))`——同形、同起点、**同一会话内共存**。
- headless 实调（2026-10-15）：`bash_bg{git log --oneline}` → `{"handle":"a1"}`；
  `team_dispatch{item:wi-1}` → `{"handle":"a1"}`；
  `jobs_manage{op:observe,handle:a1}` → `kind=worker,node=wi-1`（worker 表）；
  `job_manage{op:observe,handle:a1}` → `async:a1 process done …`（后台表）。
  **两个调用都没有 error 字段**。
- 真实会话后果（`session-485eb286107affb3`）：leader 想停 teammate 的 `a5`，调
  `job_manage(op=done, handle="a5")`——后台表里恰好吃到一个无关作业，被**静默销项**
  （返回成功）；紧接着 `kill` 报"未知句柄"，验收继续被拒。
- 提示词已经警告过（`plugins/default/goal/SKILL.md:73`："别和 job_manage 混用"），
  模型照混——**这类缺陷不可能靠措辞修**。

**为什么是设计缺陷**：句柄是**跨表不唯一的外部身份**，而工具名只差一个字母、schema
还在两处各长一半（`job_manage` 收 `handles` 数组：`seelebridge/tools/job_tools.go:31`；
`jobs_manage` 不收，实测报 `unknown field "handles"`）。调用方**没有任何线索**判断
"这个 a5 属于哪张表"。

**最小修法（按性价比）**

1. **让句柄自带来源**：后台表的取回回执已经在写 `async:a1`，把它提为**对外唯一形态**
   （`job_manage` 只认 `async:*`），teammate 侧对应 `job:*`；跨表调用因此**显式报错**
   而不是静默命中。
2. 或把两张表合到一个工具、按句柄前缀路由（工具名只留一个）。
3. 无论选哪个：`jobs_manage` 与 `job_manage` 的 schema 必须**对称**（要么都收
   `handles`，要么都不收）。

---

## D2（P0）验收闸门判"行还在册"而不是"作业还在跑"

**现象**：teammate 干完后，工作项的验收仍可能被拒——因为闸门看的是"句柄在不在登记表里"，
而**没有人自动销项**；能不能验收取决于"谁先想起来销项"。

**证据**

- `seelebridge/teamwork/items.go:1049` `handleAlive` = `c.jobs.Observe(handle)` 返回 ok
  （终态行**仍在册**）。
- 拒绝文案：`seelebridge/teamwork/items.go:693`（"还在跑（handle … 还在册）…或先
  `jobs_manage(op=kill, handle=…)` 把它停掉"）。
- 实测：`team_accept` 22 次里 4 次被拒；其中同一工作项**连续两次**同一拒绝，随后模型按
  文案去 kill——却因 D1 打到了对偶工具的表，白跑两轮。

**为什么是设计缺陷**：终态驱动的状态机不该把"回收动作"寄存在调用方自觉上。文案给的出路
（`jobs_manage(op=kill)`）同时依赖 D1 修好，否则"拒绝 + 请你先做 X"里的 X **不可达**。

**最小修法**：闸门按**作业状态**判（只有 `running` 才拒；终态即放行——settle 链已经
合并完并投了回执）；销项由编排面在 settle 完成时自动做一次（幂等），不要求 leader 记得。

---

## D3（P1）读面把"还没有团队"当成错误（已修一处，通则未立）

**现象**：模型在没有团队计划的会话里调 `team_items` / `team_context`，收到
`open …\metadata\teamwork.json: The system cannot find the path specified.`，
并自行判定"这是错误，不是空结果"。

**证据**：`sessionstore/teamwork.go` 的 `ReadTeamworkPlan` 原样上抛 `os.ErrNotExist`
（同文件的 `ReadTeamworkEvents` 早已是"缺失即空集"）；实调复现见 2026-10-15 headless
探针（随后已修：读面给空结果，写面给领域文案 `本会话还没有团队计划——先用 team_plan…`）。

**为什么是设计缺陷**：**"不存在"是状态，"读不出来"才是错误**——这条通则没有落在
契约层，于是同一个包里两种口径，谁新写一个读面就可能再犯一次。

**最小修法**：把通则写进 `docs/arch/`（模块 head 读侧口径），并在 contract test 里钉一条
"缺失 head 的读面必须返回空值而不是错误"。

---

## D4（P1）执行壳、判定白名单、描述三份事实（bash_read 已删，教训要留）

**现象**：`bash_read` 的判定白名单是 POSIX 词表（`ls -la` 合法），而本机执行壳退化成
PowerShell（`ls -la` 必失败、`Get-ChildItem` 必被拒），描述又只写"写命令会被拒"。
实测 479 次调用 165 次（34.4%）折在判定上，成因分布 复合语法 102 / PowerShell 命令面 38 /
白名单外子命令 18 / 其它 7；另有 48 次（15.3%）"报 success 但退出码非零"。

**证据**：`seelebridge/tools/router.go:695` `scopedBashCommand`（优先 Git Bash，否则
PowerShell）；本机 `bash` 解析到 WSL 被 `IsWSLBash` 拒 ⇒ 实际壳 = `powershell.exe`。
该函数自己的注释就写着"用 PowerShell 优先会拒掉 `pwd && ls -la` 这类普通命令"——
执行层为复合命令让路，判定层把它们全拒，**两个设计互相拆台**。

**现状**：工具已删（2026-10-15 用户裁决），判定与死代码一并退役。但**"实际壳是什么"仍然
没有单一事实源**：剩下的 `bash` 依旧会落到 PowerShell，而权限白名单/系统提示词/模型认知
都按 POSIX 写。

**最小修法**：装配期**解析一次实际壳**，把结论写进系统提示词的环境段（"本机 shell =
powershell 5.1：`ls -la`/`head`/`grep` 不可用，用 `Get-ChildItem`/`Select-String`/`rg`"），
并让权限白名单模式表按**同一份结论**分派（现在是写死的 POSIX 形态）。

---

## D5（P1）工具面与 SKILL 漂移：文档承诺 ≠ 注册事实

**现象**：`$teamwork` / `$goal` 的 SKILL 描述了一套 leader 工具面（含 `team_milestone`、
`team_join`、`team_recover`、stages 语义），而注册面在 `seelebridge/runtime_teamwork.go:219`
起是 12 个 `team_*` + `jobs_manage`，且计划结构里**没有 stages**（2026-10-04 已整条退场）。
SKILL 里"取回产出优先用 `team_context`"与 `jobs_manage(op=fetch)` 的**消费式**语义
（取尽即销项）也容易互相打脸。

**证据**：`seelebridge/runtime_teamwork.go:223-241`（注册面）；
`plugins/default/teamwork/SKILL.md`（第 2 节的工具表）；
`plugins/default/goal/SKILL.md:73`（唯一一处点明两工具区别的地方——在**另一个** SKILL 里）。

**最小修法**：加一条 contract test：SKILL 工具表里出现的每个 `team_*`/`jobs_manage`
必须在注册面存在，且注册面里每个 `team_*` 必须在 SKILL 里出现（名单从注册表读，不比字面量）。

---

## D6（P1）"teammate 要提交、要收尾"只写在文档里，没进硬约束

**现象**：真实会话里 leader 为 **4 件以上**工作项手工补提交、变基、解冲突、合并——
说明"teammate 收尾 commit"这一步在执行面没有硬约束，尾插一旦遇到未提交改动就把现场留给
leader。

**证据**：`team_accept` 的 note 里反复出现"teammate 未执行收尾 commit，leader 补提交 …"；
`seelebridge/teamwork/README.md` 的尾插段落写明"有未提交改动却没有提交时**显式报错**，
现场保留"。

**最小修法**：① 把"完成即 commit"写进 teammate 的系统提示（不是只在 SKILL 里）；
② 尾插失败时的回执给出**可直接照做的下一步**（现在是 bug 原文 + "请 leader 亲自执行"）。

---

## D7（P2）可观察性缺"卡点"这一格

**现象**：leader 只能靠 `team_items`（状态/依赖/绑定）+ `team_context` + `job_manage`
反推"这件事到底卡在哪"；实测里 leader 反复轮询取回工具几十次（同一会话
`job_manage(op=fetch)` 出现 20+ 次）。

**最小修法**：`team_items` 每项带一格**卡点**（`gate:依赖未 done` / `job:running` /
`review:等你裁决` / `merge:冲突待处置`）。判据放编排面（它同时知道闸门与作业表），
前端只显示。

---

## D8（P2）零散但会立刻失败的两处

- `fork_subagents` 的 `id` 必填没有出现在模型读得到的约束位置（实测 1 次
  `subagent "" must have id and goal`）。
- `team_close` 在"还有没落定的工作"时拒绝（`unsettledItems`），但拒绝文案列了四种情形、
  没有告诉 leader **当前是哪种**（与 D7 同一根因）。

---

## 优先级与建议顺序

| 序 | 缺陷 | 为什么先做 | 影响面 |
|---|---|---|---|
| 1 | D1 句柄/工具名撞车 | 唯一会**静默销掉别的作业**的一条 | 契约（Seele 句柄形态）+ 两个工具 |
| 2 | D2 验收闸门按状态判 + 自动销项 | 去掉"leader 必须记得销项"这条隐性职责 | `seelebridge/teamwork` |
| 3 | D4 实际壳写进提示词/白名单同源 | 让剩下的 `bash` 不再"过闸即死" | 装配期一次解析 + 白名单 |
| 4 | D3/D5 通则与漂移门禁 | 防下一处同类复发（契约 test 即可） | 文档 + contract test |
| 5 | D6/D7/D8 | 降低 leader 手工成本与轮询 | 提示词 + `team_items` 加一格 |

## 复现与证据索引

- 句柄撞车（headless，可重跑）：本目录同级探针
  `_tmp/probe-class/prompt-b.txt`（`bash_bg` + `team_dispatch` 同会话），
  输出快照 `_tmp/probe-class/snapshot-b2.json`。
- `bash_read` 拒绝分布（165 条真实命令重放）：`_tmp/probe-class/commands.txt`。
- 真实会话轨迹：`dist/seelex-gui-dev/.seelex/sessions-json/project-03b5b8d29af3f44b/
  session-485eb286107affb3`（验收被拒 + 错表销项）、`session-4cac98a2b9ec667b`
  （计划缺失报 ENOENT）。
- 计数口径的现场数据（本文多处引用）：`docs/arch/teamwork-leader-worker-architecture.md`
  与本轮工具记录台账（`<数据根>/tool-record.jsonl`，按会话分、批次落盘）。
