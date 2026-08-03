# 前置审查报告

## 需求摘要

将“工作区”（产品语义为项目）变为会话实际的文件读写与工具执行根目录，避免未绑定或切换到其他项目时访问 Seelex 自身仓库。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `seelebridge/project_scope.go` | 新增 | 项目工具作用域 | 统一解析项目内路径，拒绝越界、无项目和符号链接逃逸。 |
| `seelebridge/scoped_tools.go` | 新增 | 内置文件与 shell 工具覆盖 | 让 `read_file`、搜索、编辑、写入和 `bash` 使用项目作用域。 |
| `seelebridge/runtime.go` | 修改 | builtin 注册与作用域 API | 在 Seele builtin 后注册同名受限工具，并暴露绑定接口。 |
| `application/ports.go` | 修改 | `RuntimePort` | 声明应用层所需的窄项目作用域接口。 |
| `application/app.go` | 修改 | 项目创建、绑定、解绑、恢复 | 将会话绑定、会话存储路由和工具根目录作为同一状态切换处理。 |
| `application/command.go` | 修改 | `/new` | 新会话继承当前项目，而不是落回默认会话目录。 |
| `application/state.go` | 修改 | Snapshot clone | 正确复制会话—项目归属映射。 |
| `application_adapters.go` | 修改 | `runtimePort` | 连接应用层端口与 Seele runtime。 |
| `gui/frontend/dist/index.html` | 修改 | 项目区文案 | 用“项目”表达用户确认的产品模型。 |
| `gui/frontend/dist/app.js` | 修改 | 项目/会话渲染 | 展示会话所属项目，并提供解绑入口。 |
| `seelebridge/*_test.go`、`application/*_test.go` | 修改/新增 | 单元与集成验证 | 覆盖项目隔离、越界拒绝、创建/切换/新建会话的绑定语义。 |

## 依赖分析

- 上游依赖：Seele `Holder.RegisterInline` 会优先保留 inline provider 中的同名工具，因此可在不修改外部 v0.0.8 依赖的前提下安全覆盖 builtin 实现。
- 下游影响：应用所有绑定路径（创建、选择、恢复、解绑、新会话）都必须同步更新工具作用域和 nested session store；GUI 根据 Snapshot 呈现该状态。

## 循环依赖检查

- [x] 新作用域位于 `seelebridge`，不依赖 `application` 或 `workspace`。
- [x] 应用层仅依赖自己声明的 `RuntimePort`，不会反向导入具体 runtime。

## 风险评估

- `bash` 的工作目录可被固定到项目，但原始 shell 命令本身可以通过 `cd`、绝对路径或网络继续越界；真正的强隔离需要 OS 沙箱或命令策略，不能伪装成已完成的安全边界。
- 符号链接和 Windows 大小写/路径前缀若处理不当会造成逃逸，因此路径解析需要真实路径校验，而不能仅做字符串前缀比较。
- Seele Agent 是单 runtime 实例；当前产品一次只运行一个活跃会话。后续若同时运行多个会话，作用域须改为请求上下文传递，不能共享可变根目录。

## 建议方案

新增 `ProjectScope`，将相对路径解析为项目根目录下的绝对路径，对绝对路径、`..` 和符号链接越界返回错误。运行时在注册 builtin 后注册同名 wrapper 工具；未绑定项目时这些工具 fail-closed。应用层集中实现项目绑定动作，并使创建、恢复、解绑和 `/new` 都调用该动作。`bash` 默认和显式 `workdir` 都受项目根目录约束，同时在工具说明中明确它不是 OS 级 sandbox。
