# 新会话提示词（接续 2026-09-16 权责系统 / Agent Team 工作）

> 生成：2026-09-16 | 生成依据：`git log` + 源码/测试复核 + 三个并行审计子代理（tier-audit /
> advisor-audit / gaps-audit）的实测证据 | 状态：一次性工作包（不冒充长期事实来源）
>
> 本文件有两部分：**A. 现状与未兑现清单（带证据）** 与 **B. 可直接粘贴的新会话提示词**。
> 清单里每条都注明"已核实/有意保留/未兑现"，请以代码与测试为最终事实来源。

---

## A. 现状与未兑现清单（可复核）

### A0. 工作区快照（实测）

| 事实 | 证据 |
|---|---|
| HEAD = `b55382a`（2026-09-16 18:15），工作树干净 | `git status --porcelain` 空 |
| 上一会话的单日主线：权责系统（主体×路由组×位）+ Agent Team"有承载体" | `d0900f1 → aba9539 → 6817554 → 2bb6312 → f43f635 → 3e50bc1 → b55382a` |
| **`go build ./...` 与 `go vet ./...` 当前直接失败**（因为 `tmp/` 被 `./...` 扫到）：`tmp/final/*.go` 里混了 `dto` 与 `contract` 两个包 | 实测 `go build ./...` / `go vet ./...` = exit 1；`tmp/` 在 `.gitignore:85` 内，所以 `git status` 是干净的（隐形破坏）；显式排除 `tmp` 后 `go build` = exit 0 |
| `gofmt -l .` 报 `tmp\goal-tl-live-smoke\smoke_test.go`（既有 scratch，非本轮引入） | 实测 gofmt 输出 |
| 定向测试绿：`./seelebridge/tools -run Tier`、`./application/core -run "Tier\|RoleTurn\|Permission"`、`./gui -run "PermissionTier\|Frontend"` | 实测均 `ok` |
| **GUI 二进制过期**：`dist/seelex-gui-dev/seelex-gui.exe` 时间 09-16 12:28，早于档位前端改动（17:05+） | 目录 mtime；肉眼验收档位面板需重建（重建含 `rm -rf dist`，见 B 段纪律） |
| 两个陈旧 stash（`9-01 WIP on main`、`7-26 On gui`），与当前主线无关 | `git stash list` |

### A1. 未兑现清单

**P0 · 门禁卫生（必做，非主题性工作）**

| # | 条目 | 证据 |
|---|---|---|
| P0-1 | `tmp/` scratch 使 `go build ./... / go vet ./... / gofmt -l .` 全部不可用（AGENTS.md §5 的常用验证命令因此失败） | 见 A0；`tmp/final/{application__contract__ports.go,application__contract__dto__permission.go}` 等 9 文件 + `tmp/head_reg.go` |

**P1 · 能力缺口（用户口径里最直接"没兑现"的两块）**

| # | 条目 | 证据 |
|---|---|---|
| P1-1 | **档位不落盘**：主会话权限档位是 `SessionUnit` 内存槽 + 快照投影，`sessionstore/` 无任何 `PermissionTier` 持久化路径 → 重启回到 `-permission` 默认值；用户在面板选的档位跨重启即丢 | `session/runtime_slot.go:53-72`、`session/ports.go:228-232`、`application/model/state.go:209-214,654-656`；`grep PermissionTier sessionstore/*.go` = 0 命中；方案文档 D5 标为"有意不落盘"（`docs/2026-09-16-session-permission-tiers/README.md` §10） |
| P1-2 | **运行期 CLI/TUI 无法切档**：`main.go:1216` 注释称"运行期由 GUI/CLI 按会话切换"，但 `application/core/command.go` 注册的只有 `help/clear/model/history/trace/compact/new/resume/fork/sessions/pool/plugins/plugin/diag/exit/effort`，**没有 `/permission`**（GUI 有 chip + 运行状态弹窗列表） | `grep register( application/core/command.go`；`main.go:1216` 注释 |
| P1-3 | **角色回合 / computer use 的真实 API 冒烟仍是"下一件事"**：仓库内无覆盖 `RunRoleTurn` 的真实 API 冒烟；`gui/role_live_probe_test.go` 最后改动 09-10，早于 `RunRoleTurn`（09-16） | `grep RunRoleTurn` 仅命中 `seelebridge/runtime_role_turn.go` 与其单测；`FOLLOWUP-role-turn-body.md` §4.1 自述；`gui/role_live_probe_test.go` mtime 2026-09-10 |
| P1-4 | **ADVISOR "可执行验证接地"只兑了一半**：评审者拿到的是 `ro` 组（read_file/grep_search/glob/读结果/读计划…），**`bash` 属 `rw` 组，不在只读面内 → 评审者仍不能跑测试/编译**；源码头注释自认"能跑测试的评审者是下一步" | `seelebridge/runtime_goal_tl.go:16-25`（边界自述）、`:184-190`（`ToolsPolicy: dto.ToolPolicyReadonly` 字面量构造）、`seelebridge/tools/permission_policy.go:656-663`（readonly = 仅 `ro: read`）；`A2A-VALUE-REVIEW.md` §2.5/§3.3 |

**P2 · 文档债与契约缺口（低风险，随时可做）**

| # | 条目 | 证据 |
|---|---|---|
| P2-1 | **f43f635（ADVISOR 只读工具）完全无文档**：`git show --stat f43f635` 只有 6 个 .go 文件，无 `docs/`、无 `CHANGELOG.md`；而 `CHANGELOG.md:72` 仍写着 "ADVISOR still holds no tools"，`CHANGES.md:117-118`、`FOLLOWUP-role-turn-body.md:86-89` 同款过时 | `git show --stat f43f635`；`CHANGELOG.md:72` |
| P2-2 | `CHANGES.md` §"已知风险与未做项" 第 1 条（`application/core` 的 `-race` 夹具竞态）**已由 6817554 修掉但文档未回填** | `application/core/service_fakes_test.go`（`projectRootMu`/`ProjectRootValue` 8 处）+ `application/core/fixture_concurrency_test.go`（AST 机械防线） |
| P2-3 | 档位前端**无 node 用例**：方案文档 §9 承诺 `node --test` 覆盖，实际只有 `snapshot-shape.test.mjs:95-98` 的键归属；档位列表 / chip 渲染无任何 mjs 断言 | `grep "permission_tier\|permissionTier\|perm-toggle\|runtime-permission" gui/frontend/dist/*.mjs` 仅命中 `snapshot-shape.test.mjs` |
| P2-4 | `gui/headless.go:274-279` 有 `SetPermissionTier` 分派但**无用例**覆盖 | grep 无 headless tier 用例 |
| P2-5 | A2A §3.3 另外三步未落地：①"只在值得的任务上开"的开关（默认常开 = 负期望）②"量起来再决定"的 A/B 对照（现有 b 侧回合级 token 打点 `application/core/goal/advisor.go` `Round{InputTokens,CachedTokens}` + `CacheStats`，但没有开关对照汇总）③跨进程 A2A（结论是"暂不投"） | `A2A-VALUE-REVIEW.md` §3.3；`advisor.go` Round/CacheStats 结构 |
| P2-6 | `Progress` 仍是"非空结论"保守近似，`no_progress` 门限未调 | `seelebridge/runtime_role_turn.go:30-31,149-151`；`application/core/agentteam/runtime.go:176-198` |

**P3 · 大件（需用户先决策）**

| # | 条目 | 证据 |
|---|---|---|
| P3-1 | 角色会话号 `teamID-roleName` 不含主会话身份 → 跨会话重号靠"取最严"兜住；根除需存储迁移 | `application/core/agentteam/spec.go:213-220` |
| P3-2 | 角色会话引擎进程内、**不接 `DurableHistory`**（重启失忆）；角色回合只绑项目根、无 worktree 隔离 | `seelebridge/runtime_role_turn.go:28-29,265-267`；`FOLLOWUP-role-turn-body.md` §4.4/§4.5 |
| P3-3 | 第五档 `readonly` 未做（当前 4 档 `manual/edit/auto/full`，D1 有意） | `application/contract/dto/permission.go:96-125` |
| P3-4 | 座位链序仍保"EXEC 先于 ADVISOR"（本轮只改了座位**存在性**的派生依据） | `CHANGES.md:119-120` |

### A2. 不要被误导的地方

- `CHANGELOG.md:72`、`CHANGES.md:117-118`、`FOLLOWUP-role-turn-body.md` §4.2 说"ADVISOR 无工具"——
  **已被 f43f635 兑现**，只是文档没回填。
- `CHANGES.md` §"已知风险与未做项" 第 1 条的 `-race` 夹具竞态**已修**（6817554）。
- `docs/2026-09-16-session-permission-tiers/README.md` 的 §6 是"前端"，**不是**"当前缺口"（全仓无
  "当前缺口" 标题）；它的 §8 才是边界、§10 才是决策点 D1–D6。

---

## B. 新会话提示词（可直接粘贴）

```text
仓库：G:\Program\go\seelex（Go 1.25，Windows/PowerShell；gofmt/go vet/go test/node --test）。
HEAD = b55382a（2026-09-16 18:15），工作树干净。先读 AGENTS.md 与 MEMORY.md，再读
docs/devlog/2026-09-16-session-permission-tiers.md、
docs/2026-09-16-ring-escape-permission-bearing/{CHANGES.md,A2A-VALUE-REVIEW.md}、
docs/2026-09-16-ring-escape-permission-bearing/FOLLOWUP-role-turn-body.md、
docs/2026-09-16-next-session-prompt/README.md（本清单的来源与证据）。

【上一会话做了什么（已落地，可复核）】
单一主线 = 权责系统（主体 × 路由组 × 位）落地 + Agent Team 从"说话权"变成"做工权"：
  d0900f1 环逃生复活 + tools_policy 写入侧枚举化
  aba9539 逃生收口为 goal 中止 + 座位按角色 kind 派生
  6817554 员工权限按主体分配（装配期，emp_<角色>）
  2bb6312 角色回合执行体 RunRoleTurn（角色会话 + 员工主体按构造 + 项目根绑定）
  f43f635 ADVISOR 评审回合带只读工具（评审者会话坐标 advisor:<主会话>）
  3e50bc1 员工「继承」口径的越权提权接进现有审批面板（跨层验收）
  b55382a 主会话权限档位 manual/edit/auto/full（会话粒度 + 运行面板列表 + chip 就地替换）

【第 0 步：先修门禁（必做，先问我确认，别擅自删东西）】
我实测：`go build ./...` 与 `go vet ./...` 现在直接失败（exit 1）——`tmp/` 被 `./...` 扫到，
`tmp/final/*.go` 里混了 `dto` 与 `contract` 两个包（`tmp/` 在 .gitignore 里，所以 git status 是干净的，
问题被藏住了）。显式排除 tmp 后 `go build` 全绿。
要求：把 `tmp/` 从 `./...` 里摘出去（改名为 `_tmp`、移出模块、或给 tmp 加嵌套 go.mod，任选并说明理由），
让 AGENTS.md §5 的六条常用验证命令重新可用；**不要删除 tmp/ 下任何内容，先给方案让我确认**
（MEMORY.md 危险操作铁律；tmp 里有历史冒烟产物）。做完给出门禁命令的实测输出。

【主线选项（按性价比排序，选一条或按我指定的顺序做；每条都要"先复现/先写会红的测试"）】
P1-1 档位落盘：主会话权限档位现在只在内存（session/runtime_slot.go:53-72），重启即回默认值。
     用户口径是"这一 session 的权限设置"，跨重启该记住。落点：会话 record（application/model/state.go
     的 SessionRecord 家族）+ sessionstore 读写 + 恢复路径；给"选 full → 重启 → 仍是 full"的回归用例。
     （方案文档 D5 当时标"有意不落盘"，改它要说明是需求变更，并回填文档。）
P1-2 运行期 CLI/TUI 切档：main.go:1216 注释写"运行期由 GUI/CLI 按会话切换"，但
     application/core/command.go 里没有 /permission（只有 /effort）。补齐命令 + 帮助 + 用例，
     并让 headless 分派（gui/headless.go:274-279）也有用例。
P1-3 角色回合的真实 API 冒烟：FOLLOWUP-role-turn-body.md §4.1 自述这是"下一件事"，
     现在 RunRoleTurn 存在、ADVISOR 也有只读工具了，正好一起冒一次（真引擎 + 真权限面 +
     computer use 实机跑一轮；参考 gui/team_work_computer_use_live_probe_test.go 的 SMOKE_* 开关写法）。
     验收要给出真实输出的关键片段，不要只写"通过了"。
P1-4 ADVISOR 的"可执行验证接地"只兑了一半：只读面里没有 bash（rw 组），所以评审者仍不能跑测试/编译。
     要往前推就得给评审者一把更细的执行位（新档位/新路由组/受限 bash），这会动权限模型 —— 先给设计
     与我确认，别直接放开 rw。

P2 文档债（低风险，可与主线并行，做完直接进主线提交或单独一次 docs 提交）：
  - f43f635（ADVISOR 只读工具）**完全没有文档**：补 CHANGELOG [Unreleased] 条目 + devlog；
    同时把过期结论回填掉：CHANGELOG.md:72 "ADVISOR still holds no tools"、
    CHANGES.md:117-118、FOLLOWUP-role-turn-body.md §4.2。
  - CHANGES.md "已知风险与未做项" 第 1 条（application/core 的 -race 夹具竞态）已由 6817554 修掉，
    回填"已修 + 证据"。
  - 档位前端缺 node 用例（现只有 snapshot-shape.test.mjs:95-98 的键归属）：给档位列表/chip 补
    node --test 断言（文档 §9 承诺过）。
  - A2A-VALUE-REVIEW.md §3.3 的另两步：给 ring 一个"值不值得开"的开关（默认常开是负期望），
    再用已有 b 侧 token 打点（application/core/goal/advisor.go 的 Round/CacheStats）做"开/不开 ADVISOR"
    的同批任务 A/B，把结论写成数字。

P3 需我决策才能动（先给方案，不要自己开工）：角色会话号编入主会话身份（存储迁移）、
  角色会话引擎接 DurableHistory、角色回合的 worktree 隔离、Progress/no_progress 门限、第五档 readonly。

【纪律（按仓库既有规范）】
1. 修复类工作：先复现（优先落成会失败的测试）→ 只改根因 → 重跑复现（红→绿）→ 沉淀回归（MEMORY.md 铁律）。
2. 变更接口/JSON 字段/CLI flag/持久化格式 → 同步 README、示例与测试（AGENTS.md §4）。
3. 提交前跑：gofmt -l .、go build ./...、go build -tags "gui,desktop,production" ./...、go vet ./...、
   go test ./... -count=1 -timeout=120s、node --test gui/frontend/dist/*.test.mjs。
   Windows 本地若 CGO_ENABLED=0，不要声称已跑过 -race。
4. 需要肉眼验收 GUI 时：dist/seelex-gui-dev/seelex-gui.exe 是 09-16 12:28 的旧产物，早于档位前端改动，
   要重建（make rebuild-gui）；该目标含 rm -rf dist → 必须先中文预警、先查 seelex 进程、先备份、等我确认。
5. 一个 commit 一个主题；不要自动 commit / push；不要动两个陈旧 stash（9-01 / 7-26）。
6. 结论要标"已核实 / 假设"，并给 文件:行 或命令输出；不要用文档里的旧结论当事实。

【产出要求】
- 先给我第 0 步的门禁修复方案与实测输出；
- 再说你选哪条主线、为什么，以及它的验收断言（红→绿的具体用例名）；
- 最后按"代码 + 测试 + 文档三同步"交付，并列出没做完/不打算做的部分（要明说）。
```

---

## C. 生成本清单时实际执行的验证（可复现）

```powershell
git log --oneline -8 ; git status --porcelain ; git stash list
go build ./...            # exit 1（tmp/final 两个包）
go vet ./...              # exit 1（同因）
go build ./application/... ./e2e/... ./gui/... ./mcpstack/... ./plugin/... ./seelebridge/... `
         ./seelexctx/... ./session/... ./sessionstore/... ./skill/... ./tui/... ./workspace/... .   # exit 0
go test ./seelebridge/tools/ -run "Tier" -count=1
go test ./application/core/ -run "Tier|RoleTurn|Permission" -count=1
go test ./gui/ -run "PermissionTier|Frontend" -count=1
git show --stat f43f635 ; git show --stat b55382a
```

（对应用例：`TestTierDecisionMatrix`、`TestTierDoesNotBypassEmployeeBoundary`、
`TestTierDoesNotBypassSubagentBoundary`、`TestTierSessionIsolation`、
`TestPermissionTierOwnershipPerSession`、`TestSetFullAccessCompatMapsToTier`、
`TestBridgeForwardsPermissionTier`、`runtime_role_turn_test.go` 8 例。）

---

## D. 回填（2026-09-16 21:5x，本文件写完之后）

> 本节只记"清单写完之后又兑现了什么"；仍以代码与测试为最终事实来源。

- **P0-1（门禁卫生）**：`tmp/` 的 scratch `.go` 已移出 `./...`（`tmp/` 下只剩 `bin/`）。
  `go build ./...`、`go vet ./...`、`gofmt -l .` 实测 exit 0 / 无输出。
- **P1-1（档位落盘）、P1-2（运行期切档）**：`application/core/session_permission_tier.go`、
  `application/core/permission_command_test.go`、`gui/headless_permission_test.go`、
  `sessionstore/session_meta.go` 等**已在工作区**（未提交）；CHANGELOG `[Unreleased]`
  已记两条。
- **P1-3（角色回合 / computer use 真实 API 冒烟）**：产品缺口已修 —— ADVISOR 裁决
  现在**在产出它的那一回合**就进可见聊天（过去要等下一次用户提交，探针结构上等不到）。
  变更范围 `application/core/{chat,goal_service,goal_coordinator,goal_team_recorder}.go`、
  `application/core/goal/{a2a,techleader}.go`、`application/core/view_state/coordinator.go`、
  `application/model/state.go`；探针证据三同步改为读可见裁决行（不再 15 分钟等待）。
  回归用例 `TestAdvisorVerdictVisibleInProducingTurn`（红→绿）、
  `TestMailboxPeekDoesNotConsume`；见
  `docs/devlog/2026-09-16-advisor-verdict-visible-immediately.md`。
- **仍未兑现**：P1-4（评审者可执行验证接地 —— 只读面里没有 `bash`）、
  P2-1/P2-2 的文档回填只做了一部分（`CHANGELOG.md`/`CHANGES.md` 已回填，
  `FOLLOWUP-role-turn-body.md` §4.2 未动）、P2-3/P2-4/P2-5/P2-6、P3 全部。
