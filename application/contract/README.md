# Application Contracts

## 定位

`contract` 定义 Application 所需的外部端口，是依赖倒置的边界。接口属于使用者，而不是 Seele、存储或前端实现方。

## 主要端口

| 接口 | 责任 | 典型实现 |
|---|---|---|
| `ChatEngine` | 流式聊天、历史替换、session 生命周期、prompt/loop 配置 | 根目录 `enginePort` |
| `RuntimePort` | Provider、Account、Tool、Plugin、项目根和 Plan branch binding | `seelebridge.Runtime` 适配器 |
| `PluginPort` | Plugin 列表、激活、停用 | `plugin.Manager` 适配器 |
| `SkillPort` | Skill 查询 | `skill.Registry` 适配器 |
| `SessionPort` | 会话保存、读取、分页、删除和 active workspace | `session.Manager` 适配器 |
| `WorkspacePort` | 项目 CRUD、session binding、Git remote | `workspace.Repo` 适配器 |

`Dependencies` 是 `core.New` 的装配输入；Engine、Runtime、Plugin、Skill 与 Session 端口为必需依赖，构造器缺失时返回 error。`EngineMessage`/`EngineToolCall` 是防止 Seele types 穿透应用层的传输模型。

### 可选扩展端口（按类型断言发现）

| 接口 | 责任 | 典型实现 |
|---|---|---|
| `RoleSessionPort` | R2/R4 群聊角色会话：建角色会话、role draft 读写与 sequencer sync、顺序设置、角色 backup、snapshot/wire 观察面 | `internal/adapters.SessionPort`（同时实现 `SessionPort`） |
| `SchedulePort` | 定时任务式插话的 `schedule.registered` / `schedule.cancelled` / `schedule.fired` 事件登记 | 同上 |

这两个端口是**可选能力**：`application/core` 用类型断言在 `Deps.Sessions` 上发现它们，
未装配时显式返回“不可用”，不静默退化、不新增旁路。签名只用
`application/contract/dto` 的纯 DTO（`dto.RoleRow` / `dto.RoleDraftRow` /
`dto.RoleSnapshot` / `dto.RoleWireSnapshot` / `dto.ScheduleEventPayload`），
存储实现类型 `sessionstore.*` 只允许出现在 `internal/adapters` 的映射函数里
（S27 收口；回归见 `e2e/dto_boundary_test.go`）。

## 扩展规则

- 只有 Application 用例确实需要的新能力才进入接口；不要为底层实现“顺手暴露”方法。
- 优先增加窄的可选能力接口，例如 core 内部的 scoped-session/storage port，而不是破坏所有实现。
- 新方法必须同步更新根适配器、fake ports、E2E harness 和编译期接口断言。

## Review 指南

- 接口参数是否使用 Application-owned model，而不是 GUI 或数据库类型。
- 方法是否表达用例语义，而非底层技术细节。
- project/session 作用域是否显式，是否会依赖可变的全局 active scope。
- mock 实现返回值是否掩盖真实错误路径。

## 测试

```text
go test ./application/... ./e2e/scenario . -count=1
```
