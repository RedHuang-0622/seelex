# 任务书：P21 剩余装配片

## 你的任务

在 `refactor/architecture-reshape` 分支上，继续 `m5-record.md` §45–§51 的 P21
“域方法随类型走”收尾。**剩余的是宿主动作与端口接线，不是判据**；判据已经下沉，
不要把它们搬回装配层，也不要在装配层复制第二份。

## 当前事实

- 最近提交：`3d2a9ab7`（Plan 引擎已删除，fork 直连 AgentNode）。
- P21-1…P21-38 已完成；装配层剩下的主要是：
  - `backend/engine/assemble/runtime_teamwork.go`：
    `RunWorker` / `workerRoleRoundSpec` / `bindWorkerProjectRoot` / `writeWorkerOutput` /
    `teamworkJobOutputs` / `SetTeamworkBackend` 的装配；
  - `backend/engine/assemble/workunit_team_records.go`：存/删/回灌的 I/O；
  - `backend/engine/assemble/runtime_teamwork_items.go` / `workunit_assembly.go`：
    设计文档明确留在装配处的端口实现与三格适配器；
  - `runtime_context.go` 组件工厂、`runtime_tools.go` 注册面。
- `runtime_plan.go` 已不存在；不要按旧文件路径改。

## 工作方式

1. 每次只取一个文件/一组同域函数，先读目标模块 README 的“文件与函数索引”。
2. 判断它属于哪一类：
   - **宿主动作**（开角色会话、绑项目根、写输出、回收现场）→ 留在装配层，但压成薄函数；
   - **判据/词表/状态判定** → 必须在拥有该规则的域包，装配层只转发；
   - **端口接线** → 用窄接口/函数值，不把跨域具体类型带进域包。
3. 如果发现“像判据但不在域包”，先停下来写审计结论，不要顺手复制。
4. 每片更新对应 README；删除的文件/函数在交付里列“文件:行 → 新唯一位置”。

## 优先顺序（按价值）

1. `runtime_teamwork.go` 的判据（作业记录上限、场景清单、派发/汇合入参口径）。
2. `workunit_team_records.go` 的 I/O（记录形状已在 `store`/`contract`）。
3. `runtime_teamwork_items.go` / `workunit_assembly.go` 的端口实现与三格适配器。
4. `runtime_context.go` 组件工厂、`runtime_tools.go` 注册面。

## 验证

```text
go build ./...
go vet ./...
go test ./backend/engine/assemble/ ./backend/multiagent/teamwork/ ./backend/workunit/ -count=1 -timeout 1200s
python scripts/gen_core_readme_index.py --check
python scripts/check_readme_refs.py --strict
```

## 约束

- 不要动 NodeScope/SceneScope 的改名工作（另一个会话负责）。
- 不要恢复 `plan_*` 工具、Plan DAG、Plan 审批门或 replan。
- 一个可解释切片一个 commit；提交到 `refactor/architecture-reshape`，不要 push。

交付时按片列出：搬走了什么、删掉了什么重复实现、哪些仍是装配层宿主动作、测试证据。
