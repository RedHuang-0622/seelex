# M4 记录：tool / engine / multiagent 板块归位

本文是 M4 的实施记录：侦察基线、切片计划、每片的删除清单与证据、未决项。事实来源是工作区
实测（`go list` + 逐包行数），不是 plan §2.5–2.8 的旧清单（那份清单的数字已过时，见 §3）。

## 0. 侦察基线（2026-10-16 实测）

- `seelebridge` 共 **30 个包**：根包 **53 个生产文件 / 13,373 行**；29 个子包共 **27,890 行**生产代码。
- 板块内依赖边 **116 条**；外部依赖边来自 **9 个包**（`application`、`application/core`、
  `application/core/subagent_view`、`application/core/view_state`、`application/console`、
  `internal/adapters`、`wiring`、`e2e/scenario`、根 `main` 包），其中 **6 个直接 import 根包** ⇒
  根包是最后一刀，子包先走。
- 自足叶子包（闭包无 Seelebridge 内依赖）：`tools/computer`（+`tools/computer/mcp`）、
  `attachment`、`multimodal`、`tools/websearch`。其余子包都被根包或彼此引用。
- **关键难点（不是改名）**：根包 53 个文件同属 `package seelebridge`，`Runtime` 是**单个类型跨文件**
  定义。plan §2.5 的"文件级 from → to"因此不能直接执行——把 `runtime_tools.go` 搬到别的包，
  等于把 `Runtime` 拆开。M4 的主体工作量在这里：先按域切出类型/接口、在装配处组合，才谈搬文件。
  这是"`seelebridge` 目录消失"的唯一路径，也是本里程碑不能按"文件 mv"估工的原因。

## 1. 切片计划（每片一个 commit，plan §5.8）

| 片 | 内容 | 前置 | 状态 |
|---|---|---|---|
| A | `seelebridge/tools/computer`（+`mcp`，41 文件）→ `backend/tool/computer` | 无 | **本 commit** |
| B | `seelebridge/security` → `backend/limit/gate` | V9 未确认 ⇒ 暂缓（§3） |
| C | `seelebridge/{fs,attachment,imageattach,multimodal}` + `runtime_image.go` → `backend/tool/io` | 需与根包拆分协同（`runtime_image.go` 在根包内） |
| D | `seelebridge/tools/websearch` + `seelebridge/search` → `backend/tool/websearch` | `seelebridge/tools/websearch` 只被 `search` 与 `main.go` 引用 |
| E | `seelebridge/{plan,worktree,task,node,fork}` → `backend/tool/*` | `fork` 被 `session` 引用 |
| F | `seelebridge/internal/{model,telemetry,config,stream,docker,actor,mapper}` | 归属未定（§3：跨板块共享 ⇒ 不能按 plan 放进 `backend/tool/internal`） |
| G | `seelebridge/{account,mcp,plugin}`、`internal/config` → `backend/engine/*` | 无 |
| H | `seelebridge/{session,workunit}`、`internal/actor` → `backend/multiagent/*` | V3（workunit 归属）未裁决 |
| I | `seelebridge/teamwork`、`runtime_teamwork*.go`、`workunit_*` → `backend/multiagent/teamwork` | 冻结解除依据见 D-M4-3 |
| J | 根包按域拆分（engine / tool / multiagent + 装配） | 需先定"谁持有 `Runtime`"（§3） |
| K | 收口：删 `ports.go`、19 处 `type X = …` 别名、`internal/mapper`、空目录 | J 完成 |

## 2. A 片：computer use 原语层归位

**删除清单（原位置 → 新唯一位置）**：`seelebridge/tools/computer/**`（41 文件：18 生产文件
3,393 行 + 用例 + `README.md` + `mcp/`）→ `backend/tool/computer/**`。同名同包名，`git mv`
保留历史，**同一份实现只换落点**，无第二份实现。

**引用改动**（全部是路径/清单，零逻辑改动）：

- Go 侧：`seelebridge/runtime_computer.go`、`seelebridge/runtime_image.go` 及 3 个根用例
  （`runtime_computer_test.go`、`image_live_smoke_test.go`、`imageattach_live_smoke_test.go`）、
  根 `computer_use_live_smoke_test.go`、`tool_meta_declaration_test.go`、`mcp/main*.go`（import 路径）。
- 门禁：`e2e/facade_boundary_test.go` 的门面闭包允许清单条目改名（**规模不变**）；
  `e2e/layout_test.go` 的 `repositoryModules` 增列 `backend/tool/computer`。
- 文档：根 `README.md`（4 处命令/链接）、`seelebridge/README.md`（2 处目录相对引用）、
  `backend/tool/computer/README.md`、`backend/tool/computer/mcp/README.md`、
  `docs/arch/remote-agent-node-feasibility.md`。

**证据（实跑）**：

- 包自足性：`tools/computer` 的闭包只有标准库 + 三个第三方包（`purego`、`xgb`），
  **没有** Seelebridge 内依赖 ⇒ 搬它不会牵动板块边界。
- `gofmt -l .` 干净；`go build ./...`、`go build -tags "gui,desktop,production" ./...`、
  `go vet ./...` 全部无输出。
- `go test ./backend/tool/computer/... -count=1`：`backend/tool/computer` ok、`.../mcp` ok。
- `go test ./seelebridge -count=1`（全部 import 方所在包）：ok（50.3s）。
- `go test ./e2e/... -count=1`：ok（门面闭包 + 模块清单 + README 门禁全绿）。
- `scripts/check_readme_refs.py --strict`：**11 条**，与本次搬迁前的基线一致（新增 0）。

## 3. 未决 / 风险 / 偏差

| ID | 事项 | 处置 |
|---|---|---|
| D-M4-1 | 首轮替换脚本把 `docs/research/**`、`docs/test/**` 的路径也改写了（这两类是"把当时状态固定下来"的记录）。已 `git checkout` 还原，并把这两类目录加入脚本跳过清单，与 M3 §8.4 登记的口径一致 | 已修 |
| D-M4-2 | plan §2.6 写 `seelebridge/internal/{stream,…}` → `backend/tool/internal/`，**实测不成立**：`internal/model` 有 11 处进口方、`internal/telemetry` 6 处，分布在未来的 engine / tool / multiagent 三个板块；Go 的 `internal` 可见性只覆盖同一父目录，放进 `backend/tool/` 会让另两个板块无法引用 | 待裁决落点（候选 `backend/internal/**`，等 §3.6 的 V 序列一起裁） |
| D-M4-3 | plan §5.7 要求冻结 `teamwork` 域直到该域改动落地且工作区干净。用户 2026-10-16 说明该域改动已完成，且当前工作区除无关文件外干净 ⇒ I 片的冻结按此**解除**。依据是用户声明 + 工作区状态，不是我自己核实的 teamwork 改动内容 | 已解除（若与实际不符请回退 I 片） |
| D-M4-4 | V9（`seelebridge/security` 终局）未获确认 ⇒ B 片不动（plan §5.12：待裁决项不自行补全） | 待裁决 |
| D-M4-5 | V3（`workunit` 归属：`backend/multiagent/workunit` 还是独立 `backend/workunit`）未裁决 ⇒ H/I 片的 `workunit_*` 先不动 | 待裁决 |
| D-M4-6 | J 片不是"搬文件"：`Runtime` 单类型跨 53 文件，拆分需要先定"谁持有 Runtime、各域如何被装配处组合"。建议 J 单独立项、按域多次提交 | 计划性风险 |
| D-M4-7 | M2-D（loop 预警）与 M2-C5（中间件定序）已由用户裁决**延后到 M5 之后**（见 `m2-record.md` D-M2-10）⇒ M4 不碰这两项 | 已知延后 |
