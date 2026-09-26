# Seelex 工具调用并发与"子进程调用系"详细设计

> 状态：设计稿（**v3 修订**，待评审）
> 范围：**契约落实（P0，§A）**、契约下的实现重构（P1，§B）、契约进 seele 与 loop 并发（P2 feat，§C）；loop 内并发的实现级设计降为备选路径（§1–§4）
> 前置结论：本设计**不新增「串行/并行」字段**，而是从已有的 `ToolMeta.Groups` 派生
> 落地顺序与判据见 `docs/tool_calling_step0_contracts.md`（打点表）；测试用例见 `docs/subprocess_contract_testcases.md`

---

## 修订说明：v3（优先级重排 + 四处纠正）

- **v1**：并发来自 loop 内 goroutine + 并发槽 + 按 wire 下标回填 ⇒ 必须动 seele 循环。
- **v2**：并发来自**作业**（工具调用只派发，作业自己 exit、自己销项，结果经投影回填）⇒ seele 零改动。
- **v3（本版）**：把 v2 从"一条主线"改成**三段优先级**，并纠正 v2 里四处错误理解。

### 优先级（用户裁定）

| 阶段 | 内容 | 动 seele？ |
|---|---|---|
| **P0** | **契约落实**：interface（添加 + 管理 + `done` 终态操作）、行 schema、投影与回填规范、bash 工具族与服务端守卫 | 否 |
| **P1** | **契约下的实现**（与 P0 规范**同步**推进）：后台 bash 重构、subagent 纳入契约、**替代掉 `async_output`** | 否 |
| **P2** | **契约同步进 seele** + seele 原生支持 loop 内并发工具调用（**feat**） | 是（feat，加能力不是改结构） |

> **关键更正**：v2 写的"seele 侧改动 = 0"**只对 P0/P1 成立**，不是永久结论。P2 的两件事（契约模型上游化 + loop 并发）正是 v1 那批设计的去处（§1–§4；打点表 P2 章）。

### 第二轮修正记录（相对 v2 的改动）

| # | 修正项 | v2 写的 | v3 改为 |
|---|---|---|---|
| **M1** | bash 形态 | 一个 `bash` + `background` 开关；读写分类只能靠服务端分类器 | **保留基本串行 bash，同时增加其他类型的 bash**（工具级分裂，§A.4）：模型选择面更清晰、权限表可读。**但服务端守卫一道都不能省**——否则"只读名牌"就是提权口 |
| **M2** | 契约形态 | "五个形态（dispatch / observe / fetch / kill / done）+ 命名" | **一个 interface：两函数（添加 + 管理）+ `done` 终态操作**（§A.1）。**串行 bash 不实现**；只有"后台运行的进程"需要实现 |
| **M3** | `done` 的主动性 | "被动形态，不需要工具" | **`done` 是契约内的终态操作，主动/被动只是调用方不同**：bash 的 done 由运行体系**被动**回填；subagent 可**自己主动** done，**main agent 也可主动** done |
| **M4** | todo 类比 | "与 todo 同构"、"exit 0 = 自动 `todo_done`" | **只是形态同源**（立项 → 销项）：todo 是打点表上的行、**不派发子进程**；作业销项是作业体系**自己的**机制（`finish()`），不是"复用了 todo" |
| **M5** | `async_output` 的去留 | 保留，另加 `async_probe` | **P1 用契约的 manage 形态替代掉 `async_output`**（§B.2）——不是并存到永久 |
| **M6** | 读/搜并发（D-1） | 二选一（作业派发 ‹晚一轮› vs loop 内并发 ‹需 feat›） | **两段式**：P1 用**作业派发**先拿收益，P2 由 seele **原生 loop 并发**接管 |

---

## 修订说明：v2 主线切换（机制替换，不是推翻）

**v1 主线**：并发来自 loop 内的 goroutine + 并发槽 + 按 wire 下标回填。代价是必须动 seele 循环（hook 载荷补 `CallID`、批量前后沿、`ParallelDispatcher`），属于框架级改动。

**v2 主线**：并发来自**后台作业**，而不是 loop 本身。工具调用只负责**派发**；派发成功即返回受理回执，tool call 本轮结束。作业自己跑、自己 exit、自己销项，结果经**投影回填**回到上下文。

> 一句话：**把「并行」从 loop 的职责改成作业系统的属性**，于是 seele 循环在 **P0/P1 阶段保持串行、零改动**；本设计**不是 refactor，而是新增一类工具调用范式（子进程调用系）**。**P2 阶段**再把契约同步进 seele 并让 seele 原生支持并发工具调用（**feat**，见 §10.7）。

### v2 的八条要点（改文档前先对齐我的理解）

| # | 要点 | 我的理解（请纠正） |
|---|---|---|
| **V1** | 派发即结束 | 子进程类工具调用 = **只负责派发**；拿到受理回执（handle/行）就算 tool call 结束，loop 不等待、不占轮次 |
| **V2** | 形态**源自** todo（第二轮已修正） | 与 todo 只是**形态同源**（立项 → 销项），**机制不同**：todo 是打点表上的行、**不派发子进程**。作业的"`exit 0` 自动销项"是作业体系**自己的**机制（`finish()`），不是"复用了 todo" |
| **V3** | 完成即回填 | 作业结束 → 划掉 active 行 → **append-only 回填**，回填内容**就是表格那一行的内容**（不靠模型主动 poll 才知道结果） |
| **V4** | 两类工具 | 读/搜（read/search）**并发**；写/改（write/update）**串行**，且写走标准 tool call 与既有审批 |
| **V5** | 契约 = interface（第二轮已修正） | 契约定义为**两个函数**：**添加（add / dispatch）+ 管理（manage）**。**凡"有进程添加与管理"的工具都要实现它**；**串行 bash 不实现**（它不产生受管进程）。`done` 是契约内的**终态操作**：**主动/被动只是调用方不同**——bash 的 done 由运行体系**被动**回填，subagent 可以**自己主动** done，main agent **也可以主动** done |
| **V6** | 契约为纲 | `init/status/done` 三类形态**写成一个契约**，且该契约**建立在子进程（作业）底座上**；后续工具一律套这个契约，**不改契约本身** |
| **V7** | 子代理同化 | subagent 建在同一契约之上 ⇒ main agent 天然可**主动查看** subagent 内容；并额外需要**干预 / 提前终止** subagent 的工具 |
| **V8** | 框架零改动 | 符合该范式的调用统一由（子）进程管理 ⇒ **不必改 seele 框架**；即便要改也是 **feat**（加能力），不是 refactor（改结构） |

### 需要你拍板的 6 个取舍点（本文档已按我的建议取值，标注为 ⚠️DECIDE）

| # | 取舍 | 用户表述 | 我的取值与理由 |
|---|---|---|---|
| **D-1** | 读/搜并发用哪种机制 | "read 和 search 支持协程并发" | **两段式**：**P1 用作业派发（`inline` 作业）**先拿收益（零框架改动、前缀天然稳定，代价是结果晚一轮）；**P2 由 seele 原生 loop 并发接管**（feat，§10.7），届时 §1–§4 的不变量 I-1..I-8 生效 |
| **D-2** | 写/改是否串行 | "write 和 update 支持串行" | **串行（按你的表述）**。但注意：`rw` 的 per-path 并行**已经实现**（§0 事实 3），全局串行是**收窄**既有能力。建议：**同名文件串行 + 跨文件仍按路径隔离**，即保留既有行为 |
| **D-3** | 回填载体是"摘要"还是"全文" | "回填的内容就是表格的内容" | **摘要 + 引用**（§10.3）：表格字段=退出码/行数/字节数/有界尾部；全文仍走游标取回/`read_tool_result`。理由：表格是**每轮重播**的投影，塞全文会按轮数线性烧 token |
| **D-4** | subagent 是否真做成 OS 进程 | "把子代理做成后台子进程给隔离出去" | **先不上 OS 进程（§10.6）**：main agent 的"可查看 / 可干预"只依赖**句柄契约**，与执行体无关；进程内已用 worktree 隔离。进程化是独立 feat（收益：崩溃/配额隔离、真中断；成本：生命周期+序列化+数十个 `subagent_*` 测试面） |
| **D-5** | bash 的形态与授权依据（第二轮已修正） | "保留基本串行 bash，同时**增加其他类型的 bash**，工具选择更多" ＋ 原案"看 agent 自身 json output 自审" | **形态取"工具级分裂"**（§10.5）：拆成 `bash`（串行）/ `bash_read`（只读）/ `bash_bg`（后台受管）等。**授权依据仍必须服务端判定**——工具级 `Groups` 是单一事实源，但 `bash_read` **必须有服务端守卫**，否则把 `git commit` 塞进 `bash_read` 就是一次提权。**自审仍否决** |
| **D-6** | bash 拆几个、叫什么 | "增加其他类型的 bash" | **【提议】3 个**（§A.4）：`bash`（串行/写类，保留现状语义）、`bash_read`（只读、免打断、**带服务端守卫**）、`bash_bg`（后台、受管、实现 §A.1 契约）。可选第 4 个 `bash_repl`（长驻交互）**先不做** |
| **D-7** | 契约两函数的命名 | （无用户表述） | **【新增待确认】取 `Add` / `Manage`**：`done` 作为 `Manage` 的终态 op，不另立函数。备选 `Dispatch`/`Control`（与权限组 `ctl` 撞词）、`Init`/`Status`（名字偏窄）。理由见 §A.1 |

> ⚠️ **D-1 与 D-2 是全局性选择**，其余为局部。若 D-1 取"作业派发"，则 §1–§4 整章降级为**备选路径**（保留，因为它是唯一能给出"同轮真并行"的方案，且其中的不变量 I-1..I-8 仍是上线前的检查清单）。

---

## 0. 决定设计形态的四个事实

在动手前必须先认清三件事，它们决定了整个方案的形状。

### 事实 1：并发分类所需的语义，`Groups` 里已经全有了

`application/contract/dto/permission.go` 的路由组注释本身就是并发策略：

| Group | 已有注释 | 天然并发语义 |
|---|---|---|
| `ro` | 读簇：不改任何共享状态 | **无共享状态 → 可并行** |
| `rw` | 写簇：项目文件 + 自有工作区 | **按路径隔离 → 可并行**（见事实 3） |
| `rw_session` | 写簇：本会话可变 transcript | **会话内串行** |
| `rw_desktop` | 写簇：共享外设（一块桌面） | **全局串行** |
| `ctl` | 叫停 loop 簇：结束/挂起/派生/装载执行结构 | **全局串行** |
| `adm` | 属主簇：改变能力面本身 | **全局串行** |

结论：**不要新增 `Serial bool` 之类的字段**。两个事实源（权限表 + 并发表）必然漂移——某天有人给 read 类工具标了 serial，两张表就打架，而且没有任何报错面。风格上也与现有 `dto` 注释的刻意设计一致：

> 唯一事实：组名与位值的字面量定义在这里，`seelebridge/tools` 的 `Group*` / `bit*` 常量是它们的别名（不是第二份定义）。

### 事实 2：生产路径从未填充 `ToolMeta` ← 这是必须先补的前置

框架侧的支持是完整的：

```go
// vendor/.../Seele/tools/tools.go:95
type ToolEntry struct {
    Definition seeletypes.Tool
    Handler    ToolHandler
    Meta       *ToolMeta   // ← 有，但没人填
}
```

但生产注册路径根本不带 meta：

```go
// seelebridge/runtime_tools.go:264
func (r *Runtime) RegisterTool(name, description string,
    inputSchema map[string]interface{},
    handler func(context.Context, string) (string, error)) {   // ← 无 meta 形参
    r.registry.AddInline(name, description, inputSchema, handler)
}

// seelebridge/tools/registry_state.go:48
func (s *RegistryState) AddInline(...) {
    s.inline.upsert(frameworktools.ToolEntry{
        Definition: types.Tool{...},
        Handler:    frameworktools.HandlerFunc(handler),
        //        ← Meta 留 nil
    })
}
```

全仓 `ToolMeta{...}` 的非零构造**只出现在测试里**（`permission_framework_gate_test.go:82,87`）。也就是说 `chainMeta` 永远把零值 `ToolMeta{}` 传给权限门——`Kind` / `Groups` / `Resource` 在运行时全是空的。

**所以 P0 的第一项（K-0）必须先做「声明路径 + 填表」，否则后面所有按 Groups 派生的逻辑都读不到东西。** 这是本设计唯一的硬前置。

### 事实 3：写侧 per-worktree 串行已经实现，而且是免费的

```go
// seelebridge/fs/filesystem_actor.go:39,51-64
type fileSystemActor struct {
    locks map[string]*sync.Mutex   // path → 写锁；同一文件互斥、不同文件并行
}
func (a *fileSystemActor) Write(path string, content []byte) error {
    lock := a.lockFor(path); lock.Lock(); defer lock.Unlock()
    ...
}
func (a *fileSystemActor) Edit(path, old, new string) (int, error) {
    lock := a.lockFor(path); lock.Lock(); defer lock.Unlock()
    ...
}
func (a *fileSystemActor) Read(path string) ([]byte, error) { return os.ReadFile(path) }  // 不加锁
```

per-path 锁 ⇒ **跨 worktree 天然并行、同文件互斥**。子代理各自在不同 worktree 上操作这件事是自动成立的。

**所以 `rw` 组不需要任何新的串行机制。** 真正需要新建串行的是 `rw_session` / `rw_desktop` / `ctl` / `adm` 这四个——这是很多人初看会搞反的地方。

### 事实 4：中间件的嵌套顺序已被框架定死，且决定了并发槽的位置

```go
// vendor/.../Seele/tools/tools.go:295-296
entry.Handler = chain(name, entry.Handler, r.middlewares)          // plain → 内层
entry.Handler = chainMeta(name, entry.Meta, entry.Handler, r.metaMiddlewares)  // meta → 外层
```

`chain` 从切片尾部往前套，所以**切片序 = 外→内**；meta 在 plain 之后套 ⇒ meta 整体在外。实际嵌套（`registry_state.go:29-35` 的装配）：

```
事件(meta) → 权限门(meta) → 诊断(plain) → handler
```

**推论：并发槽必须在 plain 层获取（权限门之内）。** 若放在 meta 层（权限门之外），一次等人点头几分钟的审批会一直占着并发槽位——把并发能力浪费在等待上，还可能与同批其它调用形成假性饥饿。

---

## A. P0：契约落实（详细设计）

> P0 的交付物**不是代码**，是"把契约写死到可以照着实现"：一个 interface、一张行 schema、一条回填规范、一族 bash 工具、一道服务端守卫。这一章是 v3 的主干。

### A.1 契约 = interface（两函数 + 一个终态操作）

> **落地修订（2026-09-26，用户裁定）**：契约的**出参一律 `[]byte`**（JSON 载荷），
> 不写 `string`；查看动作从笼统的 `Manage` 收敛成具名的 **`Status`**；**`Done` 必须在
> `JobTool` 下面**（它是契约的终态动作，不是内部钩子）。四个管理动作因此都成了契约上
> 看得见的名字：`Status` / `Fetch` / `Kill` / `Done`。

```go
// seelebridge/tools/job_contract.go（"进程型工具"的契约）
//
// 判据：**凡"有作业添加与管理"的工具都要实现这个 interface**。
// 串行 bash 不实现——它不产生受管作业（跑完即返回，没有句柄、没有生命周期）。
type JobTool interface {
    // Add 添加一个作业：起作业并**立刻**返回受理回执（handle + 行标题）。
    // 调用方不等待执行结果；回执就是这次 tool call 的全部返回。
    Add(ctx context.Context, spec JobSpec) ([]byte, error)

    // Status 查看一个作业的只读读数（原 Manage(op=observe)）：
    // 不推进游标、不消费输出——"看一眼"不该吃掉输出。
    Status(ctx context.Context, handle JobHandle) ([]byte, error)

    // Fetch 取回增量（消费式）：取过的字节不再给第二次。
    Fetch(ctx context.Context, handle JobHandle) ([]byte, error)

    // Kill 终止执行体（进程树 / 取消级联）：**已产出内容保留**，仍可 fetch。
    Kill(ctx context.Context, handle JobHandle) ([]byte, error)

    // Done 销项一个**已终态**的作业：终态迁移 + 回填，幂等（见 A.3）。
    Done(ctx context.Context, handle JobHandle) ([]byte, error)
}
```

**为什么出参是 `[]byte` 而不是 `string`**：载荷就是将来要交给模型/前端的结构化输出，
bytes 直接就是那份 JSON；先定成 `string` 的话，外面每次要用结构化字段都得再解一次、
再拼回去（"string 转 json 容易，string 当 output 再转一次 json 很鸡肋"）。
框架的 `ToolHandler` 只吃 `string`，所以转换点只有一个：**工具边界**那一处
（`job_manage` / `bash_bg` / `read_batch` 的 handler 返回 `string(payload)`）。

**为什么没有 `Manage(op, handle)` 这个形状**：op 是个字符串，写在调用点上就看不见
"这一次到底干了什么"——而 `observe` 正是最容易被当成 getter 误用的那一个。四个具名
动作把语义摊在接口上，工具面仍然只暴露一个 `job_manage` + 一个 `op` 入参（模型侧不需要
四个名字），两者之间是 `jobManager` 的一次分派（单一实现，见 `job_contract.go`）。

**`done` 的主动/被动（M3）——这是契约里最容易写错的一条**：

| 场景 | 谁触发 done | 主动 / 被动 | 现有锚点 |
|---|---|---|---|
| 后台 bash 进程退出 | **运行体系**（`awaitAsync` 收尾时） | **被动**——bash 自己不会调 `done` | `finish()`（`async_exec.go`）：`close(run.done)` + `notifyLocked()` |
| 后台 bash 被 kill | 运行体系（`markKilled` → `finish`） | 被动 | `markKilled` |
| subagent 编排跑完 / 失败 / 被取消 | **运行体系**（`Router.CompleteJob`，fork 收尾时） | 被动（**执行体判定终态**） | 落地：`job_subagent.go` 的 `CompleteJob` + fork 的 `dispatchJobs` |
| subagent 被 main 叫停 | **main agent 调 `job_manage(op=kill)`** | **主动** | 落地：`jobManager.Kill` → 登记表里的取消口（取消整批编排） |
| 结清任一终态行 | 模型侧 `job_manage(op=done)` | **主动**（销项，不迁移终态） | 落地：`jobManager.Done` + 幂等守卫 |
| inline 作业跑完 | 运行体系（goroutine 收尾） | 被动 | 同 bash（同一状态机） |

> **落地口径（与上文表格的一处收紧）**：`Done` 只销**已落定的终态行**——在途作业调用
> `op=done` 直接报错并要求改用 `op=kill`。理由：终态只由执行体判定（K-5 的第 5 条约束），
> "模型说它完了"不算。于是 `done` 的主动/被动两个入口是
> **运行体系 `CompleteJob`（迁移终态，被动）+ 模型侧 `op=done`（销项回填，主动）**，
> 两者共用同一条状态机，且每个 handle 只迁移一次（`finish`/`CompleteJob` 的
> "状态非 running 即返回"守卫）。

> 结论：**`done` 不是"bash 专属的被动形态"，而是契约的终态操作**；主动/被动只决定**调用方是谁**。v2 把它写成"不需要工具"是把一个契约动作误降级成了一个实现细节。
>
> 推论（P1 的工作面）：`Manage(OpDone)` 必须能被**模型侧**调用（subagent 场景），也必须能被**运行体系**调用（bash 场景）——即它同时是"工具"和"内部钩子"，**同一份状态机、两种入口**。

**两个函数的命名是待确认项**（本文档取 `Add` / `Manage`）：

| 候选 | 赞成 | 反对 |
|---|---|---|
| **`Add` / `Manage`** ✅ 推荐 | 覆盖"添加 + 管理"两层语义，`done` 作为 `Manage` 的一个 op 不另立函数 | 动词偏抽象 |
| `Dispatch` / `Control` | 与现有 `dispatchAsync` 同名，好认 | `Control` 与权限组 `ctl` 撞词，读起来像"叫停 loop 族" |
| `Init` / `Status` | 贴合用户最初的"bash init / bash status"说法 | `Status` 表达不了 kill/done/fetch（名字偏窄） |

### A.2 三种作业与行 schema

| 作业类型 | 事实源 | `Add` 的调用方 | `done` 的触发方 | 可 kill |
|---|---|---|---|---|
| **`process`** | 后台 bash（`asyncRegistry`，已有） | 模型（`bash_bg`） | 运行体系（进程退出，被动） | ✅（`async_kill` 已有） |
| **`inline`** | 读/搜扇出（v2 新增，同一状态机） | 模型（批量读工具） | 运行体系（goroutine 收尾，被动） | ✅ |
| **`subagent`** | `fork_subagents` / plan agent 节点 | 模型（`fork_subagents`） | **subagent 自己**（主动）或 **main agent**（主动） | ✅（P1 新增） |

**行 schema 收敛**（扩 `dto.AsyncRunRecord`，`application/contract/dto/async_run.go:20-45` 已含其中 12 个字段）：

| 字段 | 用途 | 约束 |
|---|---|---|
| `Kind` | `process` / `inline` / `subagent` | **新增**（决定读取口与 kill 语义） |
| `Handle` | 唯一锚点 | 与 history 回执里的 handle **必须一致**（可对账） |
| `SessionID` | 会话归属 | 已有；跨会话取回一律拒绝（`async_tools.go:44`） |
| `BatchID` | 同批派发归组 | 已有（`begin` 的 batchID 形参） |
| `Description` | 行标题 | 已有（`background=true` 必填，`router.go:494`） |
| `State` | `running` / `done` / `failed` / `killed` | **四态即可**（`async_exec.go:25-29` 已是这四态） |
| `ExitCode` | 终态退出码 | 已有；running 时 `-1` 而非 `0`（`async_probe.go:92`） |
| `Summary` | 退出码 + 行数 + 字节数 + **有界尾部** | **新增**；**硬上限 ≤512 字节**，超限截断并标注"全文经 fetch 取回" |
| `Notified` | 该作业是否已回填过 | **新增**（幂等键；见 A.3 约束 K-4） |
| `Index` | 同批内 wire 下标（inline 用） | 新增；**排序键**，不是完成序 |

### A.3 完成回填：语义、载体与生命周期

**目标形态**（用户 V3 原话："子进程的结束划掉 active 下的内容，然后 append only 回填到上下文中，回填的内容就是表格的内容"）：

```
作业在跑：投影 = [ … running 行（handle / 标题 / 字节数）… ]
      ↓ finish() → notifyLocked() → Events()（已有触发链）
作业结束：投影 = [ … 完成行（handle / 标题 / 退出码 / 行数 / 字节数 / 有界尾部）… ]   ← running 行被"划掉"（状态迁移）
      ↓ 模型 fetch（Manage OpFetch）或确认
取回后  ：投影 = [ … 该行消失 … ]        ← history 里的工具结果成为唯一事实
```

**载体（P0 唯一的新机制决策）**：

| 案 | 做法 | 优点 | 代价 |
|---|---|---|---|
| **案 1 · 投影重播（推荐）** | 完成行带 `Summary` 留在打点块里；块**每轮重建**（`work_table.go:676` 的 `workTableTraceBlockFor`，块常量 `:648-650`；"running 行 + 字节数"就是现在的形态） | **零新机制**；无幂等/落盘问题；history 逐字节不变 ⇒ **J-1 结构上成立** | token 成本 = **轮数 × 在册完成行数**（有界：`workTableTraceMaxLines=30` + FIFO） |
| 案 2 · 合成 history 条目 | 在装配路径插入一条 `[Seelex job report: …]`（先例：`history.go:22-38` 的 `InterruptedToolResultPrefix` 正是 runtime 合成的 provider-only 条目） | token 成本**常数**；不随轮数放大 | 必须**持久化 + 幂等**（否则每次请求重新插入或重复回填）；**直接改 history 字节**，I-1 需重新论证 |

**P0 取案 1**：它用既有机制满足 V3（"回填的内容就是表格的内容"——表格那一行现在**带着结果**），且不触碰前缀稳定。案 2 只在实测发现"模型频繁漏取回"时启用（判据见打点表 P1-7）。

**必须写死的五条约束**：

| # | 约束 | 为什么 |
|---|---|---|
| **K-1** | 回填**只影响投影**，不改写 history（取回前） | 前缀稳定（I-1/J-1）；投影是视图不是通道 |
| **K-2** | 回填内容**有界**：每行 ≤512B、完成行 ≤`asyncWorkMaxRows`(32)、块 ≤`workTableTraceMaxLines`(30) | 投影按轮数重播，无界即按轮数线性烧 token |
| **K-3** | **全文只走 fetch**（`Manage`）或 `read_tool_result` 引用 | 与 K-2 同源；表格是"摘要 + 引用" |
| **K-4** | 回填**幂等**：每个 handle 每个终态**恰好回填一次**（`Notified` 位） | 否则"完成"会被重复通知；案 2 下更是硬约束 |
| **K-5** | 终态**只由执行体判定**，不得按墙钟猜 | 既有纪律（`async_exec.go:51-54`）：`noteOutput` 的时间戳只用来去抖，不参与死活判定 |

**完成行的生命周期（不定义就会无界膨胀）**：

| 事件 | 行的变化 |
|---|---|
| `finish()` exit 0 | `running` → `done`（带 `Summary`），行**保留** |
| exit ≠ 0 | `running` → `failed`（退出码 + 尾部），行**保留** |
| 被 kill | `running` → `killed`，行**保留**（区分"没跑完"与"失败"） |
| 模型 `Manage(OpFetch)` 取回 | 行**消失**（history 里的结果成为唯一事实） |
| 无人在册且超预算 | FIFO 淘汰**最老的完成行**，只留一行汇总："N 个作业已完成未取回，handle…" |

> **M4 更正落点**：v2 在这里写过"作业完成会划掉 running 行 ⇒ 等价于自动 `todo_done`"。**这个类比作废**：`todo_done` 划的是**打点表上的一行**，作业销项动的是**真派发的进程**——两者只是"立项 → 销项"这个**形态**同源，机制没有任何共享。

### A.4 bash 工具族（M1）：保留串行 bash，增加其他类型

**现状**：一个 `bash` + `background` 开关 + `timeout` + `description`（`router.go:99,464,654-670`）。模型要自己判断"这次该不该 background"，而权限层面**同一个名字**只能有一个组（`rw`），于是"只读命令也要打断人"。

**目标形态（工具级分裂）**：

| 工具 | 路由组 | 语义 | 实现契约？ | 免打断？ |
|---|---|---|---|---|
| `bash` | `rw`（`Mode=rw`，Default=Allow，rules 覆盖） | **串行、同步**、写类（保守归类）——**保留现状语义** | ❌ | ❌（`write_file` 同级，rules 里 `bash` 默认 ask + 白名单 allow） |
| `bash_read` | `ro`（`Mode=r`，Default=Allow） | **只读命令**，同步快返回 | ❌ | ✅（`ro` 组默认 allow，`async_output` 同组，`permission_policy.go:90-115`） |
| `bash_bg` | `rw` | **后台受管**（原 `background=true` 的路） | ✅（`Add` / `Manage`） | 继承 `bash` 的 rules 口径 |
| ~~`bash_repl`~~ | — | 长驻交互式会话 | — | **先不做**（D-6） |

**分裂的收益与代价**（诚实记账）：

| 收益 | 代价 |
|---|---|
| 模型的选择面从"一个工具 + 一个布尔开关 + 一段解释"变成"三个名字"，**选择更明确**；`background=true` 那个"容易被当成超时更长"的误解消失 | 工具面每多一个名字都要进 system prompt（token + 选择歧义）；**必须靠工具描述消歧** |
| 权限表**按名字**分流，无需新增"自声明分类"字段（单一事实源仍是 `Groups`） | 三个名字要同时出现在：注册处、`DefaultPermissionGroupList`、`DefaultPermissionRules`（bash 的 rules 需覆盖 `bash_bg`）、工具面过滤、文档、测试 |
| `bash_read` 落在 `ro` 组 ⇒ 子代理/只读员工天然拿得到，且**不弹审批** | **`bash_read` 必须自带服务端守卫**（见下），否则它就是"挂着只读名牌的 bash" |

**服务端守卫（不得省，R-11 的落点）**：

```go
// seelebridge/security/command_class.go（新增，纯函数 + 表驱动测试）
// ClassifyCommand 判定一条 shell 命令能否在 bash_read 下执行。
// 判据：命令首词白名单（ls/cat/head/tail/grep/rg/git status/git log/…）
//      + 无写重定向（> >> tee）
//      + 无写子命令（git commit/push、npm install、rm、mv、chmod…）
// 失败模式：**分类失败一律按写处理**（保守默认，与"未分类 = 串行"同构）。
func ClassifyCommand(command string) (readOnly bool)
```

- `bash_read` 的 handler **必须**先过它；不通过 → 报错要求改用 `bash`（**不得静默降级成执行**，与 `router.go:477` 的"能力不可实施时必须拒绝"同源）。
- 守卫**不接受任何来自模型的分类主张**：提示词约束、agent 自审、模型自报的 json output 一律不作为授权依据（自我声明式授权 = self-declared privilege，`seelebridge/security/pathgate.go` 的既有风格是"服务端按规则判定"）。

### A.5 P0 验收判据（编号 = 打点表 `K-*`）

| # | 判据 | 可执行形态 |
|---|---|---|
| K-0 | 全量注册工具的 `Groups` 非空 | 装配期读面：`Runtime.UndeclaredTools()` 为空 + `TestBuiltinToolsDeclareGroupsAndMetas`；源码扫描：根包 `TestEveryRegisteredToolDeclaresGroups`（TC-K0-1） |
| K-1 | interface 写进代码并被**三类作业**满足 | 编译期断言 `var _ JobTool = (*bashBgTool)(nil)` / `(*inlineReadTool)(nil)` / `(*subagentTool)(nil)`（`TestJobToolContractImplementations`，TC-K1-1） |
| K-3 | bash 工具族在权限表里**按预期分组**（`bash`/`bash_bg`/`job_manage` → rw；`bash_read`/`read_batch` → ro） | 表驱动路由断言 `TestBashFamilyRoutingTable`（TC-K3-1）+ 契约表 `TestJobContractToolFaceRoutingTable` |
| K-4 | `bash_read` 守卫：**写命令不可能通过** | `seelebridge/security/command_class_test.go` 表驱动：`git commit`/`rm -rf`/`echo hi > f`/`$X`/复合命令必须被拒；`ls`/`git status`/`go test` 通过（TC-K4-1..4） |
| K-5 | 回填有界 + 幂等（K-2 / K-4 两条约束） | `TestJobSummaryIsBounded`（≤512B，按字节切）、`TestJobTerminalTransitionHappensOnce`、core 侧 `TestAsyncBackfillStaysBounded`（100 个完成作业 → 行数有界 + 汇总行，TC-K5-1/2） |
| K-6 | `done` 两种入口都通（运行体系被动 / 模型侧主动） | `TestJobTerminalTransitionHappensOnce` + `TestJobManageEntryRejectsInvalidCalls`（在途 done 报错、重复 done 无副作用）+ `TestSubagentJobContract`（TC-K6-1） |

---

## B. P1：契约下的实现（与 P0 规范同步推进）

> P0 定规范、P1 落实现，两者**同步**推进（用户裁定），不是先 A 后 B 的串行阶段。P1 的三件事：后台 bash 走契约、**替代 `async_output`**、subagent 纳入契约。

### B.1 后台 bash 走 `Add`/`Manage`（`bash_bg`）

| 项 | 现状 | 目标 |
|---|---|---|
| 派发 | `bash(background=true)` → `dispatchAsync`（`async_run.go:151`）→ `renderAccepted` | `bash_bg` → `JobTool.Add` → 同一回执（**回执形状不变**，模型侧观感连续） |
| 观察 | 无模型面工具（只有 GUI 探针 `AsyncRuns`） | `Manage(OpObserve)` |
| 取回 | `async_output` → `advanceTail` + `markCursor` | `Manage(OpFetch)` |
| 终止 | `async_kill` → `killTarget` | `Manage(OpKill)` |
| 销项 | `finish()`（内部） | `Manage(OpDone)`，**同时**由运行体系被动调用 |

**只换壳不换状态机**：`asyncRegistry` 的 `begin/attach/snapshot/finish/killTarget/advanceTail/markCursor/notifyLocked` 全部保留（`async_exec.go`，久经测试）。`Add`/`Manage` 是它们的命名外壳 + 权限/回执面。

### B.2 替代 `async_output`（M5）

`async_output` 的语义**全部被 `Manage(OpFetch)` 覆盖**，而它的名字把"取回"耦死在"后台命令"上——subagent / inline 作业也要取回，名字必须泛化。

**必须一并迁移的面（漏一个就是死工具）**：

| # | 面 | 位置 |
|---|---|---|
| 1 | 工具注册 | `router.go:103-104` |
| 2 | 权限分组（**当前在 `ro` 组，且注释写明"不重复弹审批"**） | `permission_policy.go:99-101` |
| 3 | 描述文本（含 `wait_ms` 三档语义、"别为确认进展花一轮"的劝告） | `async_tools.go:144-152` |
| 4 | 注册开关（`asyncEnabled()` 门控） | `router.go:101-105` |
| 5 | 既有测试（`TestAsyncOutputRoutesToReadOnlyGroup`、`TestAsyncPollDeliversOnlyNewBytes` 等） | `async_exec_test.go:530,138` |
| 6 | 文档/提示词/工具面文案 | 本文档 + 打点表 + 工具描述 |
| 7 | 兼容期别名（可选） | 若历史会话里有 `async_output` 的调用记录，取回路径需认旧名 |

**必须保留的两条性质（丢了就丢输出）**：

| 性质 | 现状 | 为什么不能丢 |
|---|---|---|
| **消费式游标**：fetch 推进 `cursor`，取过的增量不再给第二次 | `advanceTail`（`:380`）+ `markCursor`（`:415`），唯一调用点 `async_tools.go:46-50` | 否则每轮把整份日志重播进上下文（轮询型唯一真实的 token 风险） |
| **观察不推进游标**：observe 是只读旁路 | `async_probe.go:43,61`（`AsyncRuns` 不碰 cursor） | 否则"看一眼"就吃掉输出（`async_probe.go:11-19` 已写明这条纪律） |

### B.3 subagent 纳入契约（`Kind=subagent`）

**现状（已复核）**：`fork_subagents`（`seelebridge/fork/tool.go:44`）是**一次阻塞调用**——main agent 的 tool call 要等到全部子代理与 summary 节点跑完才返回（`seelebridge/fork/README.md` 时序图："Waiting for output 期间即预期行为"）。模型侧**没有任何**查看/干预/终止子代理的工具（G-3）。

**目标形态**：

| 诉求 | 实现 | 依赖 OS 进程化吗 |
|---|---|---|
| main agent **主动查看** subagent 在干什么 | `Manage(OpObserve)`（泛化 `AsyncRuns` 的只读探针） | **不需要** |
| main agent **提前终止** subagent | `Manage(OpKill)`：终止 = 回收 worktree + 标 `killed` 行 + **保留已产出内容**（不是丢结果） | **不需要** |
| subagent **自己销项** | subagent 调 `Manage(OpDone)` | **不需要** |
| 崩溃/配额隔离、真中断 | 需要 OS 进程 | 需要（独立 feat，不与本设计捆绑） |

**为什么先不做进程化**：V7 的两个诉求（可查看 / 可干预）只依赖**句柄契约**，与执行体是 goroutine 还是 OS 进程正交。进程化会同时引入生命周期管理 + 结果序列化 + 数十个 `subagent_*` 测试面的改动，把"最小改动"变成"最大改动"。

**这一步的真实工作量在 `fork_subagents` 的语义变更**：从"阻塞直到全部完成"改成"`Add` 派发 + 结果经回填/reference 取回"——它牵动 `fork` 的 DAG 编排、task 幂等登记、summary 节点、worktree 生命周期、merge-back 合回主会话（`seelebridge/fork/tool.go`、`runtime_plan.go`）。**这是 P1 里唯一的大改动，也是 P1 的进度风险点。**

### B.4 迁移面（提示词 / 配置 / 文档）

| # | 面 | 改什么 |
|---|---|---|
| 1 | 系统提示词的 bash 段落 | 从"一个 bash + background 说明"改成三个名字 + 各自适用场景（**必须写清 `bash_read` 只允许只读命令**，否则模型会拿它当 bash 用） |
| 2 | `seele.yaml` 的 `limits.async_exec.enabled` | 名字与新契约对齐（能力开关现在是"进程型工具族"的总闸） |
| 3 | 权限配置的持久化面 | 三个新名字要能在员工/角色权限装配里分格（`EmployeePermission.Groups` 按组给位，**工具名不进配置**——只要组对，装配面自动生效） |
| 4 | 文档 | 本文档、打点表、测试用例三份同步；`docs/2026-09-24-async-tool-deferred-ack/README.md`（既有规格）标注被本契约取代的范围 |

### B.5 P1 验收判据（编号 = 打点表 `L-*`）

| # | 判据 |
|---|---|
| L-1 | `bash_bg` 派发回执与旧 `bash(background=true)` **逐字段等价**：`TestJobAcceptedReceiptMatchesLegacyShape`（同一条 `renderJobAccepted` 渲染路径） |
| L-1 | 五个名字在模型面上可用（`bash` / `bash_read` / `bash_bg` / `read_batch` / `job_manage`），且 `bash_read` 对写命令**报错**（不是执行）：`TestJobCapabilityGatesSchemaAndRegistration` + `command_class_test.go` |
| L-2 | **消费式游标**（`TestAsyncPollDeliversOnlyNewBytes`）与**观察只读**（`TestObserveDoesNotAdvanceCursor`）两条性质不变 |
| L-2 | `async_output` 已下线：注册表、权限组、描述、文案、测试、配置**零残留**（`TestRetiredJobToolNamesLeaveNoResidue`；允许的只有标注为迁移说明的行与 `docs/`+`CHANGELOG`） |
| L-3 | 派发即返回、一批 N 个句柄：`TestReadBatchDispatchesJobsAndReturnsImmediately`（墙钟收益是人工验收 AA-1，**P2 门的判据数据**） |
| L-4 | kill 后**已产出内容不丢**：`TestJobKillPreservesProducedContent`（非进程作业）+ `TestAsyncKillTerminatesProcessTree`（`exit=137` 注记可取回） |
| L-5 | subagent：模型面可 observe / kill / done —— `TestSubagentJobContract`（契约层）+ `TestForkSubagentsAsyncRunsAsJobs`（端到端：`fork_subagents{async:true}` → observe / fetch / 销项消失） |
| — | 既有 async 测试全绿（`async_exec_*`、`async_kill_*`、`async_probe_*`、`async_progress_*`、`work_table_async_*`） |

**L-5 的剩余边界（诚实记账）**：subagent 的"自己销项"目前由运行体系在节点收尾时以
`CompleteJob` 落地（执行体判定终态），模型侧的 `op=done` 负责结清那一行；把句柄注进
子代理的节点提示词（让它自己调 `job_manage`）不在本轮，因为它要动 `plan` 节点输入面，
而它不改变任何一条已落地判据。

**L-3 的一个已知取舍**：`read_batch` 的产出走作业正文文件（`[seelex:job] read_file …`
头 + 原样内容），因此取回是**两段式**（先 fetch 再读内容）；把它换成"结构化结果由
`read_tool_result` 引用回读"是 P2 之后的事（那时 loop 内并发才是主路径）。

---

## C. P2：契约进 seele + loop 并发（feat）

> P2 是**加能力**，不是改结构（V8）。它做两件事，且只有当 P0/P1 已稳定、且"同轮并行、同轮见结果"被证明是硬需求时才启动。

| # | 内容 | 依据 |
|---|---|---|
| **S-0** | **门**：用 P1 的墙钟数据证明"结果晚一轮不可接受"，否则不开工 | §7 阶段门 |
| **S-1** | **把契约模型同步到 seele**：`JobTool` 这一层从 seelex 的实现细节上升为框架可识别的一类工具（进程型工具的通用面），使"派发即结束 + 句柄管理"成为框架的一等范式 | 用户 V6 / V8："契约为纲"；P0/P1 先在 seelex 内把语义跑通，再上游化才不平移 |
| **S-2** | **seele 支持 loop 内并发工具调用**：一批 tool_call 并发派发、按 wire 下标保序回填 | §1–§4 全量（不变量 I-1..I-8）+ 打点表 P2 章 |
| **S-3** | 框架侧最小前置：`ToolCallInfo` 补 `ID` / `Index` / `BatchSize` | 现无 ID ⇒ hook 按 `(Turn, Name, Arguments)` FIFO 配对（`tool_hooks.go:448`），同批同名同参调用**必然错配**（I-2 的前置缺口） |
| **S-4** | 批量前后沿（hook 单 goroutine、按 Index 序触发） | hook 契约是"同步调用、不要阻塞"（`session/hooks.go`），并发调用会破 I-8 |

**S-2 的判据（D-1/M6 的第二段）**：P1 的作业派发若已能把"读/搜扇出"的墙钟收益拿到（代价是结果晚一轮），则 S-2 只在"晚一轮不可接受"时才有必要。**先实测再选型。**

---

## 1. 【P2 备选路径】并行 read 的精确语义

> **⚠️ v1 备选路径**：本章描述"loop 内 goroutine 并行 + 按 wire 下标回填"的精确语义。**v3 分段**：P1 的读/搜并发改由**作业派发**实现（本章不落地）；只有 P2 门通过（实测晚一轮不可接受）时，本章才启用。但 §1.3「顺序的键是 Index、身份的键是 CallID」这条**概念区分在任何阶段都成立且必须遵守**（作业回执也要按 wire 序 append，见 J-1）。

### 1.1 你要的行为，精确定义为

一次 assistant 消息里的 N 个 tool_call：

```
执行层：N 个 goroutine 并发跑（完成顺序任意）
回填层：等齐后，按 wire 下标排序，逐个 append 进 history
结果  ：history 里 N 个 tool 消息连续、有序，与串行执行时逐字节相同
```

**「看起来串行、本质并行」是准确的描述**，而且这正是必须的：provider 要求同一批 tool 结果紧跟其 assistant 消息，且 prefix 稳定才能吃到 prompt cache。

### 1.2 但因果链要修一处

> 「因为这个是 waitgroup 所以在并发也是前缀稳定的」

`WaitGroup` **只保证「等齐」，不保证「顺序」**。前缀稳定 = **「等待齐」+「按稳定键排序回填」** 两个条件共同的结果，`WaitGroup` 只是必要条件。

反例：两个 goroutine 各写 `slice[i]`，用 `WaitGroup` 等齐后直接遍历切片——顺序取决于写入索引。若索引来自完成序，前缀就不稳。

### 1.3 排序键的选择（本设计最容易出错的一处）

| 候选键 | 是否稳定 | 说明 |
|---|---|---|
| 随机 UUID | ❌ | 跨轮/跨重试顺序漂移 → 前缀不稳 → cache 全失效 |
| `tc.ID`（provider 的 `call_xxx`） | ❌ | **无序**，只能当身份不能当顺序 |
| `ToolHookBridge.nextToolIDLocked()` 的 `tool-%d` | ⚠️ | 单调递增，但只在分配顺序 == wire 顺序时才等价 |
| **`assistantMsg.ToolCalls` 的数组下标** | ✅ | **唯一真正稳定的键**——它是模型输出的一部分，同一条消息内容固定 |

**所以核心设计决策：把「顺序」与「身份」彻底分开。**

| 概念 | 职责 | 取值 | 现状 |
|---|---|---|---|
| **Index（顺序）** | 回填排序、history 顺序 | `assistantMsg.ToolCalls` 数组下标 | 未使用 |
| **CallID（身份）** | 配对 hook / 结果通道 / 审批 / telemetry | `tc.ID` | **已有**，但未进 hook 载荷 |

你说的「先分 uuid 再按 uuid 排序」，如果 uuid 是**分配序号的别名**，那它恰好等价于 wire 下标，思路正确；但如果真是随机 UUID，就要改成 wire 下标。**混用这两个概念是本设计最大的坑，且症状是静默的（不报错、只是 cache 命中率掉、hook 偶尔错配）。**

### 1.4 贯穿全链的顺序契约（必须写成不变量）

```
Index = 0 ──┐
Index = 1 ──┤  并发执行（完成序任意）
Index = 2 ──┘
      │
      ▼  WaitGroup 等齐
      │
      ▼  按 Index 排序
      │
assistant(tool_calls: c0,c1,c2)   ← 模型原样
tool(c0, result)                  ← 按 Index 回填
tool(c1, result)
tool(c2, result)
```

---

## 2. 【P2 备选路径】分类矩阵：从 Groups 派生并发策略

### 2.1 策略表

| Group | 并发策略 | 隔离范围 | 现有保障 | 需新建 |
|---|---|---|---|---|
| `ro` | **并行**（无上限限制） | — | 无（读无共享状态） | — |
| `rw` | **并行** | **per-path** | **fs actor 已实现** | — ⚠️DECIDE **D-2**：用户主张"写/改串行"，见 §2.3 的 v2 注 |
| `rw_session` | **串行** | per-session | — | ✅ 会话级锁 |
| `rw_desktop` | **串行** | **全局** | — | ✅ 全局锁 |
| `ctl` | **串行** | **全局** | — | ✅ 全局锁 |
| `adm` | **串行** | **全局** | — | ✅ 全局锁 |
| 空/未分类 | **串行** | 全局 | — | 保守默认 |

### 2.2 两条必须坚持的默认

1. **未分类 = 串行。** `ToolMeta` 现在全空（事实 2），派生逻辑若把「空 Groups」当并行，上线第一天就是全线并行——所以默认必须是串行，白名单开并行。
2. **桶间并行、桶内串行。** 同一资源键（如 `rw_desktop`）的调用即使在不同 index 也要排队；不同资源键的桶之间互不阻塞。

### 2.3 为什么 `ro` 可以放开

`ro` 的定义就是「不改任何共享状态」，所以并行读之间无因果。这也是唯一一个可以无条件开并行的组。

**注意 `rw` 也开并行不是疏忽**——是事实 3 的直接结果。很多人会本能地认为「写必须串行」，但既有实现已经是 per-path，把 `rw` 降级成全局串行反而会**削弱**现有能力。

> **v2 注（D-2）**：用户的新表述是「以写操作作为标准 tool calling，write/update **串行**」。这与本节的 per-path 并行冲突，取值建议如下：
>
> | 方案 | 语义 | 评价 |
> |---|---|---|
> | (a) 全局串行 | 所有写/改排队 | 最安全，但**扔掉** fs actor 已有的 per-path 能力，且跨文件批量改动变慢 |
> | (b) **per-path 串行（现状）** | 同文件互斥、跨文件并行 | **建议保留**：语义上"每个文件内部是串行的"，已经在"写是串行的"这一意图之内，只是不把不相干的文件互相阻塞 |
> | (c) 作业化串行 | 写/改也走派发 | 会让"写"失去同步回执，**不建议**（见 §10.4 的判据） |
>
> 需要用户确认取 (a) 还是 (b)。若取 (a)，实现代价是**删代码**（fs actor 的 per-path 锁退化为全局锁），不是加代码——这也是它值得明确拍板的原因。

---

## 3. 【P2 备选路径】架构（loop 内并发）

> **⚠️ v2 定位**：本章（§3.1–§3.7）是**备选路径**——"同轮真并行（loop 内 goroutine + 并发槽 + 按 wire 下标回填）"的实现方案。它只在 **D-1 取"要同轮并行"** 时才需要落地，届时必须连带 seele 的 feat（P0-1）。
>
> **v3 分段**：P0/P1（作业派发）**不需要** `ParallelDispatcher`、不需要并发槽中间件、不需要批量前后沿 hook——并发在作业系统里，loop 保持串行。本章的四个部件属于 **P2**（§C-2），届时才需要。
>
> 保留本章的理由：① 它是唯一能给出"同轮并行、同轮见结果"的方案；② 其中 §3.6 的「hook 只能单 goroutine、同步触发」是**框架硬约束**，对任何方案（含 §10）都成立，不得违反。

### 3.1 分层与依赖倒置

```
┌─────────────────────────────────────────────────────────────┐
│ application 层：声明 + 策略（纯数据 / 纯函数，无并发原语）    │
│   · ConcurrencyClass（enum）                                 │
│   · PolicyFor(groups []string) ConcurrencyPolicy             │
│   · ResourceKey(toolName, argsJSON) string                   │
└──────────────────────────┬──────────────────────────────────┘
                           │ 接口（端口定义在此，实现在下）
┌──────────────────────────▼──────────────────────────────────┐
│ seelebridge 层：执行（goroutine / 信号量 / 锁 / 排队）        │
│   · SlotAcquirer   ← 由 Policy 决定取哪种槽                  │
│   · ParallelDispatcher ← loop 层的分派器                     │
│   · ResultCollector   ← index 索引回填                       │
└─────────────────────────────────────────────────────────────┘
```

依赖倒置的落点：**application 只描述「什么可以并行、按什么键隔离」，不知道「怎么并行」**。换实现（goroutine → 进程池 → 远程执行）不动 application 一行。

### 3.2 端口定义

```go
// application/core/toolpolicy —— 纯声明，无并发原语
package toolpolicy

type ConcurrencyClass int

const (
    ClassSerialGlobal  ConcurrencyClass = iota // 全局串行
    ClassSerialSession                         // 会话内串行
    ClassSerialResource                        // 按资源键串行
    ClassParallel                              // 可并行
)

type Policy struct {
    Class       ConcurrencyClass
    ResourceKey string   // ClassSerialResource 时的隔离键（如文件绝对路径）
}

// PolicyFor 从已有的路由组派生策略。零默认 = 串行。
func PolicyFor(groups []string) ConcurrencyClass {
    if len(groups) == 0 {
        return ClassSerialGlobal   // 未分类 → 保守串行
    }
    for _, g := range groups {
        switch g {
        case "rw_desktop", "ctl", "adm":
            return ClassSerialGlobal
        case "rw_session":
            return ClassSerialSession
        }
    }
    for _, g := range groups {
        switch g {
        case "rw":
            return ClassSerialResource   // 隔离键来自具体参数
        case "ro":
            return ClassParallel
        }
    }
    return ClassSerialGlobal
}
```

> 注意优先级：**先扫串行组再扫并行组**。一个工具同时属于 `ro` 和 `ctl` 时必须以串行为准——安全侧优先。

### 3.3 责任链：并发槽中间件的位置

```
事件(meta)  →  权限门(meta)  →  诊断(plain)  →  并发槽(plain)  →  handler
                                              ↑  新增，必须在此
```

装配改动（`registry_state.go:29-35`）：

```go
Registry: frameworktools.NewRegistry(
    frameworktools.WithCallTimeout(timeout),
    frameworktools.WithMiddleware(
        diagnosticMiddleware,
        concurrencyMiddleware,      // ← 新增：plain 层的末位 = 最内 = 审批之后
    ),
    frameworktools.WithMetaMiddleware(
        asMetaMiddleware(eventMiddleware),
        permission.Middleware(approvalTimeout),
    ),
)
```

各环职责矩阵：

| 环 | 类型 | 职责 | 与并发的关系 |
|---|---|---|---|
| 事件 | meta | `OnToolStart` / `OnToolComplete` | **并行下必须携带 CallID**，否则按 (turn,name,args) FIFO 配对会静默错配 |
| 权限门 | meta | 审批 / 放行 | **必须在槽外**（人等 = 不占槽） |
| 诊断 | plain | 观测埋点 | 无 |
| **并发槽** | **plain** | 取槽 → 执行 → 还槽 | **新增**；读 Groups 决定槽型 |
| handler | — | 实际执行 | — |

### 3.4 装饰器

```go
// 装饰器 1：给执行套并发槽
func WithSlot(policy toolpolicy.Policy, acquire func(toolpolicy.Policy) (release func(), ok bool),
    next frameworktools.ToolHandler) frameworktools.ToolHandler {
    return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
        p := policy
        p.ResourceKey = resourceKeyFor(p.Class, argsJSON)   // rw → 文件绝对路径
        release, ok := acquire(p)
        if !ok {
            return "", fmt.Errorf("tool: 并发槽等待超时（%s）", p.ResourceKey)
        }
        defer release()
        return next.Execute(ctx, argsJSON)
    })
}

// 装饰器 2：把结果写到 index 槽位，而非直接 append
type indexedSink interface{ Put(index int, out toolOutcome) }
```

### 3.5 分派器（loop 层新增）

```go
// seelebridge/dispatch —— 替换 loop.go:327 的串行 for
type ParallelDispatcher struct {
    sink    indexedSink
    acquire func(toolpolicy.Policy) (release func(), ok bool)
    maxPar  int
}

func (d *ParallelDispatcher) Dispatch(ctx context.Context,
    calls []types.ToolCall,            // 模型给的顺序 = Index
    groupsOf func(name string) []string,
) []toolOutcome {

    outcomes := make([]toolOutcome, len(calls))   // ← 按 Index 定址，不是 append
    var wg sync.WaitGroup
    sem := make(chan struct{}, d.maxPar)          // 并行桶的并发上限

    // 串行桶：按 Index 序，在独立通道上排队
    serialCh := map[string]chan struct{}{}

    for i, tc := range calls {
        class := toolpolicy.PolicyFor(groupsOf(tc.Name))
        wg.Add(1)
        go func(i int, tc types.ToolCall, class toolpolicy.ConcurrencyClass) {
            defer wg.Done()
            if class != toolpolicy.ClassParallel {
                queue := serialQueueFor(class, tc)   // 同键同队列
                queue <- struct{}{}
                defer func() { <-queue }()
            } else {
                sem <- struct{}{}
                defer func() { <-sem }()
            }
            outcomes[i] = runOne(ctx, tc)            // 按 Index 定址
        }(i, tc, class)
    }
    wg.Wait()                                        // 等齐
    return outcomes                                  // 已是 Index 序
}
```

**关键点：`outcomes[i] = ...` 按 Index 定址，且 `wg.Wait()` 之后才返回。** 切片本身就承载了顺序，不需要额外排序——这比「完成后收集再排序」更不易错。

### 3.6 hook 调用的并发契约（框架硬约束）

`hooks.go` 对 `LoopHooks` 写死了一条约束：

> 回调在循环内**同步**调用——**不要在回调中执行阻塞操作**

而 `ToolCallInfo` 的字段是 `Turn / Name / Arguments / Result / Error / Duration`——**没有 ID**（P0-1 要补的就是它）。

两条推论：

1. **hook 不能在 N 个 goroutine 里直接并发调用。** 回调实现（`ToolHookBridge`）用互斥量/chan 保护自己的状态，但 application 层的处理链（session transcript、`AttributeToolNarrationLocked`…）没有为并发调用设计。
2. **正确的做法是「批量前后沿」**：所有 `OnToolStart` 在派发**之前**按 Index 顺序连续触发，所有 `OnToolComplete` 在**等齐之后**按 Index 顺序连续触发。

```
OnToolStart(0) → OnToolStart(1) → OnToolStart(2)      ← 派发前，顺序，单 goroutine
        ↓ 并发执行（hook 不参与）
OnToolComplete(0) → OnToolComplete(1) → OnToolComplete(2)   ← 等齐后，顺序，单 goroutine
```

这样既**完全保持**「回调节点在单线程、且同步」的既有契约，又让 UI 看到的形状是明确定义的「N 个 start，然后 N 个 complete」——**顺带解决了 R-3 里 UI 假定 start→complete 严格交替的问题**：交替性在 hook 层被恢复，UI 只需额外知道「这是一个 batch」。

这是本设计里代价最小、收益最大的一处取舍：**用「批量前后沿」换取「零 hook 并发」**。

**代价（要诚实记账）**：`OnToolStart` 提前触发意味着 UI 看到的「该工具运行中」跨度 = 整批跨度，而非各工具自身时长。但 `ToolCallInfo.Duration` 字段仍在，`OnToolComplete` 里填**真实单工具耗时**即可——丢失的只是可视化跨度，不是数据。若某个 UI 需要精确跨度，可另加一个「后沿 start」轻事件，但不应为此引入 hook 并发。

### 3.7 回填（loop.go 的改动面）

```go
// 原：串行 for + 每个调用即时 append
outcomes := dispatcher.Dispatch(toolCtx, assistantMsg.ToolCalls, groupsOf)
for i, tc := range assistantMsg.ToolCalls {          // ← 按 wire 序回填
    o := outcomes[i]
    rl.OnToolComplete(tc.Name, string(o.result))     // 需带 CallID
    rl.history = append(rl.history, types.Message{
        Role:       "tool",
        ToolCallID: tc.ID,                           // 已有
        Content:    resultContent(o),
    })
    // ... ContextAfterTool 事件
}
```

**回填循环仍是串行的、且按 wire 序**——这就是「看起来串行」的实现来源。

---

## 4. 【P2 备选路径】必须写成测试的不变量（I-1..I-8）

> **⚠️ v1 备选路径**：I-1..I-8 是"loop 内并发"方案的不变量（**P2 的检查清单**）。P0/P1 的不变量有两组：**K-1..K-5（回填，§A.3）** 与 **J-1..J-6（作业与 history 的关系，§10.8）**。
> 本章**不删除**：若 D-1 最终选择 loop 内并发，这张表就是上线检查清单；即便选择 v2，I-2/I-3/I-7（配对唯一、槽外审批、未分类串行）对**权限与分类**依然是有效断言。

| # | 不变量 | 验证方式 |
|---|---|---|
| **I-1** | 前缀稳定：并发执行不改变 history 的 `(role, call_id, content)` 序列 | 同一批调用跑两次（人为打乱完成序），history 逐字节相等 |
| **I-2** | 配对唯一：每个 tool_call 恰好一个 tool_result，CallID 一致 | 计数断言 + ID 集合比对 |
| **I-3** | 槽外审批：审批等待期间不持有并发槽 | 并发发起 N 个待批调用，断言槽位未被占用 |
| **I-4** | 桶内串行：同资源键的操作时间区间不重叠 | 记录 enter/exit 时间戳，断言无交叠 |
| **I-5** | 结果保序：回填顺序 == wire 下标序，与完成序无关 | 人为让 index=2 先完成，断言仍写在 index=2 |
| **I-6** | 失败隔离：单个工具失败不影响同批其它工具，错误落在原 index | 注入一个必败工具 |
| **I-7** | 未分类串行：`Groups == nil` 的工具有效串行 | 注册一个无 meta 工具 |
| **I-8** | hook 不并发：任一时刻只有一个 hook 在执行，且 start 全在 dispatch 前、complete 全在 wait 后 | hook 内加并发探测（原子计数断言 == 1）+ 事件序列断言 |

**I-1 是本设计的存在理由**，其它都是围绕它的保障。

---

## 5. 后台作业：派发 / 观察 / 销项 / 回填

> **v2**：本章是 §10 契约的**现状锚点**——契约不需要新建机制，它需要**补齐三个缺口**并**给现有机制命名**。

对应你的第 3 点。**现状比你以为的完整**：

| 能力 | 现状 | 位置 |
|---|---|---|
| 后台派发 | ✅ | `dispatchAsync` → `go r.awaitAsync(...)` |
| 受理回执（handle + log_path） | ✅ | `renderAccepted` |
| 增量取回（游标式） | ✅ | `scopedAsyncOutput` → `advanceTail` |
| 终止（整棵进程树） | ✅ | `scopedAsyncKill` → `killTarget` |
| 在途计数 | ✅ | `AsyncPendingFor(sessionID)` |
| 工作表格行（行标题 = description） | ✅ | `asyncRegistry.begin` + batchID |
| **只读探针（不推进游标）** | ✅ **已实现**（本节写完后的复核结论） | `seelebridge/tools/async_probe.go:24-58`：`Router.AsyncRuns()` / `AsyncRunInfo` 是只读旁路；消费式游标唯一入口是 `advanceTail`+`markCursor`（`async_exec.go:380,415`，调用点 `async_tools.go:46-50`）。**§5.1 的要求成立，但不必再做**——补一条回归断言即可（已落地：`subprocess_contract_test.go` 的 `TestObserveDoesNotAdvanceCursor`） |
| **工作台「后台进程」面板** | ⚠️ 待确认（GUI 侧） | 数据源已齐（`WorkTable` 行 + `AsyncRunInfo`），无需新增通道 |

### 5.0 v2 缺口表：契约要补的只有三件事

把上表的"✅"集合按 §10 的三类形态重新归类，会看到**契约本身几乎不需要新建**，缺的是：

| 缺口 | 用户要的形态 | 现状 | 缺口性质 |
|---|---|---|---|
| **G-1 被动 done 回填** | "bash done 回填"、"回填的内容就是表格的内容" | ❌ `finish()` 只 `close(done)` + `notifyLocked()` 触发**行状态**更新；`asyncTraceLines` 只投 **running 行 + 字节数**。**结果内容不会自己回到上下文**，必须模型主动调 `async_output`（消费式游标） | **真缺口**，改动面小：投影侧（§6 / §10.3） |
| **G-2 主动观察与取回** | "bash status 这种主动技能" | ⚠️ 只读旁路 `Router.AsyncRuns()`/`AsyncRunInfo` **已存在**（§5.1），但**未注册为模型面**；`async_output` 是消费式的——"看一眼"会吃掉输出 | **收敛为 `Manage(OpObserve)` / `Manage(OpFetch)`**（§A.1），无新机制 |
| **G-3 子代理的查看与干预** | "main agent 主动查看 subagent 下的内容"、"干预或提前结束 subagent" | ❌ 模型面无任何 subagent 读/停工具；`async_kill` 只管后台 shell。子代理现状是**进程内 goroutine + worktree 隔离的 Session**（`fork_subagents` → `seelebridge/runtime_plan.go:311`），模型侧只有打点表的 subagent 状态行 | **真缺口**，但可复用同一句柄契约（§10.6） |

> 结论支撑了 V6："契约为纲、不用大改契约内容"成立——**因为 `asyncRegistry` 已经是那个契约的执行体**（`begin/attach/snapshot/finish/killTarget/killSession/advanceTail/notifyLocked`），本设计的实质工作是**把它收敛成一个 interface（§A.1）、补齐 G-1/G-2/G-3、并把更多工具接入**。

### 5.1 探针必须「不推进游标」

这是唯一有设计含量的点。现在的 `async_output` 是**消费式**的——`advanceTail` 推进游标，取走的增量不会再给第二次。

如果探针复用这条路径，会出现：**探针看一眼，模型就永远拿不到那段输出了**（或者要看全量只能重跑命令）。

所以：**探针走只读旁路，取回走游标**，共用同一个 log 文件：

```go
// 观察 = Manage(OpObserve)：只读副本，不触碰 cursor（v3：这不是新工具名，
// 而是同一 interface 的一个 op；只读旁路本身已经存在，见 AsyncRuns）
func (r *Router) scopedObserve(ctx context.Context, argsJSON string) (string, error) {
    // 读 run.cursor 之后 tailWindow 字节，返回快照，不调 markCursor
}
```

| 路径 | 推进游标 | 用途 | 谁调用 |
|---|---|---|---|
| `Manage(OpFetch)`（现 `async_output`） | ✅ | 取回增量（进入下一轮上下文） | 模型 |
| `Manage(OpObserve)`（只读旁路已存在） | ❌ | 看现在到哪了（旁路观察） | 模型 / 面板 |

### 5.2 工作台面板

数据源已经够：`AsyncPendingFor` + `asyncRegistry.runs` 的快照。需要给 registry 加一个只读快照导出（现在只有内部的 `snapshot(handle)`）：

```go
type JobView struct {
    Kind        string      // process | inline | subagent（§A.2）
    Handle      string
    SessionID   string
    Description string      // 行标题
    BatchID     string
    State       string      // running | done | failed | killed（四态，区分失败与中止）
    ExitCode    int
    StartedAt   time.Time
    LogPath     string
    TailBytes   int64       // 已产出字节数
    Summary     string      // 有界摘要，完成回填的载体（§A.3）
    Notified    bool        // 该作业是否已回填过（幂等键，K-4）
}
func (g *asyncRegistry) SnapshotFor(sessionID string) []JobView
```

面板只读这份视图，**不参与任何执行路径**——避免面板成为第二个事实源。

---

## 6. 在途调用纳入打点表投影

对应你的第 2 点。好消息是机制已有 80%：打点表**本来就每轮重播进上下文尾部**（`async_run.go:126-128` 注释明确写了这件事）。

所以**不要另造 active stack**，只需把「在途并行调用」并入同一投影：

```go
// 现在：WorkTableRow{ Handle, Description, State }
// 改为（v2 字段规范见 §10.3，此处为最终形态）：
type WorkTableRow struct {
    Kind        string   // "process" | "inline" | "subagent"   ← 新增
    Handle      string   // 作业才有（内联/子代理同理，统一句柄）
    Description string
    State       string   // running | done | failed | killed
    Summary     string   // 有界摘要：退出码 + 行数 + 字节数 + 尾部（≤512B）
    BatchID     string
    Index       int      // 内联：wire 下标，供回执对账
}
```

> **v2 说明**：`Kind` 从 v1 的 `async|inline` 扩为 `process|inline|subagent`（子代理并入同一投影，见 §10.6）；`State` 从三态扩为**四态**（区分 `failed` 与 `killed`）；新增有界 `Summary`——它正是"完成回填"（V3）的载体，规则与生命周期见 §10.3。

这样压缩后模型仍能从投影里看到「我刚才派了 3 个并行的 read，其中 2 个已完成」，而不用复述工具内容。

**投影是视图，不是通道**——结果仍然必须回填 history（见 §1.4）。这条边界不能破。

> **v3 边界澄清（G-1 / D-3，权威版在 §A.3）**：
>
> | 边界 | 内容 |
> |---|---|
> | **不放宽** | **全文**仍只能经 `Manage(OpFetch)` 取回或 `read_tool_result` 引用取回——投影不得成为结果的唯一载体 |
> | **受控放宽一次** | 投影可以携带**完成行的状态 + 有界摘要**（退出码、行数、字节数、尾部 ≤512B）。这就是 V3 要的"完成即回填"，也是"回填的内容就是表格的内容"的落点 |
> | **硬约束** | K-1 回填**只影响投影**、不改写 history（⇒ 前缀稳定 J-1 结构上成立）；K-2 有界；K-4 幂等；K-5 不按墙钟猜状态 |
>
> 成本判据：投影按**轮数**重播 ⇒ 全文入投影的 token 成本 × 轮数；摘要 + 引用 ⇒ 常数级。载体备选（合成 history 条目）与启用判据见 §A.3。
>
> **完成行的生命周期（必须显式定义，否则无界膨胀）**：
>
> | 事件 | 行的变化 |
> |---|---|
> | 作业 `finish()`（exit 0） | `running` → `done`（带摘要），行**保留** |
> | exit ≠ 0 | `running` → `failed`（带退出码 + 尾部），行**保留** |
> | 被 kill | `running` → `killed`，行**保留**（区分"没跑完"与"失败"） |
> | 模型取回（`async_output`）/确认 | 行**划掉**（从投影移除，history 里的工具结果成为唯一事实） |
> | 无人取回且超出行数/字节预算 | 按 FIFO 淘汰**最老的完成行**，只保留一行汇总："N 个作业已完成未取回，handle 列表…" |
>
> **M4 更正（v3）**：v2 在这里写过"作业完成会划掉 running 行 ⇒ 这正是'子进程 exit 0 相当于自动 `todo_done`'的落地点"。**这个类比作废**：`todo_done` 划的是**打点表上的一行**、不派发任何进程；作业销项动的是**真派发的进程**（`finish()` 的终态迁移）。两者只有"立项 → 销项"这个**形态**同源，机制上没有共享——作业**额外**做到的是：把结果摘要留在表格里等取回。

> **本节已展开为详细设计（v3 权威版）**：数据模型、载体两案与五条约束（K-1..K-5：只影响投影 / 有界 / 全文走 fetch / 幂等 / 不猜状态）、完成行生命周期，见 **§A.3**；打点表的投影规则与端到端用例见 `docs/tool_calling_step0_contracts.md`。

---

## 7. 阶段、顺序与门（权威清单在打点表）

> 本章只给**顺序与门**；每条打点的落点、判据、依赖、验收在 `docs/tool_calling_step0_contracts.md`。

| 阶段 | 章节 | 门（进入下一步的条件） | 可回滚性 |
|---|---|---|---|
| **P0 契约落实** | §A | 无前置门（P0 内部第一项是**事实 2 的硬前置**：`RegisterTool` 加 meta 声明路径 + 给现有工具填 `Groups`——不填则一切按组派生的判定都读空） | 纯声明与文档，零风险 |
| **P1 契约下的实现** | §B | 与 P0 **同步**推进（用户裁定），无阶段门；但 §B.3（subagent）单独设门：先落 §B.1/§B.2 并跑一轮真实任务，确认 `Manage` 的四种 op 在 bash 上稳定 | 逐项可回滚（换壳不换状态机） |
| **P2 契约进 seele + loop 并发** | §C | **必须实测证明**"结果晚一轮"不可接受（§B 的批量扇出收益数据），否则不开工 | 独立 feat，可单独回收 |

### P0 工作项（详设 §A）

| # | 内容 | 为什么必须先做 |
|---|---|---|
| **K-0** | `RegisterTool` 加 meta 声明路径 + 给现有工具填 `Groups`（`registry_state.go:48-64`、`runtime_tools.go:264`） | **事实 2**：生产路径从不填 `ToolMeta`，`Groups` 运行时全空 ⇒ 一切按组派生的逻辑都读不到东西 |
| **K-1** | `JobTool` interface（`Add` / `Manage` + `done` op）写进代码 + 文档 | 契约本体；M2 的落点 |
| **K-2** | 行 schema 收敛（`Kind` / `Summary` / `Notified` / `Index`） | 回填与回执对账的载体 |
| **K-3** | bash 工具族（`bash` / `bash_read` / `bash_bg`）+ 权限表分流 | M1 的落点；模型选择面 |
| **K-4** | `ClassifyCommand` 服务端守卫（纯函数 + 表驱动） | `bash_read` 的存在前提（否则是提权口） |
| **K-5** | 回填规范（载体案 1 + 五条约束 K-1..K-5 + 生命周期） | V3 的落点 |
| **K-6** | `done` 双入口（运行体系被动 + 模型侧主动） | M3 的落点；只迁移一次 |
| **K-7** | 三份文档同步 | 契约可照着实现 |

### P1 工作项（详设 §B）

| # | 内容 | 验证 |
|---|---|---|
| **L-1** | 后台 bash 走 `Add`/`Manage`（`bash_bg`），回执**逐字段等价**旧路径 | 既有 async 测试全绿 |
| **L-2** | `inline` 作业执行体：goroutine 跑同一 `begin/finish` 状态机（读/搜扇出用它） | `AsyncPendingFor` 覆盖 `kind != process`；`killSession` 能清掉 inline 作业 |
| **L-3** | 批量读工具作业化：一次调用派发 N 个 `inline` 作业 ⇒ 回执含 N 个 handle | 派发即返回；**墙钟收益可量化**（这是 P2 门的判据数据） |
| **L-4** | **替代 `async_output`**（`Manage(OpFetch)`）——含 7 个迁移面 + 两条必须保留的性质 | `grep async_output` 零残留；消费式游标回归断言 |
| **L-5** | subagent 纳入契约（observe / kill / done；kill 保留已产出） | 模型面三能力可用；`fork_subagents` 语义从阻塞改派发 |
| **L-6** | 提示词 / 配置 / 文档迁移（三处 bash 名字 + `async_output` 下线） | 工具描述明写 `bash_read` 只允许只读命令 |
| **L-7** | 迁移面 7 项逐条核过（注册/权限/描述/开关/测试/文档/别名） | 无遗漏 |

### P2 工作项（详设 §C）

| # | 内容 |
|---|---|
| **S-0** | **门**：用 P1 的墙钟数据证明"结果晚一轮不可接受" |
| **S-1** | 契约模型同步进 seele（进程型工具成为框架一等范式） |
| **S-2** | seele loop 内并发工具调用（§1–§4 全量 + I-1..I-8） |
| **S-3** | `ToolCallInfo` 补 `ID` / `Index` / `BatchSize`（I-2 的前置缺口） |
| **S-4** | 批量前后沿（hook 单 goroutine、按 Index 序触发） |

> **独立立项（不在 P0/P1/P2 内）**：子代理**进程化**（OS 进程隔离）。它不解决 §A/§B 的任何缺口（那些只依赖句柄契约），收益是崩溃/配额隔离与真中断，成本是进程生命周期 + 结果序列化 + 数十个 `subagent_*` 测试面。

---

## 8. 风险与对策

| # | 风险 | 严重度 | 对策 |
|---|---|---|---|
| R-1 | **同轮 tool_call 间有隐式依赖**（read→edit） | 高 | 声明式策略无从知晓。**默认串行、白名单开并行**；且只对 `ro`/`rw` 开——`rw` 有 per-path 锁兜底，即使模型同时发 read+edit 也不会写坏 |
| R-2 | **Hook 配对静默错配** | 高（不可见） | P0-1 先修；I-2 断言 |
| R-3 | **application 层 start→complete 假定交替** | 高（工作量最大） | 由 §3.6「批量前后沿」缓解：hook 层恢复严格交替，UI 只需知道「这是 batch」。但 `handleToolStart`/`handleToolComplete`、`AttributeToolNarrationLocked`、`appendAssistantPlaceholderAfterToolLocked` 仍需逐处确认能处理「N 个 pending」。**这是实际工作量最大的一块，不是线程安全** |
| R-8 | **hook 被 N 个 goroutine 并发调用** | 高 | `hooks.go` 明写「回调同步调用，不要阻塞」。对策见 §3.6：hook 只在批量前后沿、单 goroutine、按 Index 序触发 |
| R-4 | UI 出现 N 个同时 running 的工具 | 中 | 按 Index 稳定排序渲染 |
| R-5 | 审批并发弹多张面板 | 中 | broker 按 ID 支持多笔（`pending map[string]`），机制没问题，但人只有一双手。`rw_desktop`/`ctl`/`adm` 串行已覆盖主要场景 |
| R-6 | **超时阈值等于放羊** | 中高 | 独立问题，见下 |
| R-7 | 并发槽耗尽 | 中 | 加 `maxPar` 上限 + 取槽超时显式报错（不静默降级成串行） |

### R-6 补充：超时不会被并行化改善，反而更该修

现状链路（**不是失效，是阈值太松**）：

| 层 | 默认值 | 位置 |
|---|---|---|
| registry `WithCallTimeout`（全工具） | `limits.tool_call_timeout` = **1800s** | `tools.go:194-200,362-369` |
| bash 同步 `scopedToolTimeout` | **30 分钟** | `router.go:620-636` |
| 合法配置 `0` | **无限制** | `limits.go:49` 注释 |

且 `router.go:620` 的注释说明 30 分钟是**故意**的：「旧 30s 兜底会掐断子代理的长命令」。

并行化让这个问题**更糟**：N 个调用共享同一个等待窗口，卡住的那个会把整轮拖满 30 分钟，而串行时至少前面几个已经出结果了。

---

### v2 新增风险（R-9 .. R-13）

| # | 风险 | 严重度 | 对策 |
|---|---|---|---|
| **R-9** | **投影从"视图"变成"结果载体"后 token 成本线性放大**（D-3） | 中高 | 表格只带**有界摘要 + handle 引用**；全文走取回；完成行有生命周期与 FIFO 淘汰（§6）；加"投影 token 预算"断言 |
| **R-10** | **作业泛化搅动既有 async 语义**（`asyncRun` 是久经测试的状态机） | 中 | 只**加字段**不改状态机；回归面明确：`async_exec_ab_live_test.go`、`async_progress_test.go`、kill/session 清理 |
| **R-11** | **自我声明式授权**（若采纳 D-5 的用户原案） | **高（安全）** | **否决**：授权依据不得由被授权者出具。改服务端分类（P0-5）；分类失败按写处理 |
| **R-12** | 作业化后结果"晚一轮"，模型可能误判为"没有结果"而空转/重复派发 | 中 | 完成回填必须显式标注"**已就绪，待取回**"；去重复用现成的幂等键 `asyncKey(sessionID, command)`（`async_exec.go:140`）；在途期不判 no-progress 停（`coordinator.go:567-577` 已实现） |
| **R-13** | **同一结果两处都是"事实"**（表格摘要 vs history 工具结果） | 中 | 表格行**到被取回为止**；history 里的工具结果是唯一事实；取回即划行（规则见 §6 生命周期表） |

## 9. 验收标准

**P0（契约落实）**

- [ ] `JobTool` 在代码里存在，且被至少一个实现满足（编译期断言）
- [ ] 三个 bash 名字在权限表里按预期分组（`bash`/`bash_bg` → `rw`；`bash_read` → `ro`）
- [ ] `bash_read` 守卫：**写命令不可能通过**（表驱动：`git commit` / `rm -rf` / `echo x > f` 必须被拒）
- [ ] 回填有界（K-2）且幂等（K-4）：100 个完成作业下投影长度有界；重复 `finish` 不产生第二次回填
- [ ] `done` 的两个入口（运行体系被动 / 模型侧主动）都到达同一终态，且**每个 handle 只迁移一次**
- [ ] 无 `ToolMeta` 填充遗漏（全量工具遍历断言 `Groups` 非空）——**硬前置**

**P1（契约下的实现）**

- [ ] `bash_bg` 派发回执与旧 `bash(background=true)` 逐字段等价
- [ ] `async_output` 下线：注册表 / 权限组 / 描述 / 文案零残留
- [ ] fetch 仍是消费式、observe 仍不推进游标（回归断言）
- [ ] subagent：模型面可 observe / kill / done；kill 后**已产出内容不丢**
- [ ] 派发一批作业后 history 逐字节不变（J-1：结构与完成时序无关）
- [ ] 批量读的墙钟收益可量化：N 文件并行读 vs 串行的对比（**P2 门的判据数据**）
- [ ] J-1..J-6 全部有测试且通过
- [ ] 既有 async 测试全绿（`async_exec_*` / `async_kill_*` / `async_probe_*` / `async_progress_*` / `work_table_async_*`）

**P2（契约进 seele + loop 并发，仅在门通过后）**

- [ ] I-1..I-8 全部有测试且通过
- [ ] 同一批调用的人为乱序完成实验下，history 逐字节一致
- [ ] 审批等待期间并发槽零占用（I-3）
- [ ] `Groups == nil` 的工具有效串行（I-7）

---

## 10. 子进程调用系契约（现状锚点与形态对照）

> **命题**：把"并行"从 loop 的职责改成**作业系统的属性**——工具调用只负责**派发**，作业自己跑、自己 exit、自己销项，结果经**投影回填**。seele 循环在 **P0/P1 阶段保持串行、零改动**（P2 才把契约上游化并做 loop 并发，§C）；这是新增一类工具调用范式（**子进程调用系**），不是 refactor。
>
> **契约本体在 §A.1**（interface：`Add` + `Manage`，`done` 是契约内的终态 op）。本章保留的是**证据**：这些形态在现有代码里各有什么锚点、缺什么，以及 v3 对 v2 表述的纠正。

### 10.1 契约形态与现有锚点（证据）

契约**不需要新建机制**——`asyncRegistry` 已经是它的执行体；§A.1 的工作是把它**命名并收敛成一个 interface**，并明确"串行 bash 不实现它"。

| 形态 | 语义 | 现有锚点 | 模型面 | 缺口 |
|---|---|---|---|---|
| **dispatch**（派发） | 起一个作业，**立刻**返回受理回执 | `asyncRegistry.begin`（`async_exec.go:170`）→ `attach`（:267）→ `go r.awaitAsync`；回执 `renderAccepted` | `Add` → `bash_bg` | — |
| **observe**（观察） | 只读看进度，**不推进游标** | `Router.AsyncRuns()` / `AsyncRunInfo`（`async_probe.go:24-58`）；`snapshot`（`async_exec.go:367`） | `Manage(OpObserve)` | **G-2** |
| **fetch**（取回） | 取增量，**推进游标**（消费式） | `advanceTail`（:380）+ `markCursor`（:415） | `Manage(OpFetch)`（**替代** `async_output`） | — |
| **kill**（终止） | 终止整棵进程树 | `killTarget`（:282）/ `killSession`（:305）/ `markKilled`（:328） | `Manage(OpKill)`（现 `async_kill`） | 对 subagent 无（G-3） |
| **done**（销项） | 作业结束 → 状态迁移 → **回填** | `finish`（:336）：`close(run.done)` + `notifyLocked()`（:458）→ `Events()`（:132）→ 投影 | 投影行 + `Manage(OpDone)` | **G-1** |

**三个已经白送的关键语义**（不要重造）：

1. **"作业收尾触发销项"** 的触发点就在 `finish()`：它 `close(run.done)` 并 `notifyLocked()`。缺的只是"回填内容"，不是"触发"。（**M4**：这**不是**"自动 `todo_done`"——todo 是打点表上的行、不派发进程，两者只是形态同源。）
2. **"在途作业未结束 → loop 不许收工"** 已实现：`coordinator.go:567-577`，无字节进展时若 `pendingAsyncForContext(ctx) > 0` 则**放行下一轮**，不判 no-progress 停。
3. **去重/幂等键已有**：`asyncKey(sessionID, command)`（`async_exec.go:140`）——同一命令重复派发可识别。

### 10.2 形态 → 工具面的映射（v3 纠正）

**v2 曾把"五个形态"当成需要在工具面上各起一个名字**（`bash init` / `bash status` / `bash done`…）。**v3 否掉这个方向**，理由有两条：

1. **形态是 `Manage` 的 op，不是工具名**：`observe` / `fetch` / `kill` / `done` 是同一个 interface 的四个操作（§A.1），把它们摊成工具名会让工具面按 op 数量膨胀。
2. **工具面按"执行体类别"分裂，而不是按"操作"分裂**（§A.4）：`bash`（串行）/ `bash_read`（只读）/ `bash_bg`（后台受管）。同一类别内的操作走 op 参数。

| 形态 | 工具面落点 | 现状锚点 | v3 结论 |
|---|---|---|---|
| `Add` | `bash_bg`（+ 批量读工具、`fork_subagents`） | `dispatchAsync`（`async_run.go:151`）→ `renderAccepted`（`async_exec.go:511`） | 换壳不换状态机 |
| `Manage(OpObserve)` | `bash_bg` 的 `op` 参数 | 只读探针已存在：`Router.AsyncRuns()` / `AsyncRunInfo`（`async_probe.go:43`），但**未注册为模型面** | **注册为 op**（v2 曾建议新增 `async_probe` 工具名——**作废**） |
| `Manage(OpFetch)` | 同上 | `async_output`（`async_tools.go:25`）→ `advanceTail` + `markCursor` | **替代掉 `async_output`**（§B.2） |
| `Manage(OpKill)` | 同上 | `async_kill`（`async_tools.go:98`）→ `killTarget` | 并入 op；subagent 亦可用（§B.3） |
| `Manage(OpDone)` | 内部钩子 + 模型面 | `finish()`（`async_exec.go:336`）**被动**；subagent 场景**主动** | 两个入口，同一状态机（§A.1 表） |

> **`done` 为什么不是"不需要工具"**：v2 写"被动形态，所以不需要工具"。这在 bash 场景下**碰巧**成立，但把它写成契约结论就错了——subagent 的 done 是**主动**的（subagent 自己调，或 main agent 调），必须有模型面入口。**契约动作不能因为某个实现里它是自动的就被降级掉。**

### 10.3 完成回填规范（G-1，V3）

**目标形态**（用户表述："子进程的结束划掉 active 下的内容，然后 append only 回填到上下文中，回填的内容就是表格的内容"）：

```
作业在跑：投影 = [ … running 行（handle/描述/字节数）… ]
      ↓ finish() → notifyLocked() → Events()
作业结束：投影 = [ … 完成行（handle/描述/退出码/行数/字节数/尾部摘要）… ]   ← running 行被"划掉"（状态迁移）
      ↓ 模型 fetch（async_output）或确认
取回后  ：投影 = [ … 该行消失 … ]   ← history 里的工具结果成为唯一事实
```

**字段规范**（`WorkTableRow` 在 §6 的基础上收敛）：

| 字段 | 用途 | 约束 |
|---|---|---|
| `Kind` | `process` / `inline` / `subagent` | 决定读取口与 kill 语义 |
| `Handle` | 唯一锚点 | 与 history 回执里的 handle **必须一致**（可对账） |
| `State` | `running` / `done` / `failed` / `killed` | **四态即可**；不需要更多 |
| `Summary` | 退出码 + 行数 + 字节数 + **有界尾部** | **硬上限**（如 ≤512 字节）；超限截断并标注"全文经 fetch 取回" |
| `BatchID` | 同批派发归组 | 已有（`begin` 的 batchID 形参） |

**"append-only"的准确落点**：回填走的是**投影重播**（`async_run.go:126-128` 注释已写明打点表每轮重播进上下文尾部），因此
- 对模型而言是"新增了一条信息"（append 观感）；
- 对 history 而言**什么都没改**（不 append、不改写）⇒ **前缀稳定**（I-1）自动成立，这也是 v2 相比 v1 的最大结构性优势。

**D-3 判据（为什么摘要而不是全文）**：投影按**轮数**重播，全文入投影 ⇒ token 成本 × 轮数；摘要 + 引用 ⇒ 常数级。而"回填的内容就是表格的内容"仍然成立——**表格那一行现在带着结果**，而不是只剩状态。

> **v3**：载体两案（案 1 投影重播 / 案 2 合成 history 条目）与五条硬约束（K-1..K-5）见 **§A.3**——**P0 取案 1**。

### 10.4 判据：谁作业化、谁保持同步（D-1 / D-2）

| 类别 | 形态 | 理由 |
|---|---|---|
| 读 / 搜（read/grep/glob，含**批量扇出**） | **两段式（M6）**：P1 作业化（`inline`）先拿收益 | 无共享状态、可并行；代价是结果晚一轮；P2 若"晚一轮不可接受"，由 seele loop 并发接管（§C） |
| 长命令（bash） | **作业化（`process`）** | 已是现状 |
| 子代理（fork） | **作业化（`subagent`）** | 天然长作业，且这样才能被 main 观察/干预（§10.6） |
| **写 / 改（write/edit/bash 写类）** | **保持同步 tool call（串行）** | 写完必须立刻知道成败才能继续推理（"改完再读"是紧耦合因果链）；作业化会让写失去同步回执 → **D-2 的落地形态：写不作业化** |
| 极短只读探测（如单文件 read） | 同步亦可 | 作业化有派发+回填两次 round-trip 的固定开销；**只在批量扇出时收益为正** |

> **D-1 的代价必须记账（M6 两段式）**：作业化后读操作的**结果至少晚一轮**回到上下文。P1 用作业派发先拿收益（零框架改动）；**若实测证明"晚一轮不可接受"**，才启动 P2 的 loop 并发（§1–§4 + seele feat）。**不要先验选型**——P2 的门就是这份实测数据。
>
> **D-2 的落地形态**：写/改**不作业化**（同步、串行），因此"写串行"是通过**不派发**实现的，而不是通过给写加锁实现的——比 §2.1 里"给 `rw` 加全局串行锁"更简单，且不动 fs actor。

### 10.5 bash 的形态与守卫（M1 / D-5）

**形态在 §A.4**（工具级分裂：`bash` / `bash_read` / `bash_bg`）。本节只保留**为什么守卫一道都不能省**的论证，因为用户原案里的"自审"方向必须被明确否决：

用户原案：**提示词约束 + agent 看自己的 json output + agent 做语法审查**。这条必须否决，理由是它把**授权依据**交给了**被授权者**：

1. 模型的 `json output` 是**主张**，不是**证据**。授权系统不能拿主张当依据——否则一次提示词注入 / 一次上下文漂移就直接提权。
2. "agent 语法审查"更弱：让被审查者出结论，且审查器与被审对象是同一个模型（同一套失效模式）。
3. 这类设计在安全上属于**self-declared privilege**，与 `pathgate`（`seelebridge/security/pathgate.go`）那种"服务端按规则判定"的既有风格**不一致**——它会成为整个安全模型里唯一的"可被语言说服的"环节。

**M1 之后的落点（工具级分裂 + 服务端守卫）**：

| 层 | 做法 |
|---|---|
| 分裂 | 三个名字按**组**分流（`bash`/`bash_bg` → `rw`；`bash_read` → `ro`），组是权限的**单一事实源**，不新增自声明字段 |
| 判定位置 | **服务端**，`seelebridge/security`：纯函数 `ClassifyCommand(cmd) bool` |
| 依据 | **命令首词白名单**（`ls` / `cat` / `git status` / `grep` …）+ 无写重定向（`>`/`>>`/`tee`）+ 无写子命令（`git commit`、`npm install`、`rm`…） |
| 失败模式 | **分类失败 → 拒绝**（要求改用 `bash`），**不静默降级成执行**（与 `router.go:477` 同源） |
| 可审计 | 分类器是纯函数 + 表驱动测试；**agent 说什么都不影响判定** |

> **为什么"分裂"不能替代"守卫"**：分裂只决定"这个名字挂在哪个组"，不决定"这条命令能不能用这个名字"。没有守卫时，`bash_read` 就是一把挂着只读名牌的 `bash`——**分裂反而让提权更隐蔽**（模型会优先选免打断的那个名字）。这两件事必须同时做。

> 补充：若 bash 命令是**纯只读 + 短**，它也没必要作业化——同步更快。**作业化的判据是"长/可并行"，不是"只读"**（§10.4）。

### 10.6 子代理接入契约（V7 / G-3 / M3）

**现状（已复核）**：`fork_subagents`（`seelebridge/fork/tool.go:44`）是**一次阻塞调用**——main agent 的 tool call 要等全部子代理与 summary 节点跑完才返回（`seelebridge/fork/README.md` 时序图："Waiting for output 期间即预期行为"）。执行体是**进程内** goroutine + worktree 隔离的 Session，模型侧**没有任何查看 / 干预 / 终止子代理的工具**（G-3）。

**接入方式（零进程化）**：把 subagent 登记为 `Kind = subagent` 的作业，于是**同一句柄契约**直接给出三个新能力：

| 诉求 | 实现 | 依赖进程化吗 |
|---|---|---|
| main agent **主动查看** subagent 内容 | `Manage(OpObserve)`（泛化只读探针）+ 复用既有 detail 读口 | **不需要** |
| main agent **干预 / 提前终止** | `Manage(OpKill)`：终止 = 回收 worktree + 标 `killed` 行 + **保留已产出内容**（不是丢结果） | **不需要** |
| subagent **自己销项** | `Manage(OpDone)`，**主动**（subagent 调）；main agent 亦可主动调 | **不需要** |
| 崩溃/配额隔离、真中断 | 需要 OS 进程 | 需要（**独立立项**） |

**结论**：先做"句柄契约上的子代理"（零进程化、改动面最小、直接消除 G-3），**进程化单独立项**——V7 的诉求只依赖**句柄契约**，与执行体形态正交；把它们捆在一起会把"最小改动"变成"最大改动"。

> **M3 的落点**：`done` 在 subagent 场景是**主动**的，这是 v2 "被动形态"表述最直接的错处——subagent 与 main agent 都需要一个模型面的 `done` 入口（§A.1 表）。

**工作量提示**：这一步牵动 `fork` 的 DAG 编排、task 幂等登记、summary 节点、worktree 生命周期与 merge-back（`seelebridge/fork/`、`runtime_plan.go`）——是 P1 里唯一的大改动（§B.3）。

### 10.7 seele 侧改动清单（按阶段）

| 阶段 | seele 改动 | 性质 |
|---|---|---|
| **P0 契约落实** | **0** | 契约的 interface、行 schema、bash 工具族、守卫全在 seelex 侧 |
| **P1 契约下的实现** | **0** | 并发仍发生在作业系统内，loop 串行；hook 仍单 goroutine、按 wire 序触发（§3.6 的框架硬约束天然满足） |
| **P2 契约进 seele + loop 并发** | **两件事**：① 契约模型上游化（进程型工具成为框架一等范式）；② loop 内并发工具调用（§1–§4 全量 + `ToolCallInfo.ID`/`Index`/`BatchSize` + 批量前后沿） | **feat**（加字段 / 加能力），不是 refactor |

> **"seele 改动 = 0" 的适用范围被收窄了**：v2 把它写成了永久结论，v3 更正为**只对 P0/P1 成立**。P2 明确要改 seele，而且那正是 v1 那批设计的去处。
>
> P0/P1 阶段这条"零改动"**已被现状锚点证实**：`finish()`、`notifyLocked()`、`AsyncPendingFor`、投影重播、幂等键——**触发链和收工链都已经在，缺的只是回填内容、`Manage` 的四个 op、以及 subagent 的看/停口**。

### 10.8 v2 不变量（J-1 .. J-6）

| # | 不变量 | 验证方式 |
|---|---|---|
| **J-1** | **回执 append-only**：同一批派发无论作业多快完成，history 逐字节不变 | 派发一批后立即断言 history；再等作业全完成，断言 history 仍相同 |
| **J-2** | **回填只影响投影**：作业进度/完成不改写 history | 对 history 做快照比对 |
| **J-3** | **观察只读**：连续 `Manage(OpObserve)` N 次后 `Manage(OpFetch)` 仍能取到全部增量 | 游标断言（§5.1 的既有性质，补回归 → **已落地** `TestObserveDoesNotAdvanceCursor`） |
| **J-4** | **销项唯一且不可逆**：每个 handle 恰有一次终态迁移（done/failed/killed） | 状态机断言 + kill/finish 竞态测试 |
| **J-5** | **回填有界**：投影增量 ≤ 预算；完成行数 ≤ M；超限按 FIFO 淘汰 + 汇总行 | 注入 100 个已完成作业，断言投影长度 |
| **J-6** | **在途不收工、不重复派发**：在途作业 > 0 时 loop 不判 no-progress 停；同一 `asyncKey` 不重复起作业 | `coordinator.go:567-577` 既有行为 + 幂等断言 |

> J-1 是 v2 的**存在理由**（对应 v1 的 I-1）：v2 用"派发与结果解耦"把前缀稳定从"需要设计排序键"降级成"结构上不可能破坏"。
>
> **与 K-1..K-5 的关系**：K-x（§A.3）约束**回填**（投影侧，行有界、幂等、不改 history）；J-x 约束**作业与 history 的关系**（派发/取回/销项）。两者不冲突：J-1/J-2 正是 K-1 的全局形态。

## 11. 一句话总结

> **命题不变**：把"并行"从 loop 的职责改成**作业系统的属性**——工具调用只负责**派发**，派发即结束；作业自己 exit、自己销项，结果经**投影回填**。
>
> **v3 的修正是"分段"与"两处纠正"**：
> - **P0 契约落实**：一个 interface（`Add` + `Manage`，`done` 是终态 op）、一张行 schema、一条回填规范、**一族 bash 工具**（`bash` / `bash_read` / `bash_bg`）加**一道服务端守卫**。
> - **P1 契约下的实现**（同步推进）：后台 bash 换壳到 `Add`/`Manage`、**替代掉 `async_output`**、subagent 纳入契约（observe / kill / **主动 done**）。
> - **P2 契约进 seele + loop 并发**（feat）：**"seele 改动 = 0" 只对 P0/P1 成立**；P2 明确要改框架，而且是加法。
>
> **两处纠正**：① **bash 分化是"工具级分裂"**，但**分裂不能替代守卫**——没有服务端 `ClassifyCommand`，`bash_read` 就是挂着只读名牌的 `bash`（且模型会优先选免打断的那个名字，提权更隐蔽）；② **`done` 不是"被动形态"**，它是契约的终态操作，只是 bash 场景由运行体系被动触发、subagent 场景由 subagent 或 main agent 主动触发。
>
> **回填用摘要 + 引用，不用全文**：投影按轮数重播，全文入投影按轮数线性烧 token；"回填的内容就是表格的内容"依然成立——那一行现在**带着结果摘要**。载体两案与五条约束（K-1..K-5）见 §A.3。
>
> **写/改不作业化**（同步、串行），所以"写串行"是靠"不派发"实现的，不需要给 `rw` 加全局锁，也不动 fs actor。
>
> **两条不能妥协的线**：① 授权依据不得由被授权者出具——`bash_read` 的守卫必须在服务端，**否决 agent 自审**；② hook 只能单 goroutine、同步触发（框架硬约束，P2 也要守）。
>
> **待你拍板**：**D-7（interface 两函数命名）** 与 **D-6（bash 拆几个）** 是新增待确认项；D-1/M6 的两段式已按你的裁定写入，P2 的门是 P1 的实测扇出数据。
