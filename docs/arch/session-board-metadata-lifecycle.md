# 会话粒度看板元数据与生命周期（goal 看板 / 团队看板）

> 状态：**已裁决（2026-10-02）**。三件事按推荐落地（§9）；另加一条硬要求——
> goal 看板必须把 **active seq 的 goal** 与 **history goals** 分开（§3.1）。
> 落笔依据：本仓既有存储布局（`sessionstore/module_heads.go` 的 `storageModule` 枚举 + `metadata/<module>.json` 原子 head 发布）与两块看板的现状投影路径（`runtime.goal_governance` / `runtime.teamwork_board`）。

## 0. 一句话

给两块看板各自补一份**会话粒度 JSON**（`metadata/board_goal.json` / `metadata/board_team.json`），
承载"这块看板**是什么、什么时候开的、什么时候关的**"以及一份快照；它唯一的作用是
**重启之后快照还能恢复**，并给看板一个**显式生命周期**（尤其：团队看板现在没有退场）。

## 1. 口径（先划边界，免得和第二份事实打架）

| | 本设计 | 不是本设计 |
|---|---|---|
| 是什么 | 会话粒度的**看板元数据 + 快照**；唯一用途：重启后的**快照恢复** | — |
| 与 `core/resume` 的关系 | 无关 | **不是** resume 的领域实现。resume 的七步（Locate→Decide→RepairParent→RestoreScene→InjectNote→Reexecute→Converge）是"**未完成工作**续跑"，对象是 subagent / role draft 这类执行单元；看板不是执行单元，没有"重跑"一说 |
| 与领域事实的关系 | 只读的**下游副本** | **不是**领域事实源：不回灌 `goal.Controller`、不参与 teamwork 计划判定、不进模型上下文、不进任何 gate/裁决 |
| 读侧优先级 | **活体投影优先**；JSON 只在活体投影给不出这块看板时兜底 | 不允许"JSON 与活体不一致时以 JSON 为准" |

一句话：**存储只是为了快照的恢复，在这里并不是恢复**（用户口径）——它是面板的存档，不是引擎的存档。

## 2. 现状盘点（哪些已经能恢复、哪些丢）

| 看板 | 内容 | 落点 | 重启后 | 备注 |
|---|---|---|---|---|
| goal | 目标正文 / 完成条件 / 非目标范围 / 打点流水 / 序 | `session/goal/active.jsonl` + `metadata/stack_goal.json`（第五栈） | ✅ 恢复（`Controller.Reload`） | 栈是活栈投影：finish/abort 弹栈即从栈文件消失 |
| goal | goal 生命周期审计 | `SessionContextRecord.GoalAudit`（context blob，append-only） | ✅ 恢复 | 与下面那份不是一回事 |
| goal | 终态帧的会话内审计（`goal.Controller.History`） | 进程内 | ❌ 丢 | 已知且是文档写明的边界 |
| goal | 评审过程（`round_steps`）/ 进行中正文 | 进程内（ADVISOR 回合现场） | ❌ 丢 | **已随用户裁决退场**（不再可见，也不打算恢复） |
| team | 计划（阶段 / 顺序 / 在编 / 里程碑） | `metadata/teamwork.json`（`moduleTeamwork` head，整份替换） | ✅ 恢复 | `stages[].depends_on` 是顺序的唯一事实 |
| team | 审计流水（plan / dispatch / join / milestone / retire） | `teamwork/events.jsonl`（append-only） | ✅ 恢复 | 看板只取最近 32 条 |
| team | **作业行**（handle / state / bytes / exit） | `jobs.Manager` 内存表 | ❌ **丢**（jobs I-4） | 现在靠 `stale` 位显式提示"句柄投影可能过期" |
| 两者 | **生命周期** | — | — | goal 看板随 active 帧消失（**已正确**）；**团队看板没有退场**：计划 head 一旦写入就永远在，团队散了看板也不走 |

**缺口就是两条**：① 团队看板的作业行与"最后一眼快照"重启即失；② 两块看板都没有**显式的开/关**记录
（团队看板尤其，收口之后无人关闭它；也没有"这块看板是什么时候没的"可查）。

## 3. 数据形状（BoardMeta）

```jsonc
{
  "schema_version": 1,
  "board_kind": "goal",              // goal | team
  "session_id": "s-...",
  "state": "active",                 // active | closed
  "seq": 7,                          // 本会话本看板的元数据序号（单调递增，只增不回改）
  "opened_at": 1760000000,           // unix 秒
  "updated_at": 1760000600,
  "closed_at": 0,                    // 0 = 未关闭
  "closed_reason": "",               // goal.finish | goal.abort | team.replan | team.close | ...
  "fingerprint": "sha256:...",       // 快照内容指纹（写入节流用，见 §7）
  "snapshot": { /* 与前端 DTO 同形的看板投影；形状由各自 dto 定义 */ }
}
```

- `snapshot` **与下发前端的投影同形**（goal：`dto.GoalGovernanceView` + 栈；team：`dto.TeamworkBoardView`），
  这样"恢复"就是把存档原样喂给同一个渲染件——不引入第二套形状。
- `state` / `seq` / `*_at` / `closed_reason` 是**生命周期字段**：它们是这份 JSON 存在的第一理由
  （快照是第二理由）。
- 未知字段/未知 `schema_version` → 读侧**不猜**：当成"没有存档"（看板退场），不报错、不半读。

## 3.1 goal 看板的载荷：active seq 的 goal ≠ history goals

这是本设计的硬要求（用户裁决 2026-10-02），也是 goal 域现状留下的真空：

- goal 域的活动栈是**活栈**——`finish`/`abort` 弹栈即从 `session/goal/active.jsonl` 消失；
  终态帧的会话内审计在 `goal.Controller.History`（**进程内态**，重启即失）。
  于是重启之后"这个会话跑过哪些目标、分别怎么结束的"完全不可追。
- `SessionContextRecord.GoalAudit`（持久、append-only）记的是**生命周期迁移**
  （begin/update/finish/abort/restore），不是看板载荷；看板不能直接拿它当 history 行渲染。

因此 goal 存档的载荷**分开两块**，读侧不许把它们混成一个列表：

```jsonc
{
  "board_kind": "goal",
  "state": "active",                     // 有 active 帧 = active；栈空 = closed
  "active": {                            // ← "active seq 的 goal"：当前治理中的那一帧
    "goal_id": "g-7",                    //    就是这个 seq（goal 自己的编号，不是打点条数、不是栈位置）
    "title": "…", "status": "active",
    "statement": "…", "acceptance": [...], "out_of_scope": [...],
    "progress_all": [ ... ],             //    完整打点流水（详情面用；有界）
    "created_at": …, "updated_at": …
  },
  "history": [                           // ← "history goals"：已结束的目标，append-only
    { "goal_id": "g-6", "title": "…", "status": "completed",
      "closed_at": …, "closed_reason": "goal.finish", "progress_count": 5 },
    { "goal_id": "g-5", "title": "…", "status": "aborted",
      "closed_at": …, "closed_reason": "goal.abort",  "progress_count": 2 }
  ]
}
```

规则：

- `active` 为 `null` ⟺ `state == "closed"`（没有治理中的目标）。嵌套压栈时 `active` 只指**栈顶那一帧**；
  栈下被压栈的目标属于活动栈（仍在 `runtime.goal_governance.stack` 里），不进 `history`。
- `history` **只追加、不重写、不重排**：一条 goal 从 `active` 迁到 `history` 是**一次单向迁移**，
  迁移那一刻写死 `status` / `closed_at` / `closed_reason`（这三样只有在弹栈那一刻才知道）。
- 存档里的 `history` 与 `active` **不共享同一份 id**：同一个 `goal_id` 不得同时出现在两处；
  出现即视为存档损坏（读侧不猜，见 §8 I6）。
- 重启后：`active` 从活体栈现算（栈是持久事实），`history` 只能来自存档——这正是这份 JSON 的第一价值。
- 展示面（前端/TUI）必须**分开显示**：看板主体只写 active 的那个 seq；history 进"历史目标"一节
  （或详情面），不得把 history 混进看板卡片当"当前目标"。

## 4. 落点与发布

沿用既有布局，**不新造机制**：

- 两个新模块枚举（`sessionstore/module_heads.go`）：`moduleBoardGoal = "board_goal"`、
  `moduleBoardTeam = "board_team"` → 文件 `metadata/board_goal.json` / `metadata/board_team.json`。
  **两个模块而不是一个**：两块看板由不同子系统写（goal 域 / teamwork 域），S16 拆栈 head 的理由
  在这里同样成立——共用一个模块就是给它们造一个共享串行点。
- 发布走既有的原子 head 通道（`commitModuleHead` / `readModuleHeadPayload[T]`：temp + rename、
  checksum 校验、失败不动已发布 head）。
- 模块锁必须各加一个 `case`（`mutexFor` 对未映射枚举**panic**，且 `TestModuleLocksAreDistinct`
  遍历枚举钉住"禁止 default 别名"）——加枚举忘了加 case 会在测试里立刻炸，不会静默别名成别人的锁。
- 读写面按 `sessionstore/teamwork.go`（`TeamworkRepository`）的既定形态提供
  （`BoardRepository.ReadBoardMeta / WriteBoardMeta`），`Key` 口径与其它模块一致。

## 5. 生命周期

```text
        open                    update*                  close
  ────────────────►  active  ────────────────►  active  ────────────────►  closed
   (看板首次出现)      (每次投影变化)              (收口/解散/被新版本取代)
```

| 看板 | open | update | close |
|---|---|---|---|
| goal | 目标上线（`goal_begin` 压栈） | 打点 / 裁决 / 状态迁移 | `goal.finish` / `goal.abort`（弹栈）→ `closed_reason` 记录是哪一种 |
| team | `team_plan` 首次写入计划 | `team_dispatch` / `team_join` / `team_milestone` / `team_retire` | **待裁决**（见 §9 第 2 条） |

`closed` 之后的读侧语义（两块一致）：**看板不出现**（面板整块退场，不留空壳）；存档保留为历史，
回答"这块看板什么时候开的、什么时候没的、为什么"。

`update` 一律**不改** `opened_at`、**只追加** `seq`；`close` 之后再 `update` 是错误路径（丢弃并记一行日志，
不复活看板——复活必须走一次新的 open）。

## 6. 恢复路径

1. 会话加载 / 首次采集快照时，活体投影**照旧**先算（goal 栈 / 团队计划 + 审计 + 作业表）。
2. 活体给得出看板 → 用它，并按 §7 刷新存档；**存档不参与这一路**。
3. 活体给不出看板（重启后作业表空、或该子系统尚未装配）→ 读存档：
   - `state == active` → 用 `snapshot` 出看板，标 `recovered: true`（前端/TUI 显示"自快照恢复"），
     其中的**作业行**同时按老口径标 stale（句柄只存在于上一个进程）；
   - `state == closed` 或没有存档 → **看板不出现**（与"没有计划就不留空壳"同口径）。
4. 活体重新可用后，下一次采集自然覆盖回活体结果（存档被刷新，`recovered` 撤销）。

失败语义：读存档失败 = 没有存档（看板退场，记一行日志）；**不**把存储错误渗进会话快照，
也**不**因此阻断任何控制流。

## 7. 写入路径与节流

- 写入点是**看板投影的产出侧**（不是渲染侧）：投影算完 → 算 `fingerprint` → 与存档里的指纹比对 →
  变了才写。这样"高频会话快照"不会变成"高频磁盘写"（无变化 = 零 IO）。
- 写入失败**不改变控制流**：看板照常下发，只记一行日志（与 jobs 事件投影同一纪律）。
- 生命周期迁移（open/close）**必须落盘成功才算数**：close 写不进去就不能把看板当关闭
  （否则重启后它会作为 active 复活）。这是本设计里唯一一处"写失败要影响行为"的地方。

## 8. 不变式与验收矩阵

不变式（每条都要有测试钉住）：

- I1 **单向**：任何领域代码都不得读 BoardMeta 做判定（只允许看板投影的兜底路径读）。
- I2 **活体优先**：活体与存档同时可用时，输出必须等于活体结果（存档只补空，不改事实）。
- I3 **枚举—锁一一对应**：新模块在 `mutexFor` 有独立 case（`TestModuleLocksAreDistinct` 扩展）。
- I4 **无变化不写盘**：连续两次相同投影只产生一次写入（可用文件 mtime/size 或统计计数验证）。
- I5 **close 不复活**：closed 存档 + 活体给不出看板 → 面板不出现。
- I6 **schema 不猜**：未知 `schema_version` / 损坏 JSON → 视为无存档。
- I7 **不进模型上下文**：BoardMeta 不出现在任何 prompt 装配面（与"第五栈不入上下文"同口径）。

验收矩阵（每条一个用例）：

| 场景 | 断言 |
|---|---|
| 首次 open | 文件出现，`state=active`，`seq=1`，`closed_at=0` |
| 投影变化 | `seq` 递增、`updated_at` 变、`opened_at` **不变** |
| 投影不变 | 不产生第二次写入（I4） |
| 重启 + 活体可用 | 输出 = 活体结果（I2），存档被刷新 |
| 重启 + 活体给不出（作业表空） | 输出 = 存档快照，标 `recovered`，作业行标 stale |
| close 后重启 | 面板不出现（I5），存档仍在且 `closed_reason` 可读 |
| 存档损坏 / 未知版本 | 面板不出现、不报错（I6） |
| 写盘失败（只读目录） | 看板照常下发、控制流不受影响；但 close 失败不算关闭 |
| goal 看板：收口/中止 | `state=closed`，`closed_reason=goal.finish|goal.abort` |

## 9. 已裁决（2026-10-02）

**结论：三件事按推荐，另加第 4 条。**

1. `snapshot` = **整份看板投影 + 作业行**（团队看板）；goal 看板的载荷按 §3.1 分成 `active` / `history`。
2. 团队看板 close = **两条都认**：(a) `team_plan` 写入新版本时把**上一版本**记为 closed；(b) 显式收口。
3. close 之后**存档保留**（`state=closed` + `closed_reason` + 时间），读侧照样不显示它。
4. **goal 看板必须区分 active seq 的 goal 与 history goals**（§3.1）。

下面是原文保留的三个备选（作为决策记录）。

### 原文：三个备选（各带推荐）

**① `snapshot` 装什么？**
- **推荐：整份看板投影 + 作业行**。理由："快照恢复"要是真的快照；且作业行是唯一重启必丢的事实。
  代价是存在一份**可漂移的副本**——用 I1/I2 把它钉成"只兜底、不参与判定"。
- 备选：只存**不可重算的部分**（作业行 + 生命周期字段），其余重启后现算。零漂移风险，
  但严格说就不是"整份快照"，而是"快照补丁"。

**② 团队看板在什么条件下 close？**
- **推荐：两条都认**——(a) `team_plan` 写入新版本时把**上一版本**记为 closed
  （版本递进 = 天然的看板生命周期，不需要新面）；(b) 显式收口（leader 判定收工）。
- 备选 A：只认显式关闭（要加一个 `team_close` 面）。
- 备选 B：派生关闭（全部阶段 done + 里程碑全 done 就自动关）——**不推荐**：这是从作业状态
  反推"团队收工了"，属于把未知当已知（本仓一贯不这么干）。

**③ close 之后的存档留不留？**
- **推荐：留**（`state=closed` + `closed_reason` + 时间），这样"这块看板什么时候没的、为什么"
  查得到；读侧照样不显示它。
- 备选：直接删文件（省一点盘，代价是历史不可追）。

## 10. 工作量分解（裁决后开工）

1. `sessionstore`：两个模块枚举 + 两个锁 + `BoardRepository` 读写面 + 用例（含 I3/I6）。
2. `dto`：`BoardMeta` 形状（或直接复用两块看板的既有视图） + 指纹口径。
3. 投影侧：goal 看板 / 团队看板的产出点写存档（节流 + 生命周期迁移）+ 兜底读路径 + 用例（I1/I2/I4/I5）。
4. 前端/TUI：`recovered` 标记的可见面（一句话，与 stale 同形）。
5. 守卫：I1（领域侧不得读 BoardMeta，静态断言）+ 一条接线守卫（存档路径真的被读写）。

---

## 11. 落地状态（2026-10-02，同批实现并验收）

### 落点

- **存储面**：`sessionstore/module_heads.go` 新增 `moduleBoardGoal` / `moduleBoardTeam` + 两把独立锁
  （`mutexFor` 各一 case）；`sessionstore/board.go`（新）：`BoardLifecycle` / `GoalBoardActive` /
  `GoalBoardHistory` / `GoalBoardMeta`（active/history 分离）/ `TeamBoardMeta`（视图快照原样搬运）+
  `ValidateGoalBoardMeta` / `ValidateTeamBoardMeta` + `BoardRepository`（带 Key 的机械面）与
  `BoardsForSession`（Key 烘焙进实现，领域侧只知道 sessionID）。
- **goal 写侧**：`application/core/goal/board_archive.go`（新）：`refreshGoalBoard`（Save 的唯一必经点）
  + `refreshBoardAfterAudit`（终态审计落账后，权威 Status/At/Reason）+ `planGoalBoard`（生命周期推进
  + 指纹节流）+ `mergeBoardHistory`（并集去重、只追加、id 复用让位）+ `closedInfo` / `activeFrame`；
  `sessionstore_store.go` 加存档面与 `Save` 末的扇出。
- **goal 读侧**：`application/core/goal_coordinator.go`：活体给不出看板时用存档 active 帧重建并标
  `Recovered`（`recoveredGoalView`），`History` 与活体栈正交下发（`goalHistoryViews`）；存档读失败 /
  损坏 / 缺失一律按"没有存档"降级。
- **team 写侧/读侧**：`seelebridge/runtime_teamwork_board_archive.go`（新）：`archiveTeamBoard`
  （team_* 成功后用**与下发同一条组装路径** `buildTeamworkBoardView` 生成载荷）+ `nextTeamBoardMeta`
  （新开 / 版本递进先关旧版 `team.replan` / 同版本 seq+1 / 节流）+ `readTeamBoardArchive`
  （只恢复 `state=active`，标 `Recovered=true` + `Stale=true`）。
- **可见面**：团队看板 `recovered` chip（与 `stale` 同屏，二者不互斥）；「目标」面板「历史目标」一节
  （账本与看板主体分开，§3.1）+ `recovered` 标记；TUI 同源同一份投影（`tui/goalteam.go`）。
- **守卫**：`application/core/goal/board_archive_boundary_test.go`（I1 静态断言：只有
  `board_archive.go` / `sessionstore_store.go` 可提到存档类型）+ `seelebridge` 的
  `TestEveryTeamworkMutationHandlerRefreshesBoardArchive`（5 个 team_* 动作都必须"失效缓存 + 刷存档"）
  + `gui/team_board_wiring_test.go` 的 `TestEmbeddedBoardArchiveVisibilityWiring`（recovered 与
  历史目标的可见面真的接在活体前端上）。

### 回归证据

- `go build ./...` exit 0；`go vet ./...` exit 0。
- `go test -count=1 ./...` 全绿（sessionstore 75.9s / application/core 32.7s / seelebridge 47.8s / tui）。
- `node --test gui/frontend/dist/*.test.mjs` → tests 613 / pass 613 / fail 0。

### 边界（如实标注）

1. **收口后的账本没有常驻入口**：面板可见性仍归 goal 状态机（没有 active 帧就整块退场，含账本）。
   于是"上一轮目标怎么收口的"要在**有活跃目标时**（GUI/TUI 面板里的「历史目标」一节）才看得到；
   收口后会话空闲时两端的账本都不露面。要不要开一个常驻入口是**未决口径**，本轮没有替用户决定。
2. **`progress_count` 只在"上一次刷新时它还在栈顶"时判得出来**（审计条目本身不承载打点数），
   其余情况为 0——不猜。
3. **存档不是领域事实源**：不进 gate / 裁决 / 模型上下文；`g` 编号复用（会话重启后新 goal 又拿到
   `g-1`）时账本里同 id 的旧条目让位，避免同一 `goal_id` 同时出现在 active 与 history（存档不变式）。
4. **`team_join` 的接线是本次补的**：动作清单（契约 §5）本来就含它，此前漏了"失效缓存 + 刷存档"
   （面板要等到下一个 team_* 才看到这次汇合），现在由守卫钉住。
5. **写存档失败只在 close 方向影响行为**：`goal.finish/abort` 时存档写不进去必须上报（"这次关闭
   没有被持久记账"）；active 方向与 team 方向一律尽力而为、不阻断控制流。
