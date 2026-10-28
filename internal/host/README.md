# Host：Application 端口的宿主适配层

## 生态位

`internal/host` 把 Application 声明的窄端口实现到 Seelex Runtime、Engine、Store、
Workspace、Plugin 与 Skill。它位于组合根与领域实现之间，主要调用方是 `boot`；
Application 只看到 `contract` 与领域包定义的消费方接口。

## 职责与非职责

职责：

- 提供 `EnginePort`、`RuntimePort`、`SessionPort`、`WorkspacePort`、
  `PluginPort`、`SkillPort` 等宿主实现。
- 做具体类型到 contract/protocol DTO 的边界转换。
- 维护会话级 Engine 注册、在飞计数、待安装历史和退出释放协调。
- 把账号、审批、压缩、流式、角色权限、SSH、Goal 等应用端口接到唯一领域实现。

非职责：

- 不拥有 Session、Snapshot、Task、Goal 或 Workunit 业务状态机。
- 不实现存储原子写、工具权限判据或 LLM 循环。
- 不向前端暴露具体 Runtime/Store 类型。

## 文件结构

| 文件组 | 职责 |
|---|---|
| `engine_port.go` | 会话级 Engine 路由、历史安装、取消与内存释放。 |
| `runtime_port.go`、`runtime_narrow_ports.go` | Runtime 读面、任务、调度、子代理与策略端口。 |
| `session_manager.go`、`session_records.go` | Session Manager 与记录分页适配。 |
| `session_workspace_ports.go`、`workspace_environment_port.go` | Workspace、Git、环境表与会话 binding。 |
| `role_permission_port.go`、`role_sessions.go` | 角色权限、SSH 路径与角色会话工厂。 |
| `account_admin.go`、`account_window.go` | 账号 CRUD、热注册与窗口策略。 |
| `compaction_index_port.go`、`compressed_turn_store.go` | 上下文压缩与压缩轮次端口。 |
| `conversation_stream.go`、`residency.go` | 流式输出、驻留与释放适配。 |
| `goal_coordinator.go`、`skill_activation.go` | Goal 与 Skill 宿主适配。 |
| `messages.go`、`error_presentation.go` | DTO 转换与错误呈现。 |

## 核心实现

- `EnginePort` 按 `sessionID` 维护独立 Engine；会话切换不再销毁其他会话。
- 历史替换遵循“锁内决定、锁外交接”的序号闸；在飞会话先登记 pending，
  收尾后安装，避免不同会话互相覆盖历史。
- `RuntimePort` 与 `SessionPort` 只做类型转换和窄转发，具体判据保留在 Runtime、
  Store、Kernel 或 Application owner。
- 所有可选能力通过消费方窄接口或编译期断言识别，不以 `if kind/role` 在实现内
  区分层。

## 数据流与生命周期

```text
boot
  -> NewEnginePort / RuntimePort / SessionPort / WorkspacePort ...
  -> Application assembly
  -> Application owner 经 port 调用
  -> host 转换 DTO
  -> Runtime / Store / Workspace / Plugin 唯一实现
```

Engine 与 Session 状态按会话创建；LRU 驱逐、会话卸载、归档和删除必须同步释放
框架 Session、上下文、历史与宿主映射。关闭路径不得留下订阅、锁等待或后台闭包。

## 依赖方向

- 允许依赖 Application 定义的端口、`contract`、`contract/dto`、
  `middle/protocol`、Kernel、Backend、Store 与 `internal/*` 基础设施。
- Application、Kernel、Store、Backend 不得反向 import `internal/host`。
- 适配器只依赖其直接实现所需的端口；禁止新增万能 Deps 或按调用方复制业务判据。

## 并发、存储与错误语义

- 会话 ID 是路由与存储唯一键；显示名称不参与 binding。
- Engine 历史与在飞计数必须同锁读取；网络/LLM 调用不得持锁。
- 历史替换必须保留最后决定序号，防止过期交接覆盖新会话。
- Store/Workspace 调用携带显式 project scope，不得借用当前 active scope。
- 可选能力未装配时返回明确错误或使用已声明的空能力语义，不静默伪造成功。

## 扩展方式

新增 Application 端口时先在消费方定义最小接口，再在本包找现有类型是否能结构满足；
只有无法结构满足时才增加薄适配器。新适配器必须写编译期断言、局部用例和模块 README，
并说明旧面、新面、删除条件和归零验证。

## Review 指南

- `EnginePort` 是否又新增会话级散字段、裸 map 或重复历史判据。
- 是否把业务状态机搬进 host，而不是只转换类型和转发。
- 并发替换是否会覆盖更晚的历史、锁内是否发生 I/O。
- 释放是否同时覆盖 Engine、Session、Context、历史与 Store 映射。
- DTO 转换是否漏字段、复制指针共享或泄露 Seele/Store 具体类型。
- 可选能力失败是否被静默降级。

## 测试与验证

```text
go test ./internal/host -count=1
go test ./application/assembly/{architecture,integration} -count=1
go test ./e2e/ -run 'Boundary|Store|Facade|Session' -count=1
```

## 文件与函数索引

| 文件 | 主要入口 |
|---|---|
| `engine_port.go` | `NewEnginePort`、`EnginePort.ApplyDeps`、`ChatStreamFor`、`ReplaceHistoryFor`、`ResumeRawSession`、`ReleaseSessionBundle`、`UnloadSession`、`TokenCountFor`。 |
| `runtime_port.go` | `RuntimePort` 的模型、上下文窗口、插件、权限、任务、子代理和事件读面。 |
| `runtime_narrow_ports.go` | Runtime 的窄能力端口与 DTO 转换。 |
| `session_manager.go` | `NewSessionManager`、`WithRouter`、`SaveCommitWorkspace`、`SaveCurrent`、`Resume`。 |
| `session_workspace_ports.go` | `PluginPort`、`WorkspacePort`、`SkillPort` 的创建、查询、Git、文件与连接适配。 |
| `workspace_environment_port.go` | `WorkspaceEnvironments`、`SetWorkspaceEnvironment`、默认环境写面。 |
| `role_permission_port.go` | 角色默认向量、账号、继承、SSH 路径与权限层适配。 |
| `session_ssh_state_port.go` | 会话 SSH 状态读写与 `SessionSSHEnvResolver`。 |
| `account_admin.go` | `AccountAdmin.AddAccount`、`UpdateAccount`、`DeleteAccount` 与热替换。 |
| `compaction_index_port.go` | `PushCompactionFrame`、`ReadbackCompactionSummary`。 |
| `goal_coordinator.go` | `NewGoalCoordinator` 与会话 Goal 存储适配。 |
| `messages.go` | `adaptMessages`、`restoreMessages`、审批选项转换。 |
