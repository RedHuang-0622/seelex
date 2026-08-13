# tools 域

## 模块定位

承载 scoped 工具路由与工具注册表状态：受项目根限制的
read/grep/glob/write/edit/bash 工具族（`Router`）、内联工具 provider
（`RegistryState.AddInline`）、权限门控（`PermissionGate`）。主要调用方：
`runtime.go`（RegisterBuiltins、seelexVisibilityPolicy）。

## 职责与非职责

- 职责：`Router` 注册并路由项目作用域工具；`RegistryState` 包装
  framework tools.Registry（超时/中间件/内联工具）；`PermissionGate`
  做工具调度前的权限检查（allow/deny/ask）。
- 非职责：MCP 工具生命周期（归 mcp 域）、plan 工具族（归 plan 域）。

## 与其它域的关系

```text
runtime ──► tools.Router（scoped 工具）
     │
     └──► tools.RegistryState ──► framework tools.Registry
                │
                ├──► mcp（重挂载 MCP provider）
                └──► task（终态工具 provider）
```

## 核心实现

- `Router`：Deps 闭包注入 Runtime 能力（filesystem/projectScope/docker
  恢复/诊断），注册 read/write/bash 等工具。
- `RegistryState`：framework registry 包装 + `InlineProvider` 累积
  RegisterTool 产品工具（重名覆盖、快照重建）。
- `PermissionGate`：middleware 闭包捕获，运行时原子更新。

## 数据流

RegisterBuiltins → Router.Register → framework registry；每次工具调度 →
permission middleware → handler → 诊断/遥测钩子。

## 依赖方向

允许依赖：`fs`、`security`、`internal/model`、`framework tools`、
`framework types`。禁止依赖：seelebridge 根包及其它域。

## 并发、存储、安全

`Router`/`PermissionGate` 自带锁；路径经 `security.ProjectScope` 校验；
bash 诊断观察者 panic 隔离。

## 扩展方式

新增 scoped 工具：扩展 `Router.Register`；新增内联产品工具：`AddInline`。

## Review 指南

- 路径是否可能逃逸项目根；权限中间件是否在注册表构造时正确闭包捕获。

## 测试与验证

`go test ./seelebridge/tools/...`（router_test、permission_state_test）。
