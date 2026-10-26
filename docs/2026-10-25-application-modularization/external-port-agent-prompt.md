# Backend/Middle 外部端口任务提示词

你负责为 Seelex Application 拆除 `application/core` 的直接内部依赖。不要继续在
Application 内复制状态机或建万能 adapter；目标是让 Backend/Middle 暴露稳定窄端口，
由 `wiring/adapters` 注入。

开始前阅读：

- `AGENTS.md`、`MEMORY.md`；
- `docs/2026-10-25-application-modularization/port-migration-plan.md`；
- `application/README.md`、`application/core/README.md`；
- `middle/switch/history/README.md`、`middle/switch/sessionusecase/README.md`、
  `middle/residency/README.md`；
- `backend/engine/{context,react,session,task}` 对应 README；
- `backend/multiagent/*`、`backend/limit/*`、`store/README.md`。

## 总体约束

1. 先列真实调用方，再定义端口；没有调用方的未来接口禁止创建。
2. 端口只使用 `contract`、`contract/dto`、标准库或纯值类型。
3. 同一语义只保留一份实现；adapter 只转发，不重新判定。
4. 不改 GUI/TUI wire、事件 revision/seq、持久化格式。
5. 每批只删一个直接依赖，先写红测/import 门禁，再补端口、adapter 并转绿。
6. 每批报告依赖数、core 文件/行数、首调用方、删除项和回退方式。

## 已完成，不要重做：History Window

`application/history.WindowAssembler`、`middle/switch/history.Assembler` 和 wiring
注入已完成；`application/core` 已不再直接 import `middle/switch/history`。
只做回归，不要重新定义第二套接口。

目标：删除 `application/core` 对 `middle/switch/history` 的直接依赖。

先实现：

```go
type WindowAssembler interface {
    ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int)
    Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int)
    DurableRows(messages []model.Message) []model.Message
    IndexOfSameRow(rows []model.Message, want model.Message) int
}
```

实现要求：

- `middle/switch/history` 新增唯一 `Assembler`，逐项调用现有纯函数；
- `application/history.Deps` 改为持有 `WindowAssembler`；
- wiring 注入 adapter；
- 删除 core 的 `middle/switch/history` import。

测试：接缝不匹配窗口不动、可见总数不倒退、空占位不参与内容对位、冷读不持
`ViewMu`、原函数与 adapter 同输入同输出。

## 当前第一批：Session/Residency

目标：删除 `middle/switch/sessionusecase`、`middle/residency` 直接依赖。

按消费方拆端口，不做完整 Service 镜像：

- `SessionCatalogPort`：List/Meta/WaitRefresh；
- `SessionHost`：CurrentView/Create/Activate/Restore/Release；
- `ResidencyPort`：resident/content order、touch、evict、protected keep predicate。

迁移顺序：

1. 只读 catalog；
2. residency preview；
3. resident/content LRU；
4. archive/unload/delete；
5. resume/fork/cold restore/draft。

测试：项目 scope 隔离、release 幂等、LRU 保护状态、restore intent、submit during
restore、切换失败回滚和 race。

## 当前第二批：Engine/Runtime

目标：删除 `backend/engine/{context,react,session,task}` 直接依赖。

建议端口：

- `ContextPort`：history append/replace、compact、provider history、input index；
- `ReactPort`：run/cancel；
- `SessionRuntimePort`：open/activate/snapshot/resume/fork/transcript/history tail/release；
- `TaskPort`：active plan、tool observation、restore、task context、close task。

实现要求：

- 具体状态仍归 Engine；
- Application 只使用 DTO；
- adapter 在 wiring；
- 先只读，再写路径；
- 慢 I/O 不持 `ViewMu`；
- cancel/stale request/fork/restore 顺序必须有 parity 测试。

## 当前第三批：Multiagent / Limit / Store

目标是删除以下具体类型依赖：

- `backend/multiagent/{goal,subagent,team,teamwork}` → GoalPort、SubagentPort、
  TeamPort、TeamworkPort；
- `backend/limit/*` → LimitsPort、PolicyPort、ApprovalPort；
- `store` → SessionRepository、EventRepository、ToolResultRepository、
  BoardRepository。

硬要求：

- Application 中不出现具体 `goal.*`、`limit.*`、`store.*` 类型；
- 档位词、状态词、上限表只有一份；
- repository key 显式包含 projectID + sessionID；
- store 负责事务、关闭、flush 和批量写；
- DTO JSON 字段和现有 wire 完全一致。

## 交付格式

每个端口批次必须给出：

1. 删除的直接 import；
2. 新增端口、唯一实现和 adapter；
3. 首调用方；
4. 删掉的重复判据；
5. 红测失败证据和绿测命令；
6. core 依赖/文件/行数前后；
7. 是否修改 GUI/TUI wire；
8. 回退点。

任何端口若需要完整 `Service`、完整 Engine、完整 session state，或必须在
Application 复制 history/residency/permission 判据，立即停止并报告，不得硬做。
