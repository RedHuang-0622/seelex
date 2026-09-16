# Team Work 会话记录的数据流链路（main 与每个 teammate 各自那一份）

本文讲清一件事：**一次 team work 里，"会话记录"有几种形态、由谁写、由谁只读派生**，
以及 teammate 自己那份记录为什么从"它入伙那一回合"开始、面板上那些占位是怎么回事。

一句话版本：**主会话的消息文档是唯一真值**；teammate 的记录 = 用它的切点
（`join_seq_id` / `compact_ref.applied_seq`）从主会话真值里切出来的**只读视图**；
前端只渲染，任何一层都不许把它写回去。

---

## 0. 一张图看懂（`g` = goal 创建，`exec` = main/EXEC 回合）

```
回合号      1    2    3    4    5    6(g)   7(exec)  8
main 车道   1    2    3    4    5    6      7        (9)
tl   车道   -    -    -    -    -    6      7        8(tl)
                                   └── tl 的切点 = 5（它入伙那一刻的主会话尾 seq）
```

- tl 的 `-` 不是"内容丢了"，而是**那几行不在它的前缀匹配区间里**：它入伙之前
  的主会话对话不属于它的记录，面板以占位呈现，不冒充它记得的上下文。
- 6、7 是**共享回合**（tl 能看到的主会话行），8 是 tl 自己的行。
- 端点语义：切点 `C` 表示"记录从 `seq > C` 的行开始"；`C=5` ⇒ 记录自第 6 行起。

---

## 1. 真值 / 派生：谁写、谁只读

| 层 | 角色 | 写 | 读 |
| --- | --- | --- | --- |
| `sessionstore` | 唯一真值（消息文档 `message` + 角色 lifecycle + draft） | 写 | 读 |
| `sessionstore.readRoleSnapshot` | 观察面：`prefix_cut_seq` + 区间内外计数 | 不写 | 读 |
| `sessionstore.assembleRoleWire` | teammate 可见 wire（模型上下文） | 不写 | 读 |
| `application/core`（`Service`） | 装配/回合/治理编排 | 经由 port | 读 |
| `gui`（Bridge / headless） | 传输与命令入口 | 不写记录 | 读 |
| `gui/frontend/dist` | 渲染（Goal 面板栈帧、Team 面板车道） | **不写** | 读 |

**不变量**：前端与观察面都没有回写口。一旦把渲染结果推回后端，真值就变成前端派生物，
前缀与后端帧账本立刻不一致——这是本链路最需要守住的一条。

---

## 2. 链路一：一次回合怎么变成"行"

```
引擎回合（RunRoleTurn / 主回合）
  → draft（未同步行，落角色 draft 区）
  → commit（写入 message 文档，追加 seq，盖 role_name 归属）
  → 主会话 message 文档（真值）+ 角色 lifecycle（join_seq_id / compact_ref / floor）
```

- **draft 不进主文档**：`DraftRows` 只在角色自己的观察面与自身车道里可见；
  它一旦 commit 就成为主会话行、对所有 `seq > 切点` 的角色可见。
- **role_name 归属**：主会话行缺 `role_name` 时 `UnassignedRoleRows > 0`，
  存储层只报事实（`design_warnings`），不自动修补。

---

## 3. 链路二：teammate 自己那份记录怎么切出来（核心）

`sessionstore/role_session.go` 里两条路径用**同一条判据**：

```go
applied := lifecycle.JoinSeqID
if lifecycle.CompactRef != nil && lifecycle.CompactRef.AppliedSeq > applied {
    applied = lifecycle.CompactRef.AppliedSeq
}
rows := readRows(mainKey, applied+1, 0)      // 只取 seq > applied
```

| 形态 | 出口 | 用途 |
| --- | --- | --- |
| 模型上下文 | `assembleRoleWire`（`RoleWireSnapshot`） | 真的喂给该角色的 wire：compact 帧 + `seq > 切点` 的 main 行 + 自身 pending draft |
| 人看的观察面 | `readRoleSnapshot`（`RoleSnapshot`） | 面板/巡检：全部 main 行 + 自身行 + draft + **切点与区间计数** |

`RoleSnapshot` 上本次新增的三个字段就是"它自己那份记录"的读数：

| 字段 | 含义 |
| --- | --- |
| `prefix_cut_seq` | 该角色记录的起点 seq（判据同 `assembleRoleWire`）。**main 复用主会话本身，恒为 0、不受闸门** |
| `visible_main_rows` | `seq > cut` 的 main 行数（共享区间） |
| `outside_prefix_main_rows` | `seq <= cut` 的 main 行数（不在它记录里的那一段，前端渲染成占位） |

**帧归属校验（I18/I22）**：角色带 `compact_ref` 时，主会话的最新 compact 帧必须与它一致，
否则报错；角色**加入之前**发生的 compact 不属于它的可见区间——宁缺毋滥，
绝不把旧帧冒充成 join 后的共享内容。

### 3.1 切点是谁定的：装配那一刻的主会话尾 seq

`application/core/goal_service.go:ensureGoalAgentTeam` 在 goal 创建时自动装配 `goal-a2a` 团队，
`joinSeq` 取 `teamJoinSeqFor(sessionID)` = **装配那一刻主会话已提交的 message 尾 seq**：

```
主会话已提交 1..5  →  装配（回合 6 = goal 创建）  →  join_seq_id = 5
                                              ⇒ teammate 记录自第 6 行起
```

装配方（`agentteam.Factory.Materialize(mainSessionID, spec, joinSeq)`）把 `joinSeq`
写进每个角色会话的 lifecycle；`/goal begin → 自动装配` 与前端「装配团队」按钮都走这一个入口。
读不到主会话 head（宿主未装配会话存储、会话还没落行）时退回 `0` = 挂在主会话可见起点：
**宁可让 teammate 多看到一段，也不给它一个假切点。**

> 历史口径修正：此前该处硬编码 `joinSeq=0`，等价于"teammate 记录包含它入伙前的全部对话"。
> 现在改为按装配回合入伙；回归测试 `TestGoalBeginJoinsTeammatesAtGoalTurn` 钉住这一点
> （表里任何角色拿到 `join_seq_id=0` 即失败）。

---

## 4. 链路三：team work 前缀（环上那份上下文）

teammate 在环上发言时，需要的是**主会话的当前上下文**（用户刚说的话、main 刚做的事，
含尚未同步的 draft），而不是它的历史记录。这条链路与 §3 的记录是**两回事**：

```
主会话（history + 未同步 draft）→ noteTeamWorkPrefix（只读投影）→ 环/席位的 team work 前缀
```

- 前缀的**作者只有一处**：主会话上下文；teammate 记录不会反过来污染前缀。
- 前缀是"此刻的共享上下文"，记录是"它入伙以来的那份账"——两把尺子，别混用。

---

## 5. 链路四：观察面 → 传输 → 渲染

```
sessionstore.RoleSnapshot
  → internal/adapters/session_workspace_ports.go:roleSnapshot（DTO 映射）
  → application/core/role_session.go:Service.RoleSnapshot
  → gui/bridge.go:AgentTeamRoleSnapshot（或 headless：ReadRoleSessionRows / RoleSnapshot）
  → gui/frontend/dist/agent-team-view.js:renderRoleSessionDetail
       └─ renderRoleRecordTable  ← 一行一条车道（main / 自身）× 一列一个回合，占位 — = 不在前缀匹配区间
```

渲染出的结构（可被测试钉住）——**一张专用表格**：一行一条车道、一列一个回合（seq）。
main 车道显示主会话自己的回合；自身车道只显示"入伙之后的共享回合（`is-shared`）
+ 它自己的行/未同步 draft（`is-own`）"，入伙之前（或已被 compact 掉）的列是占位
`—`（`is-outside`）——不冒充它记得的上下文。

```html
<div class="role-record" data-record-cut="5" data-record-outside="5" data-record-visible="2" data-record-own="1">
  <div class="role-record-legend muted">前缀匹配自 seq 6 起：更早的 5 行不在它的记录里（占位 —）</div>
  <table class="excel-grid role-record-table" data-role-record-table>
    <thead><tr class="excel-head-row"><th class="role-record-lane-head">车道</th><th>1</th><th>5</th><th>6</th><th>7</th><th>8</th></tr></thead>
    <tbody>
      <tr class="role-record-lane is-main" data-lane="main"><th class="role-record-lane-name">main</th>
        <td class="role-record-cell is-main" data-seq="1">goal 之前 1</td>
        <td class="role-record-cell is-main" data-seq="5">goal 之前 5</td>
        <td class="role-record-cell is-main" data-seq="6">goal 这一回合</td>
        <td class="role-record-cell is-main" data-seq="7">exec 这一回合</td>
        <td class="role-record-cell is-empty" data-seq="8">·</td></tr>
      <tr class="role-record-lane is-own" data-lane="tl"><th class="role-record-lane-name" title="ADVISOR">tl</th>
        <td class="role-record-cell is-outside" data-seq="1" title="不在 ADVISOR 的前缀匹配区间">—</td>
        <td class="role-record-cell is-outside" data-seq="5" title="不在 ADVISOR 的前缀匹配区间">—</td>
        <td class="role-record-cell is-shared" data-seq="6" title="共享上下文（它能看到的 main 回合）">goal 这一回合</td>
        <td class="role-record-cell is-shared" data-seq="7" title="共享上下文（它能看到的 main 回合）">exec 这一回合</td>
        <td class="role-record-cell is-own" data-seq="8" title="ADVISOR 自己的回合">tl 这一回合</td></tr>
    </tbody>
  </table>
</div>
```

同一条渲染链路旁边还有两个只读投影（Goal 面板）：

| 投影 | 字段 | 为什么需要 |
| --- | --- | --- |
| goal 活动栈 | `goal_governance.stack[]`（栈底→栈顶，逐帧 title/statement/acceptance/progress） | 嵌套压栈后原来只有栈顶一帧，"栈上还有什么"看不见；现渲染成**专用表格**（`goal-stack-table`，一行一帧） |
| ADVISOR 进行中 | `goal_governance.in_flight` / `in_flight_chars` | b（ADVISOR）回合同步跑完才推一次状态，正文进行中必须靠只读快照才可见；回合结束即清空，权威正文是 `tl_directive` 行 |

---

## 6. 口径与不变量（改动前先读）

1. **端点语义**：切点 `C` ⇒ 记录自 `seq > C` 起。写测试时 `C=5` 要断言"第 6 行在记录里"。
2. **main 恒为 0**：主会话复用自己，不受 join 闸门——别给 main 也套切点。
3. **两种记录不等价**：`assembleRoleWire`（喂模型）与 `RoleSnapshot`（给人看）共用判据但内容不同；
   断言"模型能看到什么"用 wire，断言"面板显示什么"用 snapshot。
4. **compact 归属**：角色加入前的帧不属于它；缺 `compact_ref` 必须报错，不能静默兼容。
5. **单向**：渲染层没有任何回写记录/前缀的入口（Goal 面板与 Team 面板都是纯渲染件）。
6. **降级**：读不到 head 时 `joinSeq=0`（宽口径），不是"随便给一个数"。

---

## 7. 失败模式与诊断

| 症状 | 含义 | 看哪里 |
| --- | --- | --- |
| 面板 main 车道整段都是共享（没有占位） | teammate 被挂在主会话最开头（`join_seq_id=0`） | `RoleSnapshot.join_seq_id` / `prefix_cut_seq` |
| 记录区间过窄（丢回合） | 装配时传入的尾 seq 偏大，或 compact 后 `applied_seq` 抬高了切点 | `role lifecycle.compact_ref.applied_seq` |
| 报错 `role X missing compact_ref for main frame Y` | 角色记录引用了加入前的帧 | §3 帧归属校验 |
| `unassigned_role_rows > 0` | 主会话有行没盖 `role_name` | 主会话生产者（`design_warnings`） |
| ADVISOR 面板空但正在评审 | `in_flight` 快照未推或已清空 | `GoalGovernanceView.InFlight` |

---

## 8. 验证（可复现命令）

```bash
# 存储层：切点与区间计数
go test ./sessionstore/ -run TestRoleSnapshotMarksRowsOutsidePrefixMatch -count=1 -v

# 装配层：teammate 在 goal 创建那一回合入伙
go test ./application/core/ -run TestGoalBeginJoinsTeammatesAtGoalTurn -count=1 -v

# 前端：车道占位 / main 无占位
node --test gui/frontend/dist/agent-team-view.test.mjs

# 全量
go test ./... -count=1
node --test gui/frontend/dist/*.test.mjs
```

---

## 9. 代码索引

| 关注点 | 位置 |
| --- | --- |
| 切点与区间计数（观察面） | `sessionstore/role_session.go:readRoleSnapshot` |
| teammate 可见 wire（模型上下文） | `sessionstore/role_session.go:assembleRoleWire` |
| 装配写入 join 切点 | `application/core/agentteam/factory.go:Materialize`、`internal/adapters/agentteam_ports.go:EnsureRoleSession` |
| goal 创建时的 joinSeq | `application/core/goal_service.go:ensureGoalAgentTeam` / `teamJoinSeqFor` |
| 观察面出口 | `application/core/role_session.go:RoleSnapshot`、`sessionstore/role_session_router.go:RoleSnapshotWorkspace` |
| DTO | `application/contract/dto/rolesession.go`（`prefix_cut_seq` 等） |
| 映射 | `internal/adapters/session_workspace_ports.go:roleSnapshot` |
| 传输 | `gui/bridge.go:AgentTeamRoleSnapshot`、`gui/headless.go` |
| 渲染（车道 / 栈帧 / 进行中） | `gui/frontend/dist/agent-team-view.js`、`gui/frontend/dist/goal-stack-view.js` |
| 测试 | `sessionstore/role_session_test.go`、`application/core/goal_team_wiring_test.go`、`gui/frontend/dist/agent-team-view.test.mjs`、`gui/frontend/dist/goal-stack-view.test.mjs` |
