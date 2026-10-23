# 权限装配（用户组 × 权限类型）—— 孤立可行性 MVP

一次性工作包，**不属于产品链路**：不注册工具、不改判定、不碰审批面。设计见 [`../../docs/arch/permission-assembly.md`](../../docs/arch/permission-assembly.md)。

## 跑法

```bash
go run ./docs/2026-10-23-permission-assembly-mvp
```

10 组场景全 PASS 才退出 0。无外部依赖、不联网。

## 它证明了什么

| 场景 | 结论 |
| --- | --- |
| S1 | 装配 `{ssh}` ⇒ 本地读 deny、远端 bash deny（**ssh 是通道能力，不隐含操作权限**），lint 报 `ssh_only` 但判定不变 |
| S2 | 装配 `{rw, ssh}` ⇒ 远端写 allow、远端读 deny、本地写 allow、本地读 deny —— 用户说的"不合理但要支持"确实可表达 |
| S3 | 装配 `{ro, ssh}` ⇒ 根内远端读 allow；越界读 deny 且**缺项精确为 `ssh.userspace`** |
| S4 | 补上 `ssh.userspace` ⇒ 越界读 allow |
| S5 | 空装配 ⇒ 全 deny（缺格 = deny）；未登记工具要求 `adm`（fail-closed，不是放行） |
| S6 | 整组 `ask` ⇒ **一次审批**覆盖所有格，通过后进入 handler |
| S7 | lint 只进 `Warnings`，**不改 Effect** |
| S8 | 老档位 `manual ≤ edit ≤ auto ≤ full` 单调不减，`full` 全放行，且 `emp_ro + manual` 仍写不了 |
| S9 | 装配 JSON 往返 `DeepEqual`（契约稳定） |
| S10 | deny 时 handler 零调用（中间件短路），错误可 `*ErrDenied` 归类 |

## 结构

| 文件 | 作用 |
| --- | --- |
| `ports.go` | **契约草案**（将来搬进 `contract`）：`PermType`/`GroupID`/`Effect`/`Grant`/`Assembly`/`AssemblyPort`/`Requirement`/`Decision`/`Gate` |
| `assembly.go` | 需求集合的唯一一张表（工具 → 权限类型）、矩阵判定、lint、老档位预设 |
| `middleware.go` | 权限中间件（与框架 `MetaMiddleware` 同形状）+ `ErrDenied`/`ErrApprovalRejected` |
| `main.go` | 十组场景 |

## MVP 暴露的一个真问题（迁移期必须处理）

**矩阵比阶梯粗一档。** 老 `edit` 档删的是**按工具名**的 ask 规则（`write_file`/`edit_file` 不问，`bash` 仍问）；矩阵的格是**按类型**的（`rw` 整类一个效果）。所以 `PresetForTier("edit")` 在矩阵里与 `manual` 同形——那一步的区分**表达不出来**。

两条修法（待选）：

1. 格子上加可选细化：`Grant{Tools []string}`，只对列出的工具生效（默认整类）；
2. 另行保留"工具级 ask 例外表"作为矩阵的补丁层（会多一个事实源，不推荐）。

MVP 里没有偷偷做掉这件事，而是在 `assembly.go` 的注释与本文档里显式标出。

## 刻意没做的

- 没接产品：`PermissionGate`、`policy.go` 的组表、审批面都未改动（M1–M3）。
- 没做环境 profile 收窄（`Assembly[group] ∩ EnvProfile[env]`，M5）。
- 没做前端权限控制组件（M4）。
- `Requirement.Remote` / `OutOfRoot` 在 MVP 里由场景直接给；生产实现应由传输选择器与 pathGate 喂入，本层不重复解析路径。
