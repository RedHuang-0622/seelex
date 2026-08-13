# account 域

## 模块定位

承载 Seelex 的账号装配与选择：从账号配置构造同步 Completer、注册进 P2C
账号池、按 role+seed 稳定哈希解析节点账号。主要调用方：`runtime.go`
（NewRuntime 装配、accountSelector）、`branch.go`（已并入 runtime.go 的
分支账号路由）。

## 职责与非职责

- 职责：`ClientFor` 构造 client、`RegisterAccounts` 注册账号池、
  `ResolveForBranch`/`ForRole`/`ByName`/`StableIndex` 选择与查找。
- 非职责：不持有账号池状态（池在 Runtime）、不决定 UI 展示。

## 与其它域的关系

```text
runtime（组合根） ──► account ──►（accountpool / api.ChatClient）
     │
     └──► node ──►（节点账号路由复用 ResolveForBranch）
```

## 核心实现

- `ClientFor(spec)`：每个账号一个独立 `api.NewChatClient`，provider 类型
  由配置设定。
- `ForRole(pool, role)`：按角色（含回退链）筛选启用账号，无匹配回退任意
  启用账号。
- `StableIndex(seed, size)`：FNV-1a 32 位稳定哈希索引（同 seed 恒等）。

## 数据流

NewRuntime 读取账号 YAML → `RegisterAccounts` 注册池 → 请求时
`accountSelector` 把选中账号/provider 过滤转为 `AcquireRequest` → P2C 租赁。

## 依赖方向

允许依赖：`internal/model`、`accountpool`、`agent`、`api`、`types`。
禁止依赖：seelebridge 根包及其它域。

## 并发、存储、安全

本域无状态、纯函数；凭据不落日志（Metadata 只放 provider/model）。

## 扩展方式

新增账号角色：在 `internal/model` 的 FallbackRoles 定义；新增选择策略：
在 `ResolveForBranch` 扩展。

## Review 指南

- 账号角色回退链是否覆盖全部角色；禁用账号是否被过滤。
- 稳定哈希是否保持同 seed 同 size 恒等。

## 测试与验证

`go test ./seelebridge/account/...`；稳定选择由根包
`runtime_test.go` 的 TestResolveAccountForBranchIsStableAndRoleScoped 覆盖。
