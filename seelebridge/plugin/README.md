# plugin 域

## 模块定位

承载 Runtime 的插件可见性**执行面**：插件不再控制 holder，而是作为
`bridge.WithVisibilityPolicy` 的输入——激活插件时按 include/exclude 过滤
每次请求的可见工具集。主要调用方：`ports.go`（插件端口）、`runtime.go`
（seelexVisibilityPolicy）。

> 边界（解耦方案 §02.4 方案 A 轻量版）：本包只是**可见性投影缓存**，不是
> 插件定义的事实源。事实源在顶层 `plugin.Manager`（manifest/skills/MCP
> 全量契约），本包 `defs` 由 root 经 `ToolBackend` 单点推送；写路径只有
> root 一个入口，配合 `plugin/apply.go` 的 `Transaction` 保证更新原子。
> 多插件叠加属产品决策，当前为单选（`active string`）。

## 职责与非职责

- 职责：`Manager.Define/Undefine/Activate/Deactivate/Active` 与
  `Filter`（include/exclude 模式过滤）。
- 非职责：插件声明式能力包加载（归顶层 `plugin/` + `plugins/`）、
  skill 联动（归 `skill/`）。

## 与其它域的关系

```text
runtime.seelexVisibilityPolicy ──► plugin.Manager.Filter ──► bridge 可见性策略
     │
     └──► node（GoalSkillActive 独立决定 plan 工具面）
```

## 核心实现

- `Manager`：自带 RWMutex 的状态 actor（defs + active）。
- `Filter`：激活插件时按 `path.Match` 通配过滤（与旧 holder 语义一致）。

## 数据流

root `plugin.Manager`（定义事实源）→ `ToolBackend` 单点推送
`DefinePlugin`/`UndefinePlugin`/`ActivatePlugin` → 本包投影缓存
（defs + active）→ 每次工具可见性求值时 `Filter` 过滤 → 模型可见工具面。

## 依赖方向

允许依赖：`framework types`。禁止依赖：seelebridge 根包及其它域。

## 并发、存储、安全

`Manager` 自带锁；快照复制防数据竞争。

## 扩展方式

新增过滤语义：扩展 `Filter` 或 `matchToolPattern`。

## Review 指南

- include/exclude 优先级（exclude 优先）是否保持；空 include 是否放行。

## 测试与验证

`go test ./seelebridge/plugin/...`；根包可见性测试覆盖联动。
