# 角色权限登记：权限向量、四级优先级、键与并发纪律

> 状态：**目标设计（未落地）**。本文只写长期有效的架构事实与判据，逐节标注"当前实现"与"目标设计"的差距。
> 日期：2026-10-23
> 关联：[`agent-permission-subjects.md`](agent-permission-subjects.md)（主体 × 路由组 × 位的稳定口径与错误语义）、[`permission-path-gating.md`](permission-path-gating.md)（路径归属判定）。

## 1. 生态位与边界

本文回答一个问题：**某个会话里的某个角色，此刻持有一份怎样的权限向量**——向量的词表、层的优先级、登记的键与并发纪律、会话加载时的恢复顺序，以及账号维度三格设置（`context_window` / `permission_vector` / `default_plugin`）落在哪里。

它**不**回答（这里只引用，不另立一份）：

| 问题 | 唯一事实源 |
| --- | --- |
| 主体怎么判（`root/sub/emp_ro/emp_rw`）、无位怎么办、错误怎么报 | [`agent-permission-subjects.md`](agent-permission-subjects.md)、`backend/limit/policy/policy.go` |
| 路由组与位值的字面量、逐格装配的校验口径 | `contract/dto/permission.go`（`PermissionGroupNames` / `NormalizePermissionGroups`） |
| 权限矩阵（组 × 权限类型）与一次调用需要**一组**权限的判定 | `docs/arch/permission-assembly.md`（并行工作项交付，本分支尚未合并；因此此处按路径引用而不做链接） |
| 路径是否越出项目根 | [`permission-path-gating.md`](permission-path-gating.md) |
| 权限档位（`manual/edit/auto/full`）的落点与持久化 | `backend/limit/tier/tier.go` |

一句话分工：**矩阵说"某个用户组能有哪些列"，本文说"这个会话的这个角色此刻被授予了哪些列"**——它只能是矩阵某一行的列子集（§7）。

## 2. 权限向量的词表

一个**权限向量**是「`PermType` → 授予」的稀疏映射。标签集合沿用既有权限词表，不新造第二份：

| `PermType`（规范名） | 语义 | UI 别名 | 现状 |
| --- | --- | --- | --- |
| `ro` | 读簇：不改任何共享状态 | `read` | 已实现（`PermissionGroupRO`） |
| `rw` | 写簇：项目文件 + 自有工作台 | `write` | 已实现 |
| `rw_session` | 写簇：本会话可变 transcript | `session` | 已实现 |
| `rw_desktop` | 写簇：共享外设（一块桌面） | `computeruse` | 已实现 |
| `ctl` | 叫停 loop 簇：结束 / 挂起 / 派生 / 装载执行结构 | `control` | 已实现 |
| `adm` | 属主簇：改变能力面本身 | `admin` | 已实现 |
| `ssh` | 允许把工具调用打到远端执行面（独立于操作类型） | `ssh` | **目标**：`permission-assembly` M2 才进词表 |
| `ssh.userspace` | 允许远端访问项目根之外的用户空间 | `userspace` | **目标**：同上 |

纪律：

1. **规范名进存储，UI 别名只出不进**。别名是展示层的那一个词（`read/write/session/computeruse/control/admin/ssh/userspace`），前端渲染用；任何写入路径只接受规范名。别名表与规范表一对一，放在同一处，不散落。
2. **未知标签必须报错，不许当 0 位**。既有判据已把这条写死在 `contract/dto/permission.go` 的 `ValidPermissionGroup` / `NormalizePermissionGroups`（注释里给了理由：一个拼写错误在运行时等价于"这一格没分配"，且没有报错面）。登记侧复用同一处校验，不另写一份白名单。
3. **授予 = (Effect, Bits)**。`Bits` 为 0 时用该类型的既有 `Mode`（与矩阵的 `Grant` 同形）。
4. **`0` 位 ≠ 未出现**。显式写 `{"rw": 0}` 是"这一族明确收回"，与"这一层没说话"是两件事——`NormalizePermissionGroups` 的注释已经把这条钉死，§3 的逐标签优先完全依赖它。

## 3. 四级优先级：逐标签取首个出现者

层序（高 → 低）：

```
user_allow  >  agent_dispatch  >  session_default  >  global_default
```

每层贡献一份**稀疏**的 `map[PermType]Grant`。合并规则是**逐标签取首个出现者**：

```
for label in vocabulary:
    for layer in [user_allow, agent_dispatch, session_default, global_default]:
        if label ∈ layer:            # "出现"，含显式 0 位 / deny
            vector[label] = layer[label]; break
    else:
        vector[label] = deny          # 四层都没出现 = 缺格 = deny（与矩阵同义）
```

**这不是"整份覆盖"。** 高层只接管它显式点到的标签，其余标签继续从低层取——否则"派发时只想加一条 ssh"会把 `ro` 也一起抹掉（见下面示例 B）。

各层的事实源：

| 层 | 事实源 | 现状 |
| --- | --- | --- |
| `global_default` | `backend/limit/policy/policy.go` 的 `DefaultPermissionConfig()`（组表 + 主体表 + `missing_bit` + `rules`），叠加 `config/seelex.yaml` 的 `permission.*` 覆盖 | 已实现（"只覆盖不替换"） |
| `session_default` | `backend/limit/tier` 的会话档位：`Deps.EffectiveTier`（会话槽优先，未选择回退进程默认），会话级持久化走 `session.SessionSettingPort` | 已实现（档位是声明式覆盖层，从不新增 allow、从不触碰 deny） |
| `agent_dispatch` | 派发时下发的角色声明：`contract/dto/agentteam.go` 的 `RoleSpec.{ToolsPolicy, PermissionGroups, Plugins}` → `Runtime.AssignEmployeePermissions` | 部分实现；键有缺陷（§4） |
| `user_allow` | 审批面的人类显式放行（`ApprovalBroker` / `SetPermissionAutoApprovalFor`） | **只有 once 与会话级 full 两种，台账未持久**（`agent-permission-subjects.md` §7） |

### 3.1 冲突示例

**示例 A：四层各点一个标签**

| 层 | 该层写的内容 |
| --- | --- |
| `global_default` | `{ro: allow, rw: allow, ctl: ask}` |
| `session_default` | `{rw: 0 位}`（本会话显式收回写） |
| `agent_dispatch` | `{ssh: allow}` |
| `user_allow` | `{rw: allow, scope: once}`（人类刚在审批页点了一次放行） |

结果：`ro=allow`（global）、`rw=allow`（user_allow，`scope=once` 用完即弃、下一次回到 session 的 0 位）、`ctl=ask`（global）、`ssh=allow`（dispatch）、其余 deny。

要点：`user_allow` 必须带 `scope`（`once` / `session`）。不带作用域，§3 的"首个出现者"会把一次点头变成永久放行——那不是提高优先级，那是把审批面废掉。

**示例 B：为什么不能整份覆盖**

`global_default` 把八个标签全配齐；`agent_dispatch` 只写 `{ssh: allow}`。

- 正确（逐标签）：只有 `ssh` 被判成 dispatch 的决定，其余七个仍从 global 取。
- 错误（整份覆盖）：其余七个变成"缺格"→ 全部 deny → 这个角色连 `ro` 都不剩。这类 bug 的症状是"派发一次之后员工突然什么都不能做"，而根因是一次覆盖。

**示例 C：收紧的方向**

`global_default` 给 `rw: allow`，`session_default` 写 `rw: deny`。结果 `rw=deny`：高层先命中，低层的 allow 不得穿透。**高层的 permit 不会放宽低层没给的标签**（缺格在低层本来就不存在 allow），二者合起来就是单调性判据 A3/A4。

## 4. 登记的键与并发纪律

### 4.1 键是 `(sessionID, role)`

**不许**用「角色名派生的主体 id」（现实现的 `emp_<role>`）当键。现实现的缺陷是可以逐行指出的：

| 事实 | 出处 |
| --- | --- |
| 员工权限表 `employeePermissions` 的键是 `toolspermission.Subject`（= `emp_<role>`），**不含会话** | `backend/tool/registry/registry_state.go`（`PermissionGate` 字段与 `SetEmployeePermissions`） |
| 该表与判定快照 `cfg.Subjects[subject]` **两份**同时维护，写入时**后到先得** | 同上 |
| 同一件事还有**第二份**判据：`ensureEmployeeSubject` 在判定路径上补默认条目，且**先到先得**（`if _, exists := state.cfg.Subjects[subject]; exists { return }`） | `backend/tool/registry/permission_policy.go` |
| 后果 | 两个会话用同一角色名、配不同格：一个被另一个静默覆盖；而"谁赢"取决于走的是哪条路径（装配期分配 vs 判定期补默认）——**一份判据两处实现，方向还相反**（AGENTS §8） |

改成 `(sessionID, role)` 后，这两条路径收敛成一处：登记表是唯一事实，判定快照只是它的一次派生。

### 4.2 每份条目自带锁，禁止全局裸 map 并发写

```
type Entry struct {                 // 一个 (sessionID, role) 的全部状态
    mu     sync.RWMutex
    vector map[PermType]Grant       // §2 的向量
    loaded bool
    degraded string                 // 降级原因（§5）
}

type Registry struct {
    mu      sync.RWMutex            // 只保护下面这张**指针表**，不保护条目内容
    entries map[key]*Entry
}
```

纪律：

1. **条目内容只由条目自己的锁保护**；`Registry.mu` 不跨条目、不做"大锁串行"。
2. **禁止在无锁的全局 map 上写**。即便加了锁，若键不含会话，仍然是"两个会话互相覆盖"——锁只解决数据竞争，不解决键设计。
3. **判定期间不持锁**：取一次 `*Entry` 指针快照后放锁，再算向量；审批（阻塞、等人）绝不能在持锁期发生。这与 `roleSessionHandle.roundGate` 的"叶子锁"同一条纪律。
4. **不把运行时登记写回判定快照**。`tools.PermissionConfig.Subjects` 是装配期的**一次性**配置物（`PermissionGate.Set` 重建 checker 用），把活的运行时态写进去就是 §4.1 那个缺陷的成因。目标态：快照由登记表派生一次，登记变更后**显式**重建，而不是"谁在判定路径上顺手补一条"。

### 4.3 与文件锁的关系（别把两者混为一谈）

- 数据根文件锁是 `store/root/data_root_lock.go` 的 `lock.owner`（`AcquireDataRootLock` / `DataRootLockedBy`），它只回答一件事：**同一数据根是否已被另一个进程占用**。它不给任何单条记录上锁，也不保护进程内 map。
- 因此登记表的进程内并发安全**不能**借文件锁：多进程互斥 ≠ 多 goroutine 安全。自有锁是必需的，不是冗余。
- 落盘只发生在两处：会话加载时读回、角色设置变更时写。写走既有原子写路径（同 `store/workspace` 的 `secretStore` 那套：独立锁 + 原子写），不新造第三套。

## 5. 会话加载时的恢复顺序与失败降级

顺序即语义，一步失败不得让后面几步停下：

| # | 步骤 | 失败时的动作 |
| --- | --- | --- |
| 1 | 取数据根文件锁（`AcquireDataRootLock`） | **显式报错，不进会话**。这是唯一"不降级"的一步：两个进程同时写同一数据根的后果比打不开会话更贵 |
| 2 | `global_default`：`DefaultPermissionConfig()` + `config/seelex.yaml` 覆盖 | 配置坏 → **显式报错，不静默**。静默会让每个主体变成"无授权"（`policyReady` 存在的理由） |
| 3 | `session_default`：读会话级设置（含磁盘） | 读失败 = "未选择" → 退进程默认。脏值**不写会话槽**，但视图快照仍要重算（`tier.Deps.ApplyStored` 的口径：看着宽实际窄只是别扭，"以为要问人其实免审"是安全问题） |
| 4 | 角色登记：按 `(sessionID, role)` 读回向量 | **单条坏 → 跳过该条并标 `degraded`**，不整份丢；**整份读失败 → 视为空**，绝不回落到 root 全位 |
| 5 | `agent_dispatch`：下一次派发时重建 | 不参与加载恢复：登记表只存"派发时下发的向量"，不存"从派发反推出来的结论" |

两条不变量：

1. **降级只降不放**。任何一条读失败的最低档是"退到更严"，不是"退到 root"。现存的 `policy.ClassForToolsPolicy` 把未识别的 `ToolsPolicy` 映射成 `root`（继承宿主默认），登记表**不得**复用它这条默认分支——那正是"读不懂就当管理员"的经典形状。
2. **快照一致**。快照里的 `permission_vector` 必须等于该会话真正生效的那一份，切会话与新建会话两条路径都要重算（与 `tier.Deps.SyncViewLocked` 同一条不变量；历史缺陷是"进入新会话漏了重算，chip 显示上一个会话的档位"）。

## 6. 账号维度设置：三格落在角色设置里的形状

```
RoleSettings {
    session_id, role            // §4 的键
    context_window              // 账号维度覆盖，键 = 账号名
    permission_vector           // §2 词表
    default_plugin              // 该角色回合起手的插件装配；空 = 继承宿主
}
```

三格各自的**家**（都不新开第二份）：

| 格 | 落点 | 口径 |
| --- | --- | --- |
| `context_window` | 唯一判据 `backend/limit/accountwindow.Validate`（挡位 200k/400k/600k/1M + 自定义，上界 10M）；覆盖层 `backend/engine/account` 的 `WindowOverlay`（落 `runtime-limits.json`） | 优先级：账号级覆盖 > `limits.context_window` > 账号档遗留键。角色设置是**同一张覆盖表的第二个写入口**，不新开文件、不新写覆盖解析。另：`config/accounts.example.yaml` 明确"账号档只放凭据与模型参数，不放运行配置"——所以 `context_window` 不进账号条目 |
| `permission_vector` | 本文件 §2/§3；校验走 `dto.NormalizePermissionGroups` | 写入侧报错优于运行时静默 0 位 |
| `default_plugin` | 角色回合的装配解析面 `backend/engine/assemble/runtime_role_plugins.go` | 语义 = 该角色起手的装配集合，与 `RoleSpec.Plugins` 同一口径；冲突时 **dispatch 显式声明优先**于角色默认；两者都空 = 空集 = 不覆盖（继承宿主）。插件只回答"能用哪些能力包"，**权限不随插件走**（同文件头部三条边界） |

现状标注：`default_plugin` 目前只有**进程级**启动基线（`wiring/bootstrap/plugins.go` 的 `ActivateDefaultPlugin`）与每会话插件数上限（`backend/limit/limits.go` 的 `DefaultPluginsPerTeammate`）；角色级 `default_plugin` 是新增项，落地时不得再造一条"启动基线"旁路。

## 7. 与权限矩阵的关系：向量是矩阵某一行的列子集

`docs/arch/permission-assembly.md`（并行工作项交付）把权限表达成「行 = 用户组 × 列 = 权限类型」的矩阵，格 = `(Effect, Bits)`，缺格 = deny。本文的向量与它同词表、同格形状，差别只在**谁在问**。四条由关系推出的纪律：

1. **不另立词表、不另立 `Effect` 取值**。向量标签集合必须能由 `dto.PermissionGroupNames()`（扩展后含 `ssh` / `ssh.userspace`）派生出全部；`Effect` 只有 `allow/ask/deny`。
2. **向量不能超出矩阵行**。登记表是矩阵的**子集筛选器**，不是旁路：一个标签若在该用户组的那一行里是缺格/deny，向量里给 allow 只能是错误（或被 §3 的高层显式放宽且可审计）。
3. **缺格 = deny 在两层同义**（矩阵同一条），所以 §3 的"四层都没出现 → deny"不是新规则，是把它落到层叠上。
4. **`ssh` 标签必须先扩词表再开写**。在 `permission-assembly` M2 落地前，`ValidPermissionGroup` 会直接拒 `ssh`——这是对的：先写后补的结果是读回时"未知标签→0 位"，静默变成"没给"，而用户以为给了远端权限。

## 8. 判据

| 编号 | 判据 | 验证方式 |
| --- | --- | --- |
| A1 | 词表唯一：向量标签集合由 `dto.PermissionGroupNames()` 派生，仓库内无第二份列表 | 代码检查 + 用例遍历词表 |
| A2 | 逐标签：高层只写一个标签时，低层其余标签原样生效（不是整份覆盖） | 示例 B 的表驱动用例 |
| A3 | 全序：同一标签只被 `user_allow > agent_dispatch > session_default > global_default` 中最高出现层决定 | 逐层单变量用例 |
| A4 | 缺格 deny：四层都未出现的标签判 deny，不回落到矩阵行的 allow | 空登记下全 deny |
| A5 | `0` 位 ≠ 未出现：显式 `{rw: 0}` 收掉该标签，且仍可被更高层改写 | 两个相邻用例 |
| A6 | 键隔离：`(s1, arch)` 与 `(s2, arch)` 各自独立；同角色不同格并发写不互相覆盖 | 并发用例 + `-race` |
| A7 | 无裸写：登记 map 只在持锁临界区读写，`go test -race` 无 `concurrent map writes`；判定与审批不持锁 | `-race` + 锁序检查 |
| A8 | 降级只降不放：任何读失败退默认或跳过单条，绝不退化成 root 全位 | 注入坏条目的用例 |
| A9 | 快照一致：快照里的 `permission_vector` == 该会话生效向量（切会话 / 新建会话后即重算） | 快照对照用例 |
| A10 | `ssh` 标签先词表后向量：词表未扩展时写 `ssh` 必须报错，不得静默 0 位 | 报错用例 |

## 9. 分期

| 期 | 内容 | 出口 |
| --- | --- | --- |
| **R0（本次）** | 本文：词表、四级优先级、键与并发、恢复顺序、账号维度三格、与矩阵的关系、判据 A1–A10 | 文档评审通过 |
| R1 | registry 骨架：键 `(sessionID, role)`、条目自带锁、优先级唯一一处判据函数、单测 + `-race` | A2/A3/A4/A5/A6/A7 绿 |
| R2 | 落盘与恢复：接 `session.SessionSettingPort` 形状的会话级设置端口，恢复顺序按 §5 | A8/A9 绿 |
| R3 | 接线 `PermissionGate`：删掉"判定路径上补默认条目"的那份派生，`cfg.Subjects` 退回纯快照（由登记表显式派生一次） | 既有权限测试零回归 |
| R4 | 账号维度三格：`context_window` 复用 `WindowOverlay`，`default_plugin` 接角色回合装配解析面 | 手工冒烟 + 用例 |
| R5 | 前端向量/矩阵编辑器（与 `permission-assembly` 的 M4 合流） | 手工冒烟 |

分期纪律：**在 R1–R3 落地前，产品行为保持不变**；任何"表达不出来就先放宽"的迁移都是禁止的——表达不出来就按**更严**落地（与 `permission-assembly` §4.1 同一条）。

## 10. Review 清单

1. 词表有没有第二份？（`dto.PermissionGroupNames` 之外出现的标签列表都是缺陷）
2. 优先级是否**逐标签**？凡是"整份替换"的实现都要找回来。
3. 键里有没有 `sessionID`？只有 `emp_<role>` 的一律打回。
4. 有没有在判定路径上写判定快照？（`ensureEmployeeSubject` 那一族）
5. 有没有"读失败 → root"的默认分支？
6. `user_allow` 有没有 `scope`？没有就是永久放行。
7. 快照与生效向量是否同源同值（切会话、新建会话两条路径都查）？
8. 新增标签有没有先扩词表、再开写、最后补判据？

## 11. 验证

```powershell
go build ./...
go vet ./...
go test ./backend/tool/registry/ -count=1
go test ./backend/tool/registry/ -race -count=1
go test ./backend/limit/... -count=1
```
