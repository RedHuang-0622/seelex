> 历史记录，不描述当前实现。本文中的工作树与旧路径仅用于追溯迁移现场。

# Application 兼容层退场交接

日期：2026-10-27

## 工作树

| 角色 | 路径 | 状态 |
|---|---|---|
| A：删除前基线 | `C:\Users\redre\.codex\worktrees\application-compat-removal\seelex` | 干净，保留在 `39e5afe0` |
| B：当前工作快照来源 | `C:\Users\redre\.codex\worktrees\application-compat-removal-b\seelex` | 有大量未提交迁移；已完成状态已复制到 C |
| C：下一会话工作树 | `C:\Users\redre\.codex\worktrees\application-compat-removal-c\seelex` | 与 B 的 84 个 tracked 变更和 10 个新增文件一致；下一会话只在这里继续 |
| 文档事实源 | `G:\Program\go\seelex\docs\2026-10-27-application-compat-removal\` | 位于 G 盘项目目录 |

不要继续修改 B。B 是交接前的快照；后续修改全部进入 C。

## 当前结论

完成度：**M0 完成，M1/M2/M3 大部分完成，M4/M5 未完成**。

已经完成的方向：

- `assembly.FrontendPorts` 已删除，替换为按限界上下文分组的 `assembly.Components`。
- `Components` 只含 owner 指针分组，不再包含 UI 方法或函数闭包。
- wiring/bootstrap、registry、console、TUI、headless、GUI 已建立大量 consumer-owned dependencies。
- `toolCommandOutcomeObserver`、`CloseRoleUnit`、`PublishInitialRuntimeProjections` 等特殊入口已开始回到明确 owner。
- `session_history_test_aliases_test.go` 已删除，部分测试已迁移到 owner 包。

仍未完成：

- `application/compat` 仍存在，约 1082 行、193 个函数。
- `application.Frontend` 和 `assembly.Frontend` 仍存在。
- `application/faces_api.go` 的旧宽接口仍在。
- 24 个 test-only alias/legacy 文件仍在。
- 150 个 assembly 测试文件仍引用 `frontendOfTestService`。
- README 仍有 27 处把 compat 描述为当前实现。
- GUI 的 `Dependencies.App` 仍是过渡宽门面。
- GUI 当前有一个明确失败：`EditUserMessageAndFork` 的测试没有装配 `MessageEdit`。

## 当前 diff 规模

C 工作树：

```text
82 modified
2 deleted
10 untracked
84 tracked files changed
1307 insertions
1590 deletions
```

主要新增文件：

```text
application/assembly/components.go
application/compat_retirement_test.go
console/dependencies.go
gui/dependencies.go
gui/dependencies_test.go
gui/headless_ports_test.go
headless/dependencies.go
scripts/application-compat-ab.ps1
tui/dependencies.go
wiring/bootstrap/frontend_ports.go
```

## 已执行测试

通过：

```text
go test ./application/assembly -count=1 -timeout=180s
go test ./tui -count=1 -timeout=180s
go test ./headless -count=1 -timeout=180s   # 无测试文件，但编译通过
go test ./console -count=1 -timeout=180s
go test ./wiring/config ./wiring/registry -count=1 -timeout=180s
```

失败：

### GUI 行为失败

```text
TestBridgeForwardsEditUserMessageAndFork
panic at gui/bridge.go:665
bridge.deps.MessageEdit is nil
```

对应测试：

```text
gui/bridge_message_edit_test.go
```

修法：该测试应把 stub 填入 `deps.MessageEdit`；不能恢复类型断言或旧兼容 Frontend。

### Windows 文件锁失败

```text
wiring/adapters TestAccountAdminDetailsUpdateDeleteRoundTrip
rename .accounts-*.yaml -> accounts.yaml: Access is denied
```

这是 Windows 上偶发的临时文件 rename 失败，先原样重跑确认；不要为此改账号语义。

### 预期红测

```text
go test ./application -run 'TestApplicationCompatibilitySurfaceIsRetired|TestAssemblyHasNoTestOnlyForwardingFixtures' -count=1
```

当前按设计失败，指出：

- compat import 未清；
- assembly.Frontend 未删；
- 旧宽接口仍在；
- 24 个 test-only forwarding/alias 文件仍在；
- 27 处 README 仍描述旧 compat 现状。

## 必须维持的边界

1. 不把 `compat/frontend.go` 整体搬到新的 frontend/bridge 包。
2. `assembly.Components` 只能含 owner 指针字段；不得增加 UI 方法、闭包或业务判据。
3. GUI/TUI/headless/console 的接口必须由各自消费方声明。
4. `wiring/bootstrap` 只做依赖装配，不承载业务状态。
5. 测试直接调用 owner；禁止重新引入 `frontendOfTestService` 这类转发 helper。
6. 不删除真实行为测试。纯“旧名字存在/只转发”的测试删除后，行为用例必须迁入 owner 包或直接调用 owner。
7. 不改变 session/project scope、锁、事件 revision/seq、持久化和关闭顺序。
8. 不 commit/push；除非用户另行明确要求。
9. 不触碰 `.seelex`、真实配置、`dist` 或 G 盘项目现有的其他未提交改动。

## 下一会话顺序

### N1：修 GUI 当前红灯

文件：

```text
gui/bridge_message_edit_test.go
gui/dependencies.go
gui/bridge.go
wiring/bootstrap/frontend_gui.go
```

目标：

- `depsFor` 为 `MessageEdit` 填 stub；
- 未装配时明确报错，不 panic；
- `go test ./gui -count=1 -timeout=180s` 绿。

先写红灯并保留现有失败作为证据；修根因，不恢复 optional type assertion。

### N2：清 GUI 过渡 App

文件：

```text
gui/dependencies.go
gui/bridge.go
wiring/bootstrap/frontend_gui.go
gui/*_test.go
```

目标：

- 删除 `Dependencies.App`；
- 删除旧 `gui.Application` 宽接口和所有 optional interface 探测；
- 剩余 GUI 方法按 owner 直接接线；
- 所有 fake 只填实际依赖字段；
- `go test ./gui` 绿。

### N3：清 application 根兼容面

文件：

```text
application/application.go
application/faces_api.go
application/frontend_assertions.go
application/query_faces_test.go
application/api_partition_test.go
application/api_contract_test.go
```

目标：

- 删除 `applicationcompat` import 与 `Frontend` alias；
- 删除旧四组及 AP-002 宽接口；
- 保留真正公开的类型 alias，如 `ContextCompactionResult`；
- 更新门面类型身份测试。

### N4：清 assembly test-only 转发

删除：

```text
frontend_migration_test.go
frontend_shell_legacy_test.go
service_*_legacy_test.go
snapshot_legacy_test.go
submission_legacy_test.go
subscription_legacy_test.go
*_test_aliases*.go
*_test_alias*.go
service_helpers_test.go 中的 frontendOfTestService
```

对 150 个引用点逐一改为：

```text
service.components.<owner>.<method>
```

或 owner 包的直接测试。不得新建测试专用 Frontend。

### N5：删除 compat 与 assembly.Frontend

文件：

```text
application/compat/
application/assembly/service.go
application/assembly/frontend_ports.go（已删除）
```

删除后全仓 grep 必须归零：

```text
application/compat
applicationcompat
compat.Frontend
assembly.Frontend
FrontendPorts
frontendOfTestService
```

### N6：文档和终验

更新所有模块 README，删除“当前 compat”叙述，只保留历史文档中的明确历史标记。

终验：

```text
gofmt -l .
git diff --check
go build ./...
go vet ./...
go test ./application/... -count=1 -timeout=180s
go test ./gui ./tui ./headless ./console ./wiring/... -count=1 -timeout=180s
go test ./e2e -count=1 -timeout=180s
go test ./test/ -count=1
python scripts/check_readme_refs.py --strict
```

还要用 A/B 脚本比较基线：

```text
powershell -File scripts/application-compat-ab.ps1 `
  -BaselineRoot C:\Users\redre\.codex\worktrees\application-compat-removal\seelex `
  -CandidateRoot C:\Users\redre\.codex\worktrees\application-compat-removal-c\seelex
```

## 完成判定

只有以下证据同时成立才算完成：

- `application/compat` 目录不存在；
- `application.Frontend`、`assembly.Frontend`、`FrontendPorts` 不存在；
- application 根包旧宽接口不存在；
- 24 个 test alias/legacy 文件和 `frontendOfTestService` 全部删除；
- 消费方直接依赖 owner，不存在新的万能门面；
- 结构红测转绿；
- A/B 行为测试一致，B 全量 build/vet/test 绿；
- 删除清单完成，README 不再把 compat 描述为当前架构。
