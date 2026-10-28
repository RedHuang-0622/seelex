# 下一会话实施提示词

```text
继续实施 Seelex Application 兼容层彻底退场。

只在新工作树工作：

C:\Users\redre\.codex\worktrees\application-compat-removal-c\seelex

只读基线：

C:\Users\redre\.codex\worktrees\application-compat-removal\seelex

不要修改 B 工作树：

C:\Users\redre\.codex\worktrees\application-compat-removal-b\seelex

开始前阅读：

1. G:\Program\go\seelex\AGENTS.md
2. G:\Program\go\seelex\MEMORY.md
3. G:\Program\go\seelex\docs\2026-10-27-application-compat-removal\front-review.md
4. G:\Program\go\seelex\docs\2026-10-27-application-compat-removal\plan.md
5. G:\Program\go\seelex\docs\2026-10-27-application-compat-removal\deletion-list.md
6. G:\Program\go\seelex\docs\2026-10-27-application-compat-removal\handoff.md

当前状态：

- M0 已完成，结构红测已经建立。
- FrontendPorts 已删除，替换为分组 Components。
- wiring/bootstrap、registry、console、TUI、headless、GUI 已大量迁移到 consumer-owned dependencies。
- application/assembly 已编译且测试通过；tui、headless 编译、console 测试通过。
- GUI 当前红灯：gui/bridge_message_edit_test.go 没有给 Dependencies.MessageEdit 填 stub，
  gui/bridge.go:665 空指针 panic。
- wiring/adapters 有一次 Windows 临时文件 rename Access denied；先重跑确认。
- application/compat 仍有约 1082 行、193 个函数。
- 24 个 test alias/legacy 文件、150 个引用 frontendOfTestService 的测试仍存在。
- application.Frontend、assembly.Frontend、faces_api.go 旧宽接口仍未删除。

工作顺序：

N1. 修 GUI MessageEdit 依赖，先跑红灯，再补依赖，保持未装配显式报错，消除 panic。
N2. 删除 GUI Dependencies.App、旧 Application 宽接口和 optional type assertion，
    所有 Bridge 方法直接依赖 owner。
N3. 删除 application 根 compat import、Frontend alias、frontend_assertions 和旧宽接口；
    调整 query/API 结构测试。
N4. 删除 assembly 的 frontend_migration_test、legacy/alias 测试文件和 frontendOfTestService；
    行为测试改用 service.components.<owner> 或迁到 owner 包。
N5. 删除 application/compat 与 assembly.Frontend。
N6. 更新 README，运行 A/B 与全量验证。

硬性约束：

- 禁止把 193 个方法整体搬到 application/frontend、application/bridge 或其他兼容包。
- 禁止新增万能 Frontend/Application 接口。
- Components 只能含 owner 指针，不允许方法、函数闭包、业务判据或锁。
- 消费方接口归消费方；单个接口不超过 20 方法。
- 不删除真实行为测试；只删除纯转发/旧名字/旧接收者测试。
- 不改变用户可见行为、wire、session/project scope、revision/seq、持久化和关闭顺序。
- 不 commit/push，不触碰 .seelex、真实配置、dist 或 G 盘项目已有未提交改动。

先运行当前基线：

go test ./application/assembly ./wiring/... ./console -count=1 -timeout=180s
go test ./gui ./tui ./headless -count=1 -timeout=180s
go test ./application -run 'TestApplicationCompatibilitySurfaceIsRetired|TestAssemblyHasNoTestOnlyForwardingFixtures' -count=1

每次修复后报告：

1. 旧转发入口 -> 新唯一 owner；
2. 删除的文件与方法；
3. 新增/修改的 consumer dependency；
4. 红测失败证据与转绿结果；
5. compat 函数数、test alias 数、frontendOfTestService 引用数；
6. 未完成项和下一步。

完成时必须证明：

- application/compat 不存在；
- application.Frontend、assembly.Frontend、FrontendPorts 不存在；
- 旧宽接口不存在；
- test alias/legacy 与 frontendOfTestService 全部删除；
- 结构红测全绿；
- A/B 测试结果一致，B 全量 build/vet/application/gui/tui/headless/console/e2e/test 通过。
```
