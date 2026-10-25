# 权限装配：用户组 × 权限类型（矩阵，不是阶梯）

> 状态：**产品已实现角色权限向量；本文记录矩阵模型与落地边界**。2026-10-24 起 SSH 只有
> 一枚能力，路径白名单不再作为独立权限；2026-10-25 起命令执行从 `rw` 拆为公开的
> `bash` 能力，旧 `admin` / `adm` 读取时迁移为 `bash`。早期孤立 MVP 保留在
> [`../2026-10-23-permission-assembly-mvp/`](../2026-10-23-permission-assembly-mvp/) 作为历史记录。
> 日期：2026-10-23
> 关联：[`ssh-tool-transport.md`](ssh-tool-transport.md)（环境路由与传输策略）、[`ssh-tool.md`](ssh-tool.md)（连接基座）

## 1. 现状（已核对到行）

| 事实 | 出处 |
| --- | --- |
| 主体 4 类：`root` / `sub` / `emp_ro` / `emp_rw` | `backend/limit/policy/policy.go:69-80` |
| 内部路由组 8 个：`ro` `rw` `rw_session` `rw_desktop` `ctl` `adm` `bash` `ssh`，每组带 `Mode`(所需位) + `Default`(allow/ask) + `Match`(工具名 glob) + `Resource` | `DefaultPermissionGroupList()`（同文件，唯一事实源） |
| 用户权限标签 7 个：`read` `write` `session` `computeruse` `control` `bash` `ssh`；`adm` 只承载内部能力管理，不是用户可选项 | `contract/dto`、`gui/frontend/dist/role-settings.js` |
| 位 3 个：`BitRead` / `BitWrite` / `BitExecute` / `BitAll` | `policy.go:105-108`、`contract/dto/permission.go` |
| **档位是权限向量预设**：`manual` → `edit` → `auto` → `full`；`TierVector` / `TierCells` 决定授予哪些组，危险命令 `deny` 不受档位影响 | `backend/limit/policy/tiers.go` |
| 工具簇属由路由组表**派生**（不另建表） | `backend/limit/policy/tool_meta.go` |
| 判定在链上的位置：permission 中间件（第 3 层） | `backend/tool/registry/registry_state.go:38-44`、`:481` |

**问题**：阶梯是**全序**。它只能表达"比上一档更松"，永远到不了 `full` 之外的形状。用户要的是**正交子集**——"只给 ssh"、"只给 write + ssh"。这两件事在阶梯上无解：`{write, ssh}` 里没有 read，而阶梯的任何一档都默认带着 read。

而且**根本没有 ssh 这一类**：现有 6 个组是"改不改共享状态"的划分，与"本地还是远端"无关。

## 2. 新模型：矩阵

```
             ro   rw   rw_session  rw_desktop  ctl  adm*  bash  ssh
root         ✓    ✓      ✓           ✓         ✓    ✓     ✓     ✓
sub          ✓    ✓      ✗           ✗         ✗    ✗     ✓     ✓
emp_ro       ✓    ✗      ✗           ✗         ✗    ✗     ✗     ✓
ci-robot     ✗    ✓      ✗           ✗         ✗    ✗     ✓     ✓   ← 自定义组：write+bash+ssh
viewer       ✓    ✗      ✗           ✗         ✗    ✗     ✗     ✗
```

- **行 = 用户组**（`GroupID`）：既有 4 个主体 + 自定义组；
- **列 = 权限类型**（`PermType`）：公开的 7 枚用户标签；`adm*` 是内部路由组，
  继续承载插件 / Skill / MCP 等能力管理工具，但不出现在角色权限候选栏；
- **格 = 授予**（`Effect`: allow/ask/deny，可带位）：**任意格子独立配**，没有"必须包含前缀"的隐含规则；
- **缺格 = deny**（默认拒绝，不再"先 ask 到底最后全接受"）。

### 2.1 一次调用需要**一组**权限，不是一个

这是本设计最关键的一点，也是"只支持写权限和 ssh 权限"能被表达的原因：

| 工具 | 本地所需 | 远端所需 |
| --- | --- | --- |
| `read_file` / `grep_search` / `glob` | `{ro}` | `{ro, ssh}` |
| `write_file` / `edit_file` | `{rw}` | `{rw, ssh}` |
| `bash` / `bash_bg` / `bg_manage` | `{bash}` | `{bash, ssh}` |
| 任意远端工具 **且路径越出远端项目根** | — | `{…, ssh}` + 当前角色 SSH 路径白名单 |

判定规则：

```
required = 工具所需集合（按执行面展开）
若任一 required 格为 deny/缺格  → deny（并列出缺哪些）
否则若任一 required 格为 ask    → ask（一次审批，覆盖整组）
否则                            → allow
```

`{rw, ssh}` 的装配因此天然得出你要的语义：**远端能写、不能读、本地也写不了读不了**——不合理，但它是矩阵里的一个合法点，系统不该替用户否决（见 §5 的 lint）。

### 2.2 SSH 是单一能力

| 类型 | 含义 | 与谁独立 |
| --- | --- | --- |
| `ssh` | 允许把工具调用打到远端执行面 | 独立于操作类型：只给 `ssh` 不给 `rw`，等于"能连但不能动" |

远端项目根之外的绝对路径由 `ssh` 能力内的 agent-role 路径白名单裁定；空白名单
是安全默认，只允许远端项目根内。路径判据与本地 `pathGate` 的 containment
**共用同一份判定**（AGENTS §8：一份判据一处函数），不另写，也不产生第二枚权限。

### 2.3 与"环境"维度的关系（上一轮的叠合）

上一轮定的是 `(策略, 环境, 权限)` 三坐标。这里的关系是：

```
EffectivePermissions(group, env) = Assembly[group] ∩ EnvProfile[env]
```

环境 profile 只能**收窄**不能放宽（与权限主体"只能收窄自己的位"同构）。所以 `test-readonly` 环境即使组里有 `rw`，也落不到 rw 上。

## 3. 契约（先行，避免依赖倒置）

端口定义在**消费方**（`contract`），实现方（`backend/limit/policy`、`wiring`）去满足它；registry 只认端口，不认具体实现。

```go
// ── 词表（唯一事实仍在 dto 与 policy.go 的组表）─────────────────────
type PermType string   // "ro" "rw" "rw_session" "rw_desktop" "ctl" "adm" "bash" "ssh"
type GroupID  string   // "root" "sub" "emp_ro" "emp_rw" + 自定义
type Effect   string   // "allow" | "ask" | "deny"

// ── 装配（矩阵）────────────────────────────────────────────────
type Grant struct {
    Effect Effect
    Bits   uint8          // rwx；0 = 用该类型的既有 Mode
}
type Assembly struct {
    Version int
    Cells   map[GroupID]map[PermType]Grant   // 缺格 = deny
}

// ── 读面端口（消费方定义）──────────────────────────────────────
type AssemblyPort interface {
    AssemblyFor(group GroupID) (Assembly, error)
}

// ── 判定端口 ───────────────────────────────────────────────────
type Requirement struct {
    Tool     string
    Remote   bool        // 是否走远端执行面
    OutOfRoot bool       // 远端路径是否越出项目根
    Path     string
}
type Decision struct {
    Effect   Effect
    Required []PermType
    Missing  []PermType
    Warnings []string
    Reason   string
}
type Gate interface {
    Decide(ctx context.Context, group GroupID, req Requirement) (Decision, error)
}

// ── 中间件（产品语义就是 middleware）──────────────────────────────
func PermissionMiddleware(gate Gate, groupOf func(context.Context) GroupID) frameworktools.MetaMiddleware
```

**为什么 `Requirement` 里放 `Remote`/`OutOfRoot` 而不是让上层拼 `[]PermType`**：拼集合的逻辑（哪个工具在哪个执行面需要哪几格）必须**只有一份**。放在实现方，五个工具各自的 handler 就不用知道自己需要什么权限——那正是"权限词表散落各处"的起点。

## 4. 迁移与兼容

老档位不删除，改成**权限向量预设**（preset），由 `TierVector` / `TierCells` 展开：

| 老档位 | 预设形状 | 说明 |
| --- | --- | --- |
| `manual` | 按组表原样（`rw_desktop` 等 = ask） | 完全手动 |
| `edit` | 加上 `rw` | 文件写入不再打断，命令仍问 |
| `auto` | 再加上 `bash` | 任意命令直跑，deny 仍硬拦 |
| `full` | 全公开格 allow | 只在**显式**选择时生成；不再依赖 `BypassAll` 短路 |

### 4.1 `edit` / `auto` 的区分已落地

2026-10-25 起 `bash` / `bash_bg` / `bg_manage` 从 `rw` 路由簇拆到独立 `bash`
簇。`edit` 只授予文件写，`auto` 额外授予命令执行，两档不再共用同一份能力向量；
也不再需要工具级 ask 例外表作为补丁。


## 5. 可表达但可疑的装配：只报警不否决

用户明确要求支持"看起来不合理"的装配。因此实现里加一条 **lint**（不参与判定）：

| 形状 | 警告 |
| --- | --- |
| 只有 `ssh`，无任何操作类型 | `assembly_ssh_only`：能连不能动 |
| 有 `rw`，无 `ro` | `assembly_write_without_read`：能写不能读回校验 |

lint 只进轨迹与设置页提示，**不改变 allow/deny**——否决"不合理但用户明确要的"装配，等于把系统自己的偏好塞进用户策略。

## 6. 中间件位置与语义

产品语义是中间件，落点不变：仍是 `registry_state.go` 那一摞的第 3 层（event → pathGate → **permission** → diagnostic）。新增的两类权限判定发生在**同一个中间件内部**，不新增链层：

```
permission middleware
  ├─ 组解析（subject → GroupID，含自定义组）
  ├─ Requirement 组装（工具 + 执行面 + 路径 containment）
  ├─ Assembly 判定（矩阵 → allow/ask/deny）
  └─ ask 时走既有审批面（ApprovalHandler）
```

传输选择（上一轮）放在它**之后**：先判权限，再建连接——被拒的调用不该白连一次远端。

## 7. 验收判据

| 编号 | 判据 | 验证方式 |
| --- | --- | --- |
| P1 | 矩阵可表达任意正交子集（含 `{ssh}`、`{rw, ssh}`） | 表驱动 |
| P2 | 缺格 = deny（默认拒绝） | 空装配下全 deny |
| P3 | 一次调用需要**一组**权限，任一缺即 deny 并列出缺项 | 断言 `Missing` |
| P4 | `ask` 覆盖整组（一次审批，不逐格问） | 断言只发一次 ask |
| P5 | SSH 只有一枚能力，旧 `ssh.userspace` 读取时归一到 `ssh` | 词表与兼容用例 |
| P6 | 远端越界时检查当前角色的 SSH 路径白名单 | 根内/根外两条用例 |
| P7 | 老档位可翻译成预设且判据不比原来宽 | 对照用例 |
| P8 | lint 不改变判定结果 | 断言 Effect 不变、Warnings 非空 |
| P9 | 环境 profile 只收窄不放宽 | 交集用例 |
| P10 | 组解析与词表仍只有一份事实源 | 代码检查 |

## 8. 分期

| 期 | 内容 | 出口 |
| --- | --- | --- |
| **M0（本次）** | 孤立 MVP：矩阵 + 需求集合 + userspace + lint + 预设 + 中间件形状 | MVP 场景全 PASS（实测 10/10） |
| M1 | 契约落 `contract`：`PermType`/`Assembly`/`AssemblyPort`/`Gate`/`Requirement` | 编译通过，registry 只依赖端口 |
| M2 | `policy.go` 增单一 `ssh` 组 + 组表派生（`DeclaredToolMeta` 自动跟随） | 已实现 |
| M3 | `PermissionGate` 内部改走矩阵（保留老档位为预设） | P7 对照用例 |
| M4 | 前端权限控制组件改矩阵编辑（格子开关 + lint 提示） | 手工冒烟 |
| M5 | 环境 profile 收窄接入（与 ssh-tool-transport 的 M2 合流） | P9 |

**M4（前端）与 M2/M3 未完成前，权限保持现行为**：本次不动产品代码。
