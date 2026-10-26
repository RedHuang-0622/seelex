# application/input/command

## 生态位

`application/input/command` 承接内置命令注册中不依赖具体会话状态机的只读/基础面。
主要调用方是 `application/core.Service.registerBuiltinCommands`；命令注册表本身仍由
`backend/engine/input` 提供，路由行为不变。

## 职责

- 注册 help、clear、model、history、trace、sessions、diag、exit；
- 注册 compact/new/resume/fork/plugin/effort/permission 的参数与结果适配；
- 统一命令描述、返回 `contract.CommandResult` 和注册错误包装；
- 保持 help 的命令排序、sigil 提示和历史角色统计。

## 非职责

- 不拥有 command registry 实现、Router 或输入 token 拆分；
- mutation 命令只做参数解析和 Application/Session callback 调用，不复制状态机；
- 不执行普通对话、工具或 Skill；
- 不 import `application/core`。

## 核心契约

```go
type Registry interface {
    Register(contract.Command) error
    All() []contract.Command
}

type Deps struct {
    Model, History, Trace, Sessions, Diag, Clear /* host callbacks */
    SigilCommand, SigilPlugin, SigilSkill string
    Mutation MutationDeps
}

type MutationDeps struct {
    Compact, BeginNewSession, Resume, Fork, SwitchPlugin, SwitchEffort, SetPermission /* callbacks */
    Capabilities, SessionInteraction, CurrentSession, AccountInteraction, Plugins, Effort, Permission, PermissionTiers /* callbacks */
}
```

`Registry` 由组合根注入；标准 `CommandRegistry` 通过结构满足接口。所有命令 handler
都在 `Application.Register` 时一次性注册。

## 数据流

`Service.registerBuiltinCommands` → `command.Application.Register` → registry →
`input_router` 调用 `Command.Execute` → `CommandResult` 返回前端。

## 依赖方向

只依赖 `contract` 与 `middle/protocol/model` 的 DTO；禁止依赖 `application/core`、
backend Router 或 GUI/TUI。新增命令必须由真实入口使用，不得制造未来接口。

## 并发与错误

注册表锁和 handler 并发语义由宿主 registry 拥有；本包不缓存状态。重复命令由 registry
返回错误，包装为 `register command "<name>": ...` 后停止后续注册。

## 扩展方式

先扩展 `Deps` 的真实回调和包内测试，再把命令注册从 core 移入。会话 mutation 命令
仍应调用现有 Application/Session API，不得复制状态机。

## Review 指南

- help 输出是否仍只列可提交命令；
- clear 是否只调用既有 reset 回调；
- history/session 格式化是否不读取未授权状态；
- 是否重复实现 Router 或 core Service。

## 测试与验证

```text
go test ./application/input/command -count=1
go test ./application/core -run 'Command|Suggestions' -count=1
```
