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

`Dependencies` 是 `core.New` 的装配输入。`EngineMessage`/`EngineToolCall` 是防止 Seele types 穿透应用层的传输模型。

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
