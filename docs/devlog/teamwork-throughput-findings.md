# teamwork 吞吐实测与编排规则（2026-10-29 一轮 prefix+goal 作业）

## 1. 实测数据

| 阶段 | 时长 | 事实 |
|---|---|---|
| goal 创建 → 首个作业派发 | 5h39m | **零提交**：leader 单线程实现 prefixprobe（11 用例 + harness） |
| 团队阶段（7 件完成） | 103 min | 单件累计 15489s，**平均并发 2.10**，**零空转** |
| 单件耗时 | 17–70 min，中位约 40 | `wi-m1-verify` 17min（目标窄）↔ `wi-m1-frames` 70min（7 文件 + 翻转验证） |

派生：

- 团队阶段没有任何一段「派了没人接」或「等回执等出空档」——慢的不是等待，是**深度**（同时可派件数只有 2）。
- 真正的时间黑洞在团队阶段之前，而且是 leader 自己的工作方式造成的，与 teamwork 机制无关。

## 2. 并发只有 2.1 的三个原因

1. **依赖画长**：spur 是 `audit → impl → wire` 三串、前缀是 `frames → verify` 两串、M2 三串。
   首批三件能并起来（env/compact/audit），恰恰因为那批本来就没有依赖。
2. **里程碑屏障过厚**：`m3/m4/m5` 全锁在 `m2` 之后。`m4`（memory 纪元表）与 `m2`（L1 装配切片）
   之间没有真实依赖，它是另一块字节来源；已改为 `m4-epoch depends_on [m1-freeze]`。
3. **一次只派一条的旧习惯**：做完一件、验收完才派下一件，回执来之前并发度掉到 1。

## 3. 派活固定税（估算，非实测）

每次派发的固定开销 = worktree 创建 + 冷启动摸仓库 + 交接文本 + 合并 + leader 验收复跑。
按本轮 7 件的形态推算约 5–15 min/件；对 40 min 的活摊得开，对 17 min 的活基本被税吃掉。
**同一件事 leader 直接做通常更快**（已有一例：定位 `wi-m2-slice` 的三个接缝坐标，leader 三个搜索即得，
派出去要一个回合重新摸）。

## 4. 派活判据（四条，按优先级）

1. **能并行**（当前 ≥2 件 ready）→ 派。并行收益大于固定税。
2. **需要独立视角**（评审、验收、防自证偏差）→ 派。自己写的用例自己验收，容易放过恒真断言。
3. **会吃满 leader 回合的长命令**（构建、全量回归、大范围搜索）→ 派。
4. **只有一件、且 leader 已有上下文** → **自己做，不派**。

反向教训：本轮把两件事做反了——把「6 小时的探针实现」留在自己手里（该派），
把「几分钟的代码定位」留着（不该派，且后来确实又派了一轮让 teammate 重新摸）。

## 5. 其他摩擦（工具面）

- `jobs_manage` 不支持批量句柄（`handles: [...]` → `json: unknown field "handles"`），`team_join` 会回
  `context canceled`；批量读只能用 `team_context(roles=[...])`。
- `worker` 冷启动：每件新 worktree + 重新 orient；`max_turns: 60` 对「7 文件 + 用例」的活不够，
  本轮已统一调到 100–120。
- leader 验收若每件都复跑全量（integration 单包 60s+），等于自己造串行；改为只跑直接相关包，
  全量回归交给收口件跑一次。

## 6. 欠费不是"慢"，是失败——但它伪装成长跑

2026-10-29 14:11 派出的两件（`wi-spur-persist` / `wi-spur-surface`）跑了 30+ 分钟，其中一件**根本没跑完**：

```
spur/wi-spur-persist  failed  session loop 203
  HTTP 402 {"message":"Insufficient Balance","type":"insufficient_balance_error"}
```

`wi-spur-surface` 34 分钟交付（19 文件 / 1055 行），`wi-spur-persist` 在同一时段空转 **203 个 session loop**
后撞上 402 退出。两个作业并行争夺同一个账号（`account-1`），余额耗尽后重试不报"欠费"，而是继续开 loop，
表现为**看不出卡住的长跑**（`bytes=0`、loop 计数持续上涨）。

由此得到两条纪律：

1. **判据**：一个作业长时间无任何产出增量，先去查 loop 计数与最近一次 bridge 错误，别等。
2. **并行作业共享账号额度**：并发度越高，额度消耗越快，撞 402 的时间点越早。别把"并发"当免费。

## 7. 危险：`team_accept` 会回收 worktree **并删掉该工作项的分支**

本轮实例：`wi-spur-surface` 的交付在分支 `seelex/spur-wi-spur-surface` @ `7e41f01f`
（19 文件 / 1055 行），因为 main 被并行工作占用而**未能合并**。对它 `team_accept` 之后：

```
$ git branch --list '*spur*'
+ seelex/spur-wi-spur-persist        ← surface 那条不见了
$ git log --oneline -1 seelex/spur-wi-spur-surface
fatal: invalid object name
$ git cat-file -t 7e41f01f
commit                                ← 提交还在，但是悬空的
```

**结论**：accept 释放 worktree 时会连分支一起回收；未合并的交付退化为悬空提交，
只能靠 sha 找回（`git branch <name> <sha>`），一旦 GC 就真没了。
已用 `git branch seelex/spur-wi-spur-surface 7e41f01f` 恢复。

**纪律（写死）**：

1. **合并失败时不要 accept**。先解决合并，或至少先打保护分支 `git branch keep/<item> <sha>`，再 accept。
2. 多个 agent 并行改同一仓库时，**main 工作区是争用点**：要么指定唯一的合并者，
   要么让每个 worktree 自己 rebase 后排队合；不要让「等下再合」跨过 accept 这个动作。
3. 收口前例行检查：`git branch --list 'seelex/*'` 与 `git log --all --oneline -N` 对照看板，
   确认每件已验收的交付都真的在 main 上。
