# Store DDD M8–M11 Qoder 顺序执行包

生成时间：2026-10-28 02:05 Asia/Shanghai

## 唯一 worktree

```text
worktree: G:\seelex-worktrees\store-ddd-M8-to-M11
branch:   codex/store-ddd-M8-to-M11
base:     af2bbaab refactor(store-ddd): begin M8 teamwork domain
```

M8→M11 有严格依赖，必须在这个 worktree 中顺序完成。不要把 M9/M10/M11
分别开成从同一 commit 出发的并行 worktree；否则后一里程碑无法安全继承前一里程碑。

## 共同规则

- 先读仓库根 `MEMORY.md`、`AGENTS.md` §0/§5/§8–§10，以及本目录
  `plan.md`、`milestones.md`、`external-change-protocol.md`、`progress.md`。
- 保留用户与其它会话改动；不读取真实配置；不删除 `.seelex`、config、dist 或用户数据。
- 禁止 `git reset --hard`、`git clean`、`make clean/release`、未审计的目录删除。
- 每个里程碑先写会红的契约/所有权/行为门禁，再实现到绿。
- 每批只迁移一个可解释能力面，列出“原位置 → 新唯一位置”删除清单。
- God 类型方法数只降不升；领域包禁止 import 根 `store`、Application、Middle、Backend。
- 路径、JSON tag、head/watermark、wire 和落盘 schema 零变化。
- 修改领域目录之外时，必须遵守 `external-change-protocol.md`：独立 worktree/branch、
  manifest、checkpoint、可重放 patch/脚本、dry-run、apply、rollback 证据。
- 每个里程碑完成时更新 `progress.md`、对应模块 README，运行该里程碑全部门禁，
  然后创建一个只含该里程碑的本地 commit；不 push。
- 门禁失败或出现未决语义时停止并报告，不用兼容 facade、复制实现或清理数据绕过。
- Go 命令使用：

```text
GOCACHE=G:\go-build-cache\seelex
GOTMPDIR=G:\go-build-tmp\seelex
```

## M8 提示词

```text
只执行 SEELEX-STORE-DDD M8 Teamwork，不进入 M9。工作目录必须是
G:\seelex-worktrees\store-ddd-M8-to-M11，分支必须是 codex/store-ddd-M8-to-M11。

先完整阅读 AGENTS.md、MEMORY.md、docs/2026-10-27-store-ddd-split/ 下的
plan.md、milestones.md、external-change-protocol.md、progress.md，以及
store/teamwork、store/teamwork.go、store/teamwork_items.go、
store/team_registry.go、backend/multiagent/teamwork 的 README、源码和测试。

当前已有 checkpoint：
- M7 完成：582c3cf0
- M8 纯模型/校验/folding：af2bbaab
不要重做 store/teamwork 中已存在的 Plan/Event/Binding 类型、DAG 校验和 binding 折叠。

本轮目标：
1. 把 teamwork plan head 整份替换、events.jsonl 只追加、worktrees.jsonl 只追加及
   崩溃残尾读取迁入 store/teamwork；保持 events.jsonl、worktrees.jsonl 和
   plan head 的物理路径与 JSON tag 逐字不变。
2. 把 team registry 的读写/删除与规范化迁入 M8 的明确 owner；如果归 store/role
   更合理，先给领域归属证据，不得为了省事继续挂在根 storeEngine。
3. 根 store 只保留既有 alias/单层转发；不得新增长期兼容 facade。
4. 写红灯所有权门禁：store/teamwork 缺 I/O owner、根仍持有 plan/event/binding
   实现时必须失败；补跨实现 parity、append-only、crash-tail、并发与冷读测试。
5. 确认 plan 整份替换、event 只追加、binding 最后一行折叠、WorkItem DAG 与
   看板归档派生语义不变。

完成门禁：
go test ./store/... ./store/teamwork/... ./backend/multiagent/teamwork/... ./test/ -count=1
go build ./...
go vet ./...

更新 progress.md、store/teamwork/README.md、相关 store README，写删除清单。
全部通过后只提交一个 M8 里程碑 commit，不 push。若需要修改领域外调用点，
严格执行 external-change-protocol.md；若阻塞，停止并报告，不猜语义。
```

## M9 提示词

```text
只执行 SEELEX-STORE-DDD M9 Role/WorkUnit，不进入 M10。开始前确认 M8 已在
codex/store-ddd-M8-to-M11 提交且 progress.md 标记完成；若未完成，停止并报告。

完整阅读 milestones.md §13、plan.md §10、AGENTS.md §8–§10，
以及 store/role_session*.go、store/role_permission.go、store/node_session_store.go、
store/unit_record_writer.go、backend/multiagent/{subagent,agentsession}、
backend/workunit、backend/tool/worktree 的 README、源码和测试。

本轮目标：
1. 新建 store/role，承接 role draft、角色会话子树、permission 与 floor/message
   lifecycle；新建 store/workunit，承接 node/subagent 记录、主会话索引、终端结论。
2. Role sync 仍由 sequencer 唯一写 main message；只有 sync 成功后才删 draft；
   floor 与 message 同步发布。
3. 消除根 storeEngine 上 roleStore/newSubagent 等递归式上帝实现；不得通过复制
   storeEngine 或第二套 role 状态机迁移。
4. 写红灯所有权、draft 半失败、restart recovery、subagent enumeration/tree、
   主会话索引与冷读测试。
5. 根 store 只保留兼容 alias/单层转发和组合根 lifecycle。

完成门禁：
go test ./store/... ./store/role/... ./store/workunit/... ./backend/multiagent/... ./test/ -count=1
go build ./...
go vet ./...

更新 progress、两个新包 README、相关上级 README 与删除清单。
所有门禁通过后创建一个 M9 里程碑 commit，不 push。
```

## M10 提示词

```text
只执行 SEELEX-STORE-DDD M10 Facade 清零，不进入 M11。开始前确认 M9 已完成并提交；
若未完成，停止并报告。

完整阅读 milestones.md §14、plan.md §10、AGENTS.md §8–§10、
progress.md、external-change-protocol.md，以及当前 Router/jsonRepository/
storeEngine/SessionGranularStore 的 ratchet 与直接 import 清单。

本轮目标：
1. 删除 BoardsFor、TeamworkFor、ToolRecordFor 等能力发现出口；生产调用方只持
   自己实际使用的窄端口。
2. 根包删到只剩数据根、原子写、head 封装、layout、锁分片、关闭和必要生命周期；
   删除领域业务判据、万能接口与根 God receiver 上的领域方法。
3. 上层直接 import 根 store 的生产文件归零，或只剩明确登记且可退场的兼容面。
4. 更新架构图、模块 README、import ratchet、God 方法 ratchet 与调用方契约测试。
5. 禁止新增 adapter/facade 换编译；接口由消费方定义，具体实现仅在 wiring 装配。

完成门禁：
go build ./...
go test ./application/... ./middle/... ./backend/... ./store/... ./wiring/... ./e2e/... -count=1
go vet ./...

外部调用点必须走 external-change-protocol.md，并完成回滚演练。
更新 progress 与删除/兼容台账后创建一个 M10 里程碑 commit，不 push。
```

## M11 提示词

```text
只执行 SEELEX-STORE-DDD M11 全仓验收与发布候选检查，不发布、不 push。开始前确认
M10 已完成并提交；若未完成，停止并报告。

完整阅读 milestones.md §15–§18、plan.md、progress.md、AGENTS.md 和
external-change-protocol.md，按发布候选标准做最终审计。

必须执行：
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./... -count=1 -timeout=1200s
go test ./test/ -count=1
node --test gui/frontend/dist/*.test.mjs

额外验证：
1. 旧会话冷读、恢复、fork、压缩、媒体回读。
2. 多进程数据根锁；正常关闭无 goroutine、数据库、MCP、Wails、文件句柄泄漏。
3. God 方法数、直接根 import、领域依赖边达到进度台账目标。
4. 所有用户数据路径、JSON schema、head/watermark、wire 格式零迁移。
5. 逐项核对 M0–M10 删除清单、README、ratchet、回滚证据。

任何红灯都停止发布；不得用清数据、重写 head、删 dist 或跳过测试解决。
全部通过后更新 progress.md，标记 M11 完成并创建一个最终验收 commit，不 push。
```

## 每轮回报格式

```text
结论：Mx 完成 / Mx 阻塞
已完成项：
进行中项：
未完成项：
红灯证据 → 绿灯证据：
原位置 → 新唯一位置删除清单：
God 方法数 / import 边变化：
数据兼容：路径、JSON、head、watermark、用户数据
测试与未跑项：
未决风险与裁决人：
下一步与首次验证命令：
commit：
```
