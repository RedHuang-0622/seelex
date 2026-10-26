# Application 模块化下一步会话提示词

日期：2026-10-26

以下内容可直接作为新会话的初始任务。

---

继续实施 `G:\Program\go\seelex` 的 Application 领域化与契约化拆分。当前 AP-001、AP-002、
AP-004、AP-005、AP-006 已完成；AP-003 的验收口径仍需在本次设计中明确；Conversation、
Session 生命周期和聊天主循环尚未迁移。不要把本任务理解为“继续拆几个文件”，先完成契约优先的设计门禁。

开始前必须：

1. 阅读根目录 `AGENTS.md`、`MEMORY.md`，重点执行 §9「契约优先的拆分与迁移纪律」。
2. 阅读：
   - `docs/2026-10-25-application-modularization/plan.md`
   - `docs/2026-10-25-application-modularization/implementation.md`
   - `application/README.md`
   - `application/core/README.md`、`README-service.md`、`README-session.md`
   - `backend/engine/assemble/runtime_role_turn.go`、`runtime_agent_conversation.go`
   - `application/core/chat.go`、`session_scope.go`、`service_state.go`、`service_components.go`
3. 先执行 `git status`，保留工作区已有改动；不要回滚、清理或覆盖无关文件。
4. 本会话不负责消息一键复制、编辑历史用户输入、Codex 式 fork UI；这部分由另一会话处理。

## 本阶段目标

为 AP-007「Conversation 聚合」完成设计门禁，并在门禁通过后只实施第一个低风险、可回退切片。

### 第一步：只做设计和契约冻结

在不修改生产语义的前提下，提交一份设计报告，至少包含：

1. `Conversation`、`ChatTurn`、`Queue`、`HistoryWindow`、`Session` 的领域边界；
2. 每个可变状态的唯一所有者、锁归属、生命周期和恢复来源；
3. 聚合不变量，例如：
   - 一个会话同时只有哪些运行状态；
   - 排队输入、当前回合、历史窗口之间的次序；
   - 中断/恢复与 Session 释放的边界；
   - 事件 revision/seq 的唯一写入点；
4. 拟新增或修改的 `contract/dto`、消费方 port、Application interface 和关键 struct；
5. 明确每个新 interface 的真实调用方，禁止创建暂无人使用的“未来接口”；
6. 下游影响矩阵：GUI/TUI、Runtime、SessionStore、EventHub、Snapshot reducer、恢复路径与测试；
7. 兼容层台账：旧 `Service` 方法、新接口、首调用方、删除里程碑、归零验证、回退方式；
8. AP-003 的明确验收口径。建议区分：
   - 入站 API 只用 `contract/*`；
   - `application/core` 内部直接依赖数另设指标。
   不要把这两件事混为一个“依赖为零”口号。

若以上任一项与现有计划冲突，先报告冲突和候选方案，不得改产品语义。

### 第二步：第一个实施切片

设计报告中选定并说明第一个切片。优先考虑“只搬状态所有权、不改 wire 与用户行为”的切片，
例如 ChatState/queue 的查询与状态收拢；本批不直接迁移 `chat.go` 主循环。

实施要求：

- 先写红测：结构门禁、旧方法兼容断言或行为 parity 用例；
- 契约/DTO/interface/struct 先落，再迁移实现与装配；
- 旧 `Service` 方法保留一行转发，GUI/TUI 零行为变化；
- 每个迁移完成后同步目标模块 README 和 implementation；
- 列出删除的重复实现：`文件:行 → 新唯一位置`；
- 高冲突文件 `chat.go`、`service*.go`、`session*.go` 单车道修改；
- 禁止自动 commit/push，除非用户当前明确要求。

## 验证

至少运行与风险匹配的：

```text
gofmt -l application contract
go test ./application/... ./contract/... ./e2e -count=1
go test -race ./application/... -count=1
go build ./...
go vet ./...
python scripts/check_readme_refs.py --strict
git diff --check
```

完成后报告：契约与 struct 清单、聚合边界、删除/合并项、真实测试结果、仍存在的语义歧义；
未完成时明确停在哪个设计门禁，不要用“测试绿”替代结构结论。
