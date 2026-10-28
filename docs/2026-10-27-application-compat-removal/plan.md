# Application 兼容层删除实施方案

## 设计目标

1. `application/compat` 在终态不存在。
2. `application` 根包不再定义兼容 Frontend，也不定义已无真实调用方的宽入站接口。
3. GUI、TUI、headless、console 各自直接依赖本包使用的能力面。
4. `application/assembly` 只保留组合、共享状态协调和状态 owner；不再实现前端转发方法。
5. test-only forwarding、alias 补丁与“facade 只转发”源码断言删除。
6. 用户可见行为、会话作用域、锁、事件与持久化语义保持不变。
7. 不把 194 个方法或 49 字段兼容快照原样搬到新目录。

## 设计模式

| 模式 | Go 实现 | 应用位置 | 理由 |
|---|---|---|---|
| Facade retirement | 删除门面、调用 owner | application / consumers | 消除第二层名字，不迁移行为 |
| Composition Root | `wiring/bootstrap` 显式构造 | 启动与测试 harness | 依赖只装配一次 |
| Consumer-owned interface | 在 `gui/tui/headless/console/wiring` 声明窄接口 | 各消费方 `dependencies.go` | 避免 producer-owned God interface |
| Data-only component snapshot | `assembly.Components` 分组字段 | assembly → wiring | 传递 concrete owner，不实现方法 |
| Strategy / Port injection | 已有 Application deps | domain packages | 复用现有 owner，不重造 |
| Ratchet | AST / `go list` / directory gate | `application`、`e2e` tests | 防兼容层回流 |

## 方案对比

| 维度 | 方案 A：搬家式 Frontend | 方案 B：共享组件图作为新门面 | 方案 C：消费方依赖 + 组件快照 |
|---|---|---|---|
| 兼容层是否真正消失 | 否 | 中 | 是 |
| 耦合度 | 高 | 中高 | 低到中 |
| 内聚性 | 低 | 中 | 高 |
| 可测试性 | 依赖大 fake | 仍依赖大 graph | 每个前端只 fake 实际能力 |
| 改动面 | 最小 | 中 | 最大 |
| 新 God 风险 | 极高 | 高 | 低，前提是分组且无方法 |
| A/B 行为风险 | 小 | 小 | 分领域迁移后可控制 |
| 可回滚性 | 表面容易 | 中 | 每个 consumer 批次可独立回滚 |

## 推荐：方案 C

推荐理由：

- 直接满足“兼容层不许存在”。
- 前端接口由使用方声明，符合 Go 接口与依赖倒置原则。
- `assembly.Components` 只承载 owner 指针，不承载行为，不形成第二份实现。
- GUI/TUI/headless/console 可按领域分批迁移，避免一次性重写所有行为。

最大风险：GUI 改动面大。缓解方式是把 GUI 的宽接口拆成 State、Conversation、Session、Accounts、Policy、Workspace、Roles、Execution、Tools、Lifecycle 十组，逐组迁移并用 A/B 行为测试守护。

## 终态依赖图

```text
wiring/bootstrap
  ├─ owns config / ports / sequence / shutdown ordering
  ├─ constructs assembly.Service
  ├─ reads assembly.Components
  └─ constructs consumer dependencies:
       gui.Dependencies
       tui.Dependencies
       headless.Dependencies
       console.Dependencies

gui/tui/headless/console
  └─ depend on their own narrow interfaces
       └─ implemented structurally by domain components

application/assembly
  ├─ owns shared state / lock / lifecycle coordination
  └─ exposes concrete component pointers through grouped Components

application/<domain>
  └─ owns use-case behavior and its dependencies

application root
  └─ stable aliases/constants/errors/Service identity only
```

## 秩序与不变量

1. `assembly.Service` 继续是跨组件会话状态、共享锁和生命周期顺序的唯一协调者。
2. 领域组件继续拥有各自用例；前端不得复制状态机、重算 revision/seq 或持有事务状态。
3. `assembly.Components` 是只读装配快照，只允许字段，不允许方法、业务判断或可变单例。
4. 每个 consumer 接口必须由真实调用点推导；单接口 ≤20 方法；没有 fake/consumer 的接口不创建。
5. `wiring/bootstrap` 是构造函数、装配顺序和关闭顺序的唯一位置。
6. 前端只 import `application`/`contract` 类型与自己的子包，不 import assembly/backend/store。
7. `application/compat` 及任何同义名（bridge/frontend facade/legacy adapter）永久禁止回流。
8. 同一判据、状态、转换和恢复顺序只能有一个 owner。
9. 修改行为测试前先证明它是转发测试；纯源码转发断言删除，行为测试迁移。
10. 用户数据、`.seelex`、真实配置与 `dist` 不在本任务写入范围。
11. 不自动 commit/push；A/B 改动只存在于 B 工作树，A 保持基线。

## 核心数据与接口

### assembly 组件快照

只提供 concrete pointers，不提供 UI 方法：

```go
package assembly

type Components struct {
    Admin       AdminComponents
    Conversation ConversationComponents
    Session     SessionComponents
    Execution   ExecutionComponents
    Workspace   WorkspaceComponents
    Policy      PolicyComponents
    Runtime     RuntimeComponents
}

type ConversationComponents struct {
    Unified      *applicationconversation.UnifiedApplication
    Queue        *applicationconversation.QueueApplication
    Submission   *applicationinput.SubmissionApplication
    Compaction   *conversationcontext.Application
    History      *applicationhistory.Application
}

type SessionComponents struct {
    Query       *applicationsessionquery.Application
    Interaction *applicationsession.InteractionApplication
    Fork        *applicationsession.ForkApplication
    Submit      *applicationsession.SubmitApplication
    Archive     *applicationsession.ArchiveApplication
    Delete      *applicationsession.DeleteApplication
    DraftEntry  *applicationsession.DraftEntryApplication
    Lifecycle   *applicationsession.LifecycleApplication
    Meta        *applicationsession.MetaApplication
    Resume      *applicationsession.ResumeApplication
    SSH         *applicationsession.SSHApplication
    View        *viewstate.Coordinator
    Snapshot    *viewstate.SnapshotApplication
}

func ComponentsFor(service *Service) Components
```

其他组同规则。实际字段只在 wiring 有真实消费点时加入；不为了“完整”保留无人使用字段。

### GUI 依赖

接口声明在 `gui`，按 UI 页面与用例拆分：

```go
package gui

type BridgeDependencies struct {
    State        StateView
    Conversation ConversationController
    Session      SessionController
    Accounts     AccountController
    Policy       PolicyController
    Workspace    WorkspaceController
    Roles        RoleController
    Execution    ExecutionController
    Tools        ToolRecordController
    Lifecycle    LifecycleController
}
```

每个字段是小型接口，直接使用 owner 方法名；Bridge 方法调用 `bridge.deps.Session.Resume(...)`，不再经过兼容 Frontend。

### TUI/headless/console

同样各自声明 dependencies。禁止为了复用把三个前端的接口合并回一个 application 级宽接口。

## 特殊入口归属

| 当前兼容入口 | 终态 owner |
|---|---|
| `AddNotice` | `viewstate.Coordinator.AddNotice` |
| `PrepareProviderHistory` | `conversationcontext.EngineHistoryCoordinator.PrepareProviderHistoryFor` |
| `PromptLayers` | `prompt.PromptStack.Layers` |
| `SetProjectBoardSink` | bootstrap 直接调用 project board 组件注入方法 |
| `SetToolRecordSnapshotStore` | `toolrecord.Application.SetSnapshotStore` |
| `CloseRoleToolRecordUnit` | 在 `toolrecord.Application` 增加 `CloseRoleUnit(main, role)`，组合 key 的判据只保留一处 |
| `PublishRuntimeProjections` | 保留为 assembly 内部协调方法；仅 bootstrap 的首次发布通过显式 assembly projection host 触发，不挂到 UI graph |
| `RefreshRuntimeSnapshot` | `admin.RuntimeApplication.Refresh` |
| `SelectAccount` / `SetAccountWindow` | `admin.AccountApplication`；parity 批保持当前 Background 语义，后续单独决定是否修 ctx |
| `SessionSSH*` | `session.SSHApplication` + `workspace.Application` |
| `HandleRoleToolActivity` | `execution.RoleActivityApplication.Handle` |
| `TeammateSessionLive*` | `execution.WorkItemApplication.SessionLive/SessionPage` |

## 文件级实现步骤

| # | 步骤 | 主要文件 | 设计模式 |
|---|---|---|---|
| 1 | 固化 A/B 与结构红测 | `scripts/application-compat-ab.ps1`（新增）、`application/forwarding_boundary_test.go`、`e2e/facade_boundary_test.go` | Ratchet |
| 2 | 建 assembly 分组组件快照 | `application/assembly/frontend_ports.go` → `application/assembly/components.go` | Data snapshot |
| 3 | wiring/bootstrap 直接消费 owner | `wiring/constructors.go`、`wiring/bootstrap/*.go`、`wiring/registry/registry.go` | Composition Root |
| 4 | headless/console 迁移 | `headless/*.go`、`console/*.go` | Consumer-owned interface |
| 5 | TUI 迁移 | `tui/ui.go`、`tui/stream.go`、`tui/dialog.go`、panel 与 tests | Consumer-owned interface |
| 6 | GUI 状态/会话组迁移 | `gui/bridge.go` 的 Snapshot/Subscribe/Session 方法组 | Strategy/Port |
| 7 | GUI 账户/策略/工作区组迁移 | `gui/bridge.go` 对应方法组 | Strategy/Port |
| 8 | GUI 角色/执行/工具/生命周期组迁移 | `gui/bridge.go`、`gui/shutdown.go`、`gui/terminal_bridge.go` | Strategy/Port |
| 9 | 删除 test-only forwarding 与 alias 补丁 | `application/assembly/*_legacy_test.go`、`*_test_aliases*.go`、`frontend_migration_test.go` | Ratchet |
| 10 | 删除根门面与 compat | `application/application.go`、`faces_api.go`、`frontend_assertions.go`、`compat/` | Facade retirement |
| 11 | 文档与门禁收口 | application/assembly/front 模块 README、e2e/contract tests、budget JSON | Documentation/Ratchet |

## 里程碑

### M0：A/B 与红测

退出条件：

- A/B 路径、commit、命令和基线结果记录；
- `application/compat` 存在时新结构测试必红；
- test-only Service/Frontend 转发扫描覆盖 `_test.go`；
- 行为测试分类清单完成。

### M1：组合面替换

退出条件：

- bootstrap/registry 不再调用 `compat.New`；
- assembly 只暴露分组组件数据；
- headless/console 可独立编译；
- A 行为测试仍绿。

### M2：TUI 替换

退出条件：

- `tui.NewModel` 接收 consumer-owned dependencies；
- 所有 fake 按窄字段装配；
- `go test ./tui -count=1` 与 A 行为一致。

### M3：GUI 替换

退出条件：

- `gui.Bridge` 不再持有 100+ 方法 Application；
- optional interface 探测被字段能力或显式 constructor validation 取代；
- GUI、headless RPC、关闭、终端测试全部绿；
- 行为 diff 不包含产品语义变化。

### M4：兼容面物理删除

退出条件：

- `application/compat` 不存在；
- `application.Frontend`、`assembly.Frontend`、`FrontendPorts` 不存在；
- `frontendOfTestService` 与全部 test-only forwarding/alias 文件不存在；
- application 根包旧宽接口不存在；
- 全仓 grep 零 compat。

### M5：终验与 A/B 报告

退出条件：

- A、B 跑同一组测试；
- B 的 build/vet/application/gui/tui/headless/console/wiring/e2e/test 全绿；
- `gofmt -l .`、`git diff --check`、README 引用检查通过；
- 删除清单全部勾选；
- 交付列明删除重复实现“旧位置 → 唯一 owner”的结构证据。

## 测试策略

### A 基线

```text
go test ./application/... -count=1 -timeout=120s
go test ./gui ./tui ./headless ./wiring/... -count=1 -timeout=120s
```

### B 终验

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./application/... -count=1 -timeout=120s
go test ./gui ./tui ./headless ./console ./wiring/... -count=1 -timeout=120s
go test ./e2e -count=1 -timeout=120s
go test ./test/ -count=1
node --test gui/frontend/dist/*.test.mjs
python scripts/check_readme_refs.py --strict
git diff --check
```

环境不满足时明确记录 skip 原因，不把 skip 写成 pass。

### 结构门禁

```text
rg -n 'application/compat|applicationcompat|compat\.Frontend' --glob '*.go' --glob '*.md' .
rg -n 'type Frontend struct|FrontendPorts|frontendOfTestService' application
rg -n '^func \(frontend \*Frontend\)' application
go list -deps ./application
go list -deps ./gui
go list -deps ./tui
```

新增 AST 门禁必须同时扫描 production 与 `_test.go`，防止“生产删掉、测试 shim 保留”的假完成。

## 回滚

- A 工作树保持未删除基线，可直接恢复任意单文件。
- B 每个 milestone 独立形成未提交变更集；失败时只回退该 milestone。
- 回滚不回退业务 owner 修复；只恢复消费者接线。
- 禁止用删除 `.seelex`、重写 head、清理用户数据来掩盖失败。

## 验收反例

出现任一情况不得宣称完成：

- `application/frontend` 或 `application/bridge` 新包承载原 194 方法；
- 仅仅兼容 Frontend 改名为 Application/Components 且仍有 UI 方法；
- 生产调用归零但 test-only shim 继续满足旧测试；
- 删除行为测试以换取全绿；
- GUI/TUI 直接 import assembly 绕过 Application 边界；
- 组件快照含状态、锁、业务判断或可变单例。
