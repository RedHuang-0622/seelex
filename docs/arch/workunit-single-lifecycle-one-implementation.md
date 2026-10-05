# 一件活的生命周期：一份实现（父），两个注册点（子）

状态：**本次改造的设计依据**（leader 撰写，实现按本文照做；改动本文 = 改口径，必须显式记录）。
适用范围：`seelebridge` 里的工作单元生命周期。**不**含 worktree 管理细节、会话 add/restart/over、
子进程树管理的完整提取——那些先做只读盘点（见 §6）。
配套：**父（端口）清单与装配矩阵 + 两步计划**（① 只定作业端口 / ② 两张作业表合一）见
`workunit-ports-and-assembly.md`；现状锚点见 `workunit-duplication-inventory.md`。

## 1. 目标（一句话）

一件活的生命周期（建现场 → 收尾 → 回收 → 重启接着做）**只有一份实现**；
subagent 与 teammate 只做**在父实现上注册 + 转发**，转发时把自己的**会话路径**当入参传进去。
层与层之间唯一允许的差别是"我是谁、我的会话在哪、我什么时候回收、我要装配什么"。

## 2. 现状：为什么必须这么做（本次诊断的事实）

现在不是"一份实现 + 两个注册点"，而是**两份各自独立的实现**：

| 同一件事 | subagent 走的路 | teammate 走的路 |
|---|---|---|
| 建现场 | `beginNodeWorktree`（`runtime_plan.go:145`） | `BindWorkspace`（`runtime_teamwork_items.go:58`） |
| 收尾合并 | `finishNodeWorktree`（`runtime_plan.go:208`） | `Coordinator.SettleWorkItemOutcome`（`teamwork/items.go`） |
| 重启后登记现场 | 有：`NoteWorktree` 调用点全仓只有 `beginNodeWorktree` 一处 | 没有：得另从团队账本 + 绑定认领一次（`runtime_subagent_recovery.go:139`） |
| 生命周期方法 | `nodeWorkUnit`（`workunit_node.go`，5 个方法各自实现） | `teamUnit`（`workunit_team.go`，5 个方法各自实现） |

并且 `workunit.Unit` 这个接口**在生产代码里没有任何调用点**——它只出现在注释、编译期断言和测试里；
连"什么时候回收"那个动作 `AfterFinish` 的调用点也全在测试里。所谓"统一契约"因此只是**形状统一**，
不是**实现统一**。

**直接推论**：此前列出的"合同没写清"里，有四项（已经收过尾怎么回答、传进来的合并结果算不算数、
重启接着做是会话级还是单件、拆的时候要不要收作业）**只是"两个实现各自为政"的副产物**——
一份实现的话它们根本不会成为问题。把它们写成合同，等于把病态固化。**它们靠合并实现消除，不靠补合同。**
只有两项是真缺口（策略没进接口、说明非空没写），随本次一并补。

## 3. 先定接口，再装配父实现（依赖倒置）

**顺序反过来**：接口先落在契约包 `seelebridge/workunit`（零依赖，不 import teamwork / worktree / session），
**父实现只是这个接口的一个实现**，在 `new` 的时候装配进去；两个注册点只拿到接口，拿不到具体类型。

```
type Lifecycle interface {            // 父实现的契约面：调用方依赖它，不依赖具体实现
    Begin(ctx, u Unit) (Scene, error)
    Finish(ctx, u Unit, result Result, mergeErr error) (Outcome, error)
    Reclaim(ctx, u Unit) error        // 析构：结束这一件活的现场 + 会话
    Recover(ctx, u Unit) (Resume, error)
    AlreadySettled(ctx, u Unit) (bool, error)
    Notice(outcome Outcome) string
}

type Unit interface {                 // 层的读数：只有身份与策略，没有逻辑
    Kind() Kind
    ID() string
    SessionPath() string              // ★ 转发时传进去的"自己的会话路径"
    Policy() FinishPolicy
    Owns() Ownership                  // 归属标注（谁的活、哪一件），不泄漏 team 类型
}
```

装配：`lifecycleHost`（在 `seelebridge`，唯一实现，端口字段**不导出**）实现 `Lifecycle`；
各注册点 `new` 时注入它，只持有 `workunit.Lifecycle` + 自己的 `Unit` 读数。
好处：① 将来两层真要不同实现 → 再写一个 `Lifecycle` 实现、改装配处一行，不动调用面；
② 顺手挡住循环依赖——契约包谁都不 import，实现包 import 契约包，调用方 import 契约包。
`teamwork` / `worktree` / `session` 的具体类型只允许出现在实现包内部；归属用契约自己的小结构表达。

```
lifecycleHost            // 父：唯一实现，持有全部端口
  ├─ ledger   workunit.SessionLedger   // 快照读/写（结构上就是 *sessionstore.NodeSessionStore）
  ├─ spaces   *worktree.WorktreeManager // 现场：fork / merge / rebase / 主工作区保护（一份）
  ├─ jobs     jobs.Manager              // 作业与子进程树：Reclaim(ctx, scope)
  ├─ bindings 团队绑定账本 + 计划存储     // teammate 归属与终态
  └─ layer    lifecycleLayer            // 层的窄 hook（只放"差异"，不放实现）

lifecycleLayer           // 层差异：只有读数，没有逻辑
  ├─ Kind() workunit.Kind
  ├─ NodeID() string                  // subagent = 节点 id；teammate = <role>-<itemID>
  ├─ SessionPath() string             // ★ 转发时传进去的"自己的会话路径"
  ├─ Policy() workunit.FinishPolicy   // Immediate / AtTeamClose
  └─ Team() (teamID, itemID string)   // 归属标注（可为空）
```

父实现提供且**只有它**提供：`Begin / Finish / Reclaim / Recover / AlreadySettled / Notice 生成`。
两层结构体只留三个读数（`Kind` / `NodeID` / `SessionPath`）+ 策略，方法体一律是
`return u.host.Xxx(ctx, u.layer)`——**方法体里不许出现 store / worktree / jobs / 账本的写入**。

父实现内部的**封装**保证"不会再有第二份实现"：端口字段**不导出**，只有父自己能拿到；
子层结构体只持有 `*lifecycleHost` 与自己的 `lifecycleLayer`，拿不到底层端口。

## 4. 重启接着做（唯一一条路）

形状（按最初设计）：**读快照 → 交给 leader 下令「继续 / 结束」→ 同一条路径处理两种回答**。

1. 父读快照：按**会话路径**把该会话的全部单元记录读回来（`Recover` 的粒度是会话级）。
2. 父判定"记录说在跑、本进程已无执行面" → 记进 `Resume.Interrupted`（在跑词表只有一份：
   `workunit.InFlight`）。
3. 父把"接着做"的说明注入回去：**同一族前缀 + 同一个注入身份**（`workunit.RecoveryNote`），
   同一条记录重复读**只注入一次**。
4. leader（用户那张令）决定继续或结束：继续 = 现场认领后接着跑；结束 = 走同一条回收路径。
   两层都走这一条，差别只有 `SessionPath()`。

## 5. "真的只有一份实现"的证据（不是用例全绿）

1. **删除清单**：交付必须列出被删掉的重复实现（文件:行 → 删除），而不是新加一层包装。
2. **结构证据**：父的端口字段不导出；子层文件里搜不到 store / worktree / jobs / 账本写入。
3. **用例证据**：跨层用例断言两层同一份输入 → 同一份结论（含"已经收过尾"的重入）；
   外加一条"子层结构体只有三个读数 + 策略"的形状断言。
4. **零调用点也要修**：`workunit.Unit` 与 `AfterFinish` 必须出现**生产调用点**（父实现内部按接口
   统一驱动），否则"统一"仍然只是纸面。

## 6. 本次范围外（先只读盘点，落待办）

- worktree 管理：fork / merge / rebase / **主工作区保护**（`worktree_manager.go:430-505` 已是一份，
  但两条调用链各走各的入口）；
- 会话管理：add / restart / over（`ResetSession` 等）；
- 后台子进程树管理（`security/process_tree_windows.go` 一个作业对象；
  谁在什么时机回收、两条链是否都走到）。
→ 本轮只盘点并写清"现状几份、该归到哪个父实现"，**不动实现**（大提取是后续独立一波）。

## 7. 工程纪律（派活时逐条带上）

1. **动手前先找既有实现**：同一件事在仓库里是否已有函数/类型；有就调用，没有才新增。
2. **只并"本质重复"**：判据相同 + 语义相同 + 会一起漂移并伤到人 → 合并。
   只是长得像的（例：两处都起进程树，但超时/取消语义不同）**不合并**，但要写清"为什么像却不并"；
   偶然重复（同一写法落在两个不同概念上）不算缺陷，不为它动刀。
3. **一份判据 = 一处函数**：同一判据、同一语义在第二处出现，即视为缺陷（不是"更清楚"）。
4. **交付必须列"删掉了哪些重复实现"**；只加不改的交付一律退回。
4. **证据是"只剩一份实现"**，不是"用例全绿"——用例证明不了"没有第二份"。
5. **不许趁没人 review 就自造形状**：与本文冲突 = 先回来改本文，不许在实现里私下另立一套。

## 8. workunit = jobs 之下 subagent 与 teammate 的**交集**

- 交集（两层真正共有的东西）落成 `workunit`：生命周期方法（`Begin/Finish/Reclaim/Recover`）、
  **析构**（结束这一件活的现场 + 会话）、策略（`Immediate` / `AtTeamClose`）、收尾分类、恢复说明、在跑词表。
- 这些做成**公有函数/接口**（`Reclaim` = 析构、`ClassifyFinish`、`RecoveryNote`、`InFlight`）。
- **策略不是实现，只是调用点**：不同策略 = 不同地方去调**同一个**析构函数
  （subagent 收尾当场调；teammate 留到整队收口调）。
- 交集之外的（调度、装配、账本、屏障）不属于 workunit，留在各自层。

## 9. 读面（进度反馈）也复用：workunit 做后端，前端同一套设计

- 样板是 subagent 的进度反馈（阶段 / 事件 / 读数那条链）——它已经"让人看得懂"。
- teammate 目前没有同等读面。把**读**（在跑与否、阶段、结论、现场）做成 workunit 的公有读面（后端），
  UI 侧用**同一套 hook / 同一套读法**消费：改一处，两处一起变，不用逐个组件改。
- 本轮先只读勘定（现状 + 接口形状建议），不动前端实现。

## 10. 红灯先行的两处已知缺陷

1. **merge back 把先合回来的那件"踢走"**：旧版 subagent 不由自己 merge 回主分支；后续的 merge back
   会把先前那件踢成另一个分支。→ **先写红灯**（两个单元先后 merge back：断言主分支同时含两件产出、
   先者的分支指针与现场不被改），红灯复现后再修，绿灯收。
2. **现场清理幂等漂移**：`cleanup`（非幂等）与 `CleanupWorktree`（幂等）语义不一致（见盘点 §1 末行）——
   按同一"红灯先行"口径处理。
