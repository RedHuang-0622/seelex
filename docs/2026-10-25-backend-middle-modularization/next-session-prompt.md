# Backend / Middle 优化下一步会话提示词

日期：2026-10-26

以下内容可直接作为新会话的初始任务。

---

继续实施 `G:\Program\go\seelex` 的 Backend / Middle 领域化与契约化拆分。当前 BM-001 至 BM-004
已完成；BM-005 只完成 56 个 `backend/engine/assemble` 生产文件的迁移归属盘点和反向 import 门禁，
尚未创建 `wiring/runtime`，也尚未移动文件。下一步仍必须先通过契约优先的设计门禁。

开始前必须：

1. 阅读根目录 `AGENTS.md`、`MEMORY.md`，重点执行 §9「契约优先的拆分与迁移纪律」。
2. 阅读：
   - `docs/2026-10-25-backend-middle-modularization/plan.md`
   - `docs/2026-10-25-backend-middle-modularization/implementation.md`
   - `docs/2026-10-25-backend-middle-modularization/runtime-migration-inventory.md`
   - `contract/runtime_migration_test.go`
   - `backend/engine/assemble/README.md`
   - 与首个切片直接相关的 domain README、contract、测试和调用方。
3. 先执行 `git status`，保留用户和并行会话改动；不要回滚、清理或覆盖无关文件。
4. 不并行修改 `backend/engine/assemble/runtime*.go`、`backend/multiagent/teamwork/items.go`、
   `backend/engine/task/task_context_state.go` 等高冲突文件。

## 本阶段目标

推进 BM-005，但先解决“最终归属”而不是整包搬目录。当前 `middle → backend = 0`、
`backend → application = 0`；backend → middle 仍有 3 条 assemble 接线与 3 条 event hub 边，
应保持显式预算，不得为迁移新增反向依赖。

### 第一步：设计门禁

先给出一份设计报告，至少包含：

1. 领域归属判定：
   - 纯构造、依赖注入、启动/关闭顺序 ⇒ `wiring/runtime`；
   - 会话/任务/工作项的不变式 ⇒ 对应 backend 领域聚合；
   - 跨域流程和外部动作 ⇒ Application 用例或 process manager；
   - 工具注册 ⇒ `wiring/registry`；
2. 首批切片的 boundary：
   - 首选一个低风险子域，例如 `teamwork_job_namer/signals`、`review_files`，
     或一个纯 runtime lifecycle 文件组；
   - 不得首批整体移动 `runtime_teamwork.go`、`runtime_role_turn.go` 或全部 `runtime_*.go`；
3. 新增/修改的 DTO、消费方 port、interface 和关键 struct；逐个写明真实调用方；
4. 聚合不变量与状态所有者，尤其 WorkUnit、Teamwork、Subagent、Session 的边界；
5. 下游影响矩阵：backend 域包、Application、Wiring、event、store、测试、README；
6. 迁移顺序、兼容 alias（D0–D4 等级）、删除里程碑和回退方式；
7. 对 `runtime-migration-inventory.md` 分类的确认或修正。

若“Application process manager”与“backend domain aggregate”之间存在争议，先报告候选方案和取舍，
不得直接把所有 `runtime_teamwork*` 原样搬进 Application。

### 第二步：只实施首个获批切片

设计要求：

- 先写红测和依赖门禁；
- 契约/interface/struct 先落，再移动实现；
- 旧 `backend/engine/assemble` 路径保留一行转发或同身份 alias；
- 具体依赖在 composition root 注入，领域包不 import `wiring/runtime`；
- 不复制生命周期判据，不制造第二份作业命名、恢复或工作树状态机；
- 每完成一个切片同步模块 README、implementation 和 runtime inventory；
- 禁止自动 commit/push，除非用户当前明确要求。

## 验证

至少运行与风险匹配的：

```text
gofmt -l backend middle contract wiring
go test ./contract -run 'TestBM00' -count=1
go test ./backend/... ./middle/... ./wiring/... -count=1
go test -race ./backend/multiagent/teamwork ./backend/engine/assemble ./middle/view/... -count=1
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
python scripts/check_readme_refs.py --strict
git diff --check
```

完成后报告：契约与 struct 清单、首个切片的领域归属、移动/删除项、依赖边变化、真实测试结果、
尚未裁决的聚合边界。若遇契约或产品语义冲突，停在设计门禁并明确请求裁决。
