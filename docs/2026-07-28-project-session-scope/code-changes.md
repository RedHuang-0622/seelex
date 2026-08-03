# 代码变更摘要

## 新增/修改文件

| 文件 | 类型 | 说明 | 设计模式 |
|---|---|---|---|
| `seelebridge/project_scope.go` | 新增 | 会话项目根的 fail-closed 路径解析；拒绝越界和符号链接逃逸。 | Policy object |
| `seelebridge/scoped_tools.go` | 新增 | 覆盖 Seele 的文件工具与 shell 工作目录，接入 `ProjectScope`。 | Decorator / Adapter |
| `seelebridge/runtime.go` | 修改 | 注册受限同名工具、保护它们不被后续插件覆盖、暴露项目绑定 API。 | Facade |
| `application/ports.go`、`application_adapters.go` | 修改 | 添加窄 `RuntimePort` 项目作用域能力并适配 runtime。 | Ports and Adapters |
| `application/app.go`、`application/command.go` | 修改 | 创建、选择、恢复、解绑与新建会话统一同步项目根、会话绑定与 session store。 | Transactional state transition |
| `application/state.go` | 修改 | Snapshot 复制项目归属映射和当前项目，避免状态别名。 | Defensive copy |
| `application/application_test.go`、`e2e/scenario/harness.go` | 修改 | 覆盖项目创建、恢复与 `/new` 的绑定语义，并补齐端口 mock。 | Test double |
| `seelebridge/project_scope_test.go`、`seelebridge/runtime_test.go` | 新增/修改 | 覆盖无项目拒绝、路径/符号链接越界、项目切换和 shell cwd。 | Unit / integration test |
| `gui/frontend/dist/app.js`、`styles.css` | 修改 | 在会话中呈现项目归属并提供项目解绑入口。 | Presentation adapter |
| `sandbox-research.md` | 新增 | 跨平台 OS 级 shell sandbox 候选与 POC 验收标准。 | Architecture decision record |

## API 变更

| API | 变更 | 兼容性 |
|---|---|---|
| `application.RuntimePort` | 新增 `BindProjectRoot(string) error`、`UnbindProjectRoot()` | 内部端口；所有 adapter/test double 已同步。 |
| `seelebridge.Runtime` | 新增同名项目根绑定方法 | 向后兼容。 |

## 接口抽象

| 接口 | 实现方 | 使用方 |
|---|---|---|
| `RuntimePort` 项目作用域方法 | `runtimePort` / 测试 fake | `application.Service` |
| `ProjectScope` | `seelebridge` | scoped builtin tools |

## 验证

- `go test -p 1 ./... -count=1 -timeout 240s`：通过。
- `go vet ./...`：通过。
- `node --test gui/frontend/dist/*.test.mjs`：通过。
- `scripts/build-gui.ps1 -Version v0.1.0-alpha.1-project-scope`：通过。
- GUI 包 SHA-256：`04356ef5989bf6591a9b3927f530d32b441dc08a21b93f03329ed6e0f5bd1a0f`。

## 循环依赖检查

- [x] 未引入 Go package 循环依赖。

## 建议提交信息

```text
fix(project-scope): bind tools and sessions to selected project

- fail closed for filesystem tools without a project
- inherit and restore project bindings across session lifecycle
- expose project ownership in the GUI and document shell sandbox options

Refs: project-session-scope
```
