# Goal 域 + Headless 调教接口原型（P0 scope）

> 状态：原型设计与实现说明（Prototype）
> 日期：2026-09-07
> 配套：`architecture.md`（总体）、`design.md`（详细设计/决策清单）、
> `techleader-mvp.md`（Part II：TechLeader A2A + 上下文共享 MVP）。
> 本文按用户要求：**自顶而下**——先定义 HeadlessGUI 调教接口，在接口下完成 goal 域内容，
> 最后在「前端投影 + 测试用例」中收束，并给出可运行测试命令。

---

## 1. 原型目标（一句话）

在不动现有运行链路的前提下，交付一个**可跑测试的 goal 域原型**：
`application/core/goal` 包实现 Part I（goal 对象/栈/状态机/存储/投影/帧），
并以 `gui/headless.go` 同款 RPC+事件范式暴露 **goal Headless 控制面**
（`/rpc` 的 `goal_begin/goal_update/goal_finish/goal_abort/goal_status/goal_projection`
与 `/events` 事件流）——外部驱动（冒烟脚本 / 未来前端 / 测试）用同一接口调教 goal 域；
真实工作全部走 domain，接口不新增业务状态机（对齐 `gui/headless.go` 的设计理念）。

## 2. 架构分层（自顶而下）

```text
┌─ Headless 调教接口（HTTP，回环可绑）─────────────────────────────┐
│  /healthz /rpc{method,args} /events（事件流，快照语义）           │  ← gui/headless.go 同款
│  goal.Headless{Server, Client}                                   │
├─ 域控制器（接口下的实现）────────────────────────────────────────┤
│  goal.Controller                                                  │
│    begin / update / finish / abort / status / projection / frame │
│    栈（LIFO，深度默认1=会话单例；嵌套时下层自动 paused/恢复 active）│
│    状态机（active→…→completed/failed/aborted/waiting_human）      │
│    事件订阅（带 dropped 计数的有界通道，快照语义投影随事件携带）    │
├─ 存储（Store 接口）───────────────────────────────────────────────┤
│  MemoryStore / JSONFileStore（原子写）                            │
├─ 前端收束契约（投影 JSON）────────────────────────────────────────┤
│  Projection{Active, Goals} = 未来的 RuntimeVisibilityProjection   │
│  .Goals / ParentEvidenceProjection.Goal 的直接数据源              │
└────────────────────────────────────────────────────────────────────┘
```

与生产接线位（原型不接，P0-wiring 后续做）：
- `gui/headless.go:dispatch`：新增一行 `goal.<method>` → `goal.Controller`（controller 由 application 按会话持有）；
- `gui/Application`（`gui/bridge.go`）增 `GoalBegin/Update/Finish/Status` 薄方法；
- `application/contract/dto/projection.go`：`RuntimeVisibilityProjection` 增 `Goals []GoalView`
  （原型 `goal.Projection` JSON 字段已对齐，见 §5）；
- `seelebridge/tools/policy.go`：`isGoalTool` 门控（与 `isPlanTool` 同 gate）。

## 3. Headless 调教接口契约

### 3.1 传输（HTTP JSON，与 gui/headless.go 同 wire shape）

```text
POST /rpc
  req : {"method":"goal_begin","args":[{...BeginRequest}]}
  res : {"ok":true,"result":{...}} | {"ok":false,"error":"goal: ..."}
GET  /events        事件流（每行一个 JSON goal.Event；订阅有界，溢出计数不丢语义由
                    Subscription.Dropped 上报，事件本身携带全量 Projection → 后发覆盖安全）
GET  /healthz      {"ok":true}
```

### 3.2 方法清单（映射 design.md §3.4 工具族）

| method | 参数 | 返回 | 语义 |
|---|---|---|---|
| goal_begin | BeginRequest | GoalRecord | 注册+压栈（同标题 active 幂等返回） |
| goal_update | UpdateRequest | GoalRecord | 更新栈顶（仅 active） |
| goal_finish | FinishRequest | GoalRecord | 栈顶 completed + 弹栈（History 留审计） |
| goal_abort | FinishRequest | GoalRecord | 栈顶 aborted + 弹栈 |
| goal_status | 无 | StatusView | 栈 + 栈顶全量（含 acceptance/progress） |
| goal_projection | 无 | Projection | 前端投影（栈顶+下层紧凑视图） |

### 3.3 事件模型

```go
type Event struct {
	Kind       EventKind  // goal.begin|goal.update|goal.finish|goal.abort|goal.status
	At         int64
	GoalID     string
	Status     Status
	Projection Projection // 每次事件携带全量投影：前端后发覆盖安全（无逐事件差分）
}
```

## 4. 域控制器（接口下的实现要点）

- **状态机**（design §3.3 P0 子集）：active → completed（finish）/ aborted（abort）；等待/越权状态
  `waiting_human` 预留在枚举与终态判定（`IsTerminal`）；嵌套时下层 auto paused，弹栈 auto 恢复 active。
- **会话单例**：`Options.Depth` 默认 1 → 栈满 `goal_begin` 报 `ErrStackFull`（报错信息带「栈深/会话单例」）；
  放开嵌套 = `Depth>1`（原型已支持，供 D2 验证）。
- **预算**：`GoalBudget{MaxLoops,MaxTokens,TLTokensShare}` 随 goal 记录；0=无限（TL share 默认 20）。
- **存储**：`Store{Load,Save}` 接口；JSON 原子写（temp+rename）；每次变更后持久化当前栈（弹栈即从栈文件消失，
  finish 审计在 `History()` 内存态）。
- **Goal 帧**：`Controller.Frame()` 输出栈顶渲染文本（title/status/statement/acceptance/progress）——
  未来 Part II TechLeader 每轮嵌入 goal 内容、goal 帧注入的前端素材；空栈返回 ""。
- **并发**：`Controller.mu` 串行变更 + 投影拷贝；订阅通道有界、溢出计数（发送不阻塞）。

## 5. 前端收束契约（投影字段对齐）

```jsonc
// goal.Projection —— 与未来 dto.GoalView / RuntimeVisibilityProjection.Goals 字段一一对应
{
  "active": { "id":"g-1", "title":"发布 v1", "status":"active", "updated_at": 1725... },
  "goals":  [ /* bottom→top 全栈紧凑视图；active == 末元素 */ ]
}
```

工作台规则（design §3.6）：`goals[last]` = 主目标；`goals[:last]` = 下层（paused）状态；
空栈（finish 弹栈后）= 无 goal 会话（回退现有行为）。Headless `/events` 让前端/冒烟驱动
在每次 goal 变更后拿到同一份投影，无需轮询。

## 6. 测试收束与运行

- `application/core/goal/stack_test.go`：栈 LIFO/弹栈/清空。
- `application/core/goal/controller_test.go`：状态机全转移、深度单例拒绝嵌套、嵌套恢复、
  update 仅 active、幂等 begin、finish 弹栈+History、JSON 存储 roundtrip（重启恢复 active goal）、
  Frame 渲染、事件顺序与 dropped 计数、并发（-race）。
- `application/core/goal/headless_test.go`：httptest 起 Headless 控制面 → healthz →
  goal_begin/status/projection/finish 全链 JSON-RPC → 错误透传（栈满）→ `/events` 收到 begin 事件行。

运行：

```text
go test ./application/core/goal/... -count=1 -race
```

## 7. 范围声明（Out，本原型不做）

TechLeader A2A / CSP channel（Part II）；`seelebridge/tools` goal 工具门控；state blob 五栈与
`SessionContextStore` schema bump；`gui.Bridge`/`Application`/`dto.RuntimeVisibilityProjection`
真实接线（落点见 §2）；`main.go` headless 装配。这些在 P0-wiring / P1 承接。
