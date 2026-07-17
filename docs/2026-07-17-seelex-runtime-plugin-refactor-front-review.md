# 前置审查报告

## 需求摘要

把 Seelex 的上下文能力归并到 `seelexctx/`；把 Skill 目录升级为 `skills/<skill-name>/SKILL.md` 并允许携带脚本及任意资源；新增 `plugins/<plugin-name>/plugin.md` 与插件内 skills；复用 Seele 已有 PluginManager 和 MCP Provider，在 Seelex 中提供稳定薄封装和真正的插件切换能力。

## 当前事实

### Seele 版本边界

- `go.mod` 当前固定 `github.com/RedHuang-0622/Seele v0.0.1`。
- Go 代理只发布了 `v0.0.1`，没有更新 tag。
- `v0.0.1` 已包含：
  - `holder.PluginManager`：定义、激活、停用和按 include/exclude 过滤工具；
  - `agent.MCP()` 与 `mcp.Provider`：stdio/SSE Attach、Detach、RefreshTools；
  - `mark3labs/mcp-go` client：不需要 Seelex 自己实现 JSON-RPC/stdio client；
  - 账号池、ChatClient、storage、tracer、builtin tools、Hub 等能力。
- `v0.0.1` 不包含当前 Seelex 正在导入的 `agent/core/tool/permission`，也没有 `Agent.SetPermissionConfig`。通过 codeload 复核的 Seele `main` 同样没有这些 API，因此当前仓库无法全量构建，且不能通过简单升级到 main 解决。
- `v0.0.1` 没有从 `plugin.md` 或 `SKILL.md` 目录加载配置的实现；Seele 的 plugin 只是运行时工具过滤器，文件发现和插件生命周期需要由 Seelex 薄层补齐。

### Seele MCP 的一个集成注意点

`Agent.MCP()` 首次调用时把空 Provider 注册到 Holder。后续 `Provider.Attach()` 会更新 Provider 自身工具，但 `holder.Holder` 的工具列表是注册时构建的快照，Attach/Detach/Refresh 不会自动触发 Holder rebuild。

Seelex MCP 薄封装需要统一处理刷新问题；长期应在 Seele 增加 provider changed/rebuild API，短期可以在 Seelex 适配层集中做安全的 Unregister/Register 刷新，不能让业务代码到处操作 Holder。

## 目标目录

```text
seelex/
├── seelexctx/
│   ├── bridge.go
│   ├── seele.go
│   ├── snapshot/
│   ├── provider/
│   ├── compactor/
│   └── merger/
├── skill/                     # Skill 目录模型与 Loader
├── skills/
│   └── <skill-name>/
│       ├── SKILL.md
│       ├── scripts/
│       └── <any resource>
├── plugin/                    # plugin.md Loader + 生命周期编排
├── plugins/
│   └── <plugin-name>/
│       ├── plugin.md
│       └── skills/
│           └── <skill-name>/SKILL.md
└── seelebridge/               # Seelex 对 Seele 的稳定薄封装
```

## 影响文件清单

### 上下文包迁移

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `snapshot/*` | 移动/删除 | 全包 | 移到 `seelexctx/snapshot/` |
| `provider/*` | 移动/删除 | 全包 | 移到 `seelexctx/provider/` |
| `compactor/*` | 移动/删除 | 全包 | 移到 `seelexctx/compactor/` |
| `merger/*` | 移动/删除 | 全包 | 移到 `seelexctx/merger/` |
| `seelexctx/bridge.go` | 修改 | imports 与调用 | 使用新的子包路径 |
| `seelexctx/integration_test.go` | 修改 | imports、测试工厂 | 使用新路径和 Seelex facade/mock |
| `seelexctx/README.md` | 修改 | 目录与示例 | 同步新包结构 |
| `docs/context-improvement-plan.md` 等 | 修改 | 旧 import/目录引用 | 避免文档漂移 |

迁移后依赖方向：

```text
seelexctx -> seelexctx/{provider,compactor,merger,snapshot}
provider/compactor/merger -> seelexctx/snapshot
```

根包不能被子包反向 import，否则会产生循环依赖。

### Skill 目录升级

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `skill/skill.go` | 重构 | Loader、Skill DTO、Create/Delete | 支持目录型 Skill、资源根目录和安全路径校验 |
| `skill/skill_test.go` | 新增 | Loader/边界/并发测试 | 覆盖目录发现、重复名、路径逃逸、死锁回归 |
| `skills/*.md` | 迁移 | 全部现有 Skill | 迁移为 `skills/<name>/SKILL.md` |
| `README.md` | 修改 | Skill 使用说明 | 记录新布局与兼容策略 |

建议 `Skill` 增加 `RootDir`、`InstructionPath`，资源访问始终相对 `RootDir`。Loader 只把 `SKILL.md` 当入口文件，其余文件不解析但原样保留，供 bash、模板、引用资料和其他工具使用。

现有 `Loader.Create/Delete` 有两个必须同时修复的问题：持写锁后再次调用加读锁的 `PrimaryDir()` 会自锁；name 未校验可能通过 `../` 逃出 skills 根目录。

### Plugin 基建

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `plugin/plugin.go` | 新增 | Plugin DTO、MCP DTO | Seelex 文件模型，不暴露 Seele 类型 |
| `plugin/loader.go` | 新增 | `plugins/*/plugin.md` 发现与 frontmatter 解析 | 把文件目录转换为运行时定义 |
| `plugin/manager.go` | 新增 | Activate/Deactivate/List/Current | 编排工具过滤、插件 Skill、MCP 生命周期 |
| `plugin/*_test.go` | 新增 | 解析、切换、失败回滚、并发 | 保证插件切换原子性 |
| `plugins/<name>/plugin.md` | 新增/迁移 | default/read/write/git/shell/plan | 替代 `main.go` 的硬编码插件表 |
| `plugins/<name>/skills/` | 新增 | 插件私有 skills | 激活插件时与全局 Skill 合并 |
| `main.go` | 修改 | `initPlugins`、`switch_mode` | 改为加载真实插件并调用 Manager；保留兼容别名 |
| `tui/commands/*` | 修改 | 新增 `/plugins`、`/plugin <name>` | 用户可直接列出和切换插件 |
| `tui/view.go` | 修改 | 状态栏插件来源 | 读取 Seelex Plugin facade，而非 raw Agent Holder |
| `tui/sugg/engine.go` | 修改 | 工具/Skill 刷新 | 插件切换后同步刷新可见工具和 Skill |

`plugin.md` 建议使用 YAML frontmatter 保存机器配置，正文保存插件说明/提示词：

```markdown
---
name: mechanical-design
description: FreeCAD mechanical design workflow
include: [switch_plugin, cad_*]
exclude: []
mcp_servers:
  - name: freecad
    transport: stdio
    command: FreeCADCmd
    args: ["-c", "server.py"]
---

# Mechanical Design
插件级说明与工作约束。
```

### Seele 薄封装

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `seelebridge/runtime.go` | 新增 | Agent 创建/关闭、Engine 装配所需 accessor | main 不再理解 Agent Options 细节 |
| `seelebridge/accounts.go` | 新增 | 配置加载、账号摘要、provider 切换 | 隔离 `agent/core/api` |
| `seelebridge/tools.go` | 新增 | builtin 注册、工具注册/枚举/调度 | 隔离 holder/builtin/types |
| `seelebridge/plugins.go` | 新增 | Define/Activate/Deactivate/Current | 对 Seele PluginManager 的薄适配 |
| `seelebridge/mcp.go` | 新增 | Attach/Detach/Refresh/List + Holder 刷新 | 复用 Seele MCP Provider/Client |
| `seelebridge/storage.go` | 新增 | SessionStore 与元数据 DTO/alias | 隔离 seelectx/storage |
| `seelebridge/trace.go` | 新增或局部适配 | tracer DTO/常量 | 避免 TUI/provider 到处 import tracer |
| `seelebridge/approval.go` | 待依赖确认 | permission/approve 映射 | 当前已发布 Seele 版本缺少 permission API |
| `seelebridge/*_test.go` | 新增 | mock 与适配契约 | 固定 Seelex 侧稳定行为 |

调用方应定义窄接口：

- `plugin.Manager` 定义 ToolPluginBackend、MCPBackend、SkillBackend；
- `tui` 定义 RuntimeView/AccountView；
- `session` 定义 SessionStore；
- `seelebridge` 的具体类型满足这些接口。

只有 `main.go`、`seelexctx` provider 和 TUI 流处理可以因 Engine API 直接 import `Seele/engine`。其他 Seele imports 应逐步收敛到 `seelebridge/`。

### 测试与文档

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `.github/workflows/ci.yml` | 修改 | format/test/race | 为新 Loader/Manager 建立门禁 |
| `smoke_test.go` | 修改 | runtime 工厂 | 使用 facade，避免重复装配 |
| `test-report.md` | 更新 | 完整测试结果 | 记录覆盖率和阻塞项 |
| `code-changes.md` | 更新 | 变更清单 | code-impl 要求 |
| `finish-review.md` | 更新 | 最终五轴审查 | finish-review 要求 |

## 依赖分析

### 上游依赖

- `main.go` 是 Agent、工具、权限、Plugin、Storage、Engine、TUI 的装配入口。
- `tui.Model` 当前直接持有 `*agent.Agent` 和 `*api.ChatClient`。
- `session.Manager` 当前直接持有 `*storage.Store`。
- context 子包直接引用 Seele engine/tracer/types/seelectx。

### 下游影响

- 包移动会改变所有 context import path，属于源码级不兼容变更。
- Skill 从平铺文件迁移到目录后，旧 `skills/*.md` 不再是主格式；建议 Loader 暂时只读兼容一个版本，但 Create 只写新格式。
- 插件切换将不再只是工具过滤，还会改变可见 Skill 和插件 MCP 连接，必须支持失败回滚。
- TUI 状态栏、补全列表、`switch_mode` 工具和命令系统都受影响。

## 循环依赖检查

- [x] `seelexctx` 根包只依赖子包，子包不反向依赖根包。
- [x] `plugin` 在调用方定义 backend interfaces，不 import `seelebridge`。
- [x] `seelebridge` 不 import `plugin`/`tui`/`session` 业务包。
- [x] `main` 负责组合具体实现。
- [x] Engine 继续作为允许的直接依赖，不包进通用 util。

## 风险预估

| 风险 | 概率 | 严重程度 | 应对 |
|---|:---:|:---:|---|
| Seele 发布版本缺少 permission API | 高 | 阻断 | 先发布新 tag 或提供本地 Seele 源码路径 |
| MCP Provider 更新后 Holder 工具快照不刷新 | 高 | 高 | 薄封装集中 refresh；同时建议回补 Seele API |
| 插件激活一半失败导致工具/Skill/MCP 状态不一致 | 中 | 高 | 两阶段切换、失败回滚、串行化 Manager |
| Skill/Plugin 目录路径穿越 | 中 | 高 | 规范化名称、验证 resolved path 在根目录内 |
| 包移动破坏外部 import | 中 | 中 | 可选保留一版 deprecated forwarding package |
| plugin.md 格式未来扩展 | 中 | 中 | `schema_version` + 严格 frontmatter DTO |
| 大范围一次性重构难定位回归 | 高 | 中 | 按 context、skill、bridge、plugin 四个垂直切片实施 |

## 建议方案

采用“Seelex 文件/生命周期层 + Seele 能力适配层”的组合方案：

1. 先修复/升级 Seele 依赖，使仓库恢复可构建。
2. 迁移 context 包，保持行为不变并先跑回归测试。
3. 重构 Skill Loader，迁移目录，修复锁和路径安全。
4. 新建 `seelebridge`，先封装当前实际用到的 accounts/tools/plugins/MCP/storage/trace；不做无调用方的过度封装。
5. 新建 plugin Loader/Manager，用接口注入 Seele backend、MCP backend 和 Skill registry。
6. 把 hardcoded mode 迁移为 plugin.md，加入 `/plugins`、`/plugin` 和 `switch_plugin`，`switch_mode` 保留兼容。
7. 插件切换成功后刷新 TUI tools/skills；失败时恢复旧插件及 MCP 状态。
8. 最后运行 build/vet/test/race/coverage，并更新文档。

## 进入实现前的阻塞确认

当前工作区没有本地 Seele 源码；已发布的 `v0.0.1` 和可下载的 Seele `main` 都缺少现有 permission API。编码方案必须暂时移除这段不可构建的接线并保留 Seelex 审批 UI，等 Seele 提供可注入 permission gateway 后再通过 `seelebridge` 恢复；如果不能接受这一临时行为变化，则需要先在 Seele 完成并发布 permission 能力。
