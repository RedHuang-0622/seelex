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
| A | `seelebridge/tools/computer`（+`mcp`，41 文件）→ `backend/tool/computer` | 无 | **已落地**（8468ce72） |
| F | `seelebridge/internal/{actor,config,docker,model,stream,telemetry}` → 模块根 `internal/*` | 无（**它是别的片的前置**） | **已落地**（见 §4） |
| B | `seelebridge/security` → `backend/limit/gate` | V9 未确认 ⇒ 暂缓（§3） |
| C | `seelebridge/{fs,attachment,imageattach,multimodal}` + `runtime_image.go` → `backend/tool/io` | 需与根包拆分协同（`runtime_image.go` 在根包内） |
| D | `seelebridge/tools/websearch` + `seelebridge/search` → `backend/tool/websearch` | `seelebridge/tools/websearch` 只被 `search` 与 `main.go` 引用 |
| E | `seelebridge/{plan,worktree,task,node,fork}` → `backend/tool/*` | `fork` 被 `session` 引用 |
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

## 4. F 片：`seelebridge/internal/*` 上移到模块根（M4 的关键路径）

**为什么必须先做它（编译器给的答案，不是设计偏好）**：第一次搬 `seelebridge/task` →
`backend/tool/task` 时构建直接失败：

```text
backend/tool/task/task.go:17:2: use of internal package
  github.com/RedHuang-0622/seelex/seelebridge/internal/actor not allowed
```

Go 的 `internal` 规则只允许**同一子树**导入。于是所有依赖 `seelebridge/internal/*` 的包
（`task`/`fork`/`plan`/`node`/`worktree`/`account`/`session`/`imageattach`/`tools`…）
在 `internal/*` 搬走之前都出不去。而 plan §2.6 给的落点 `backend/tool/internal/` 只在
"进口方全部落在 tool 板块"时成立——实测进口方横跨 engine / tool / multiagent 三个板块，
且根包（`seelebridge`）在 J 片之前还活着，它也不在 `backend/` 子树内。**唯一能让
`seelebridge/*` 与 `backend/*` 双方都合法引用、且整个搬迁过程构建始终为绿的位置，是模块根
`internal/*`**（模块内任意包都可导入）。因此本轮按"过渡落点 = 模块根 `internal/*`"落地，
终局是否再下沉进某个板块，留给 J 片之后的收口（见 D-M4-8）。

**删除清单（原位置 → 新唯一位置）**：

| 原位置 | 新唯一位置 | 规模 |
|---|---|---|
| `seelebridge/internal/actor` | `internal/actor` | 1 文件 139 行 |
| `seelebridge/internal/config` | `internal/config` | 1 文件 295 行 |
| `seelebridge/internal/docker` | `internal/docker` | 1 文件 180 行 |
| `seelebridge/internal/model` | `internal/model` | 6 文件 257 行 |
| `seelebridge/internal/stream` | `internal/stream` | 1 文件 314 行 |
| `seelebridge/internal/telemetry` | `internal/telemetry` | 6 文件 582 行 |

`seelebridge/internal/mapper`（19 行，单一进口方 `runtime_live.go`）**本轮未动**：plan §2.10
要求"删"，但它的删除要改 `runtime_live.go` 的投影代码（不是纯路径改写），归入 J 片（根包拆分）。

**证据（实跑）**：

- 引用改写：`internal/model` 68 文件、`internal/telemetry` 45 文件、`internal/config` 13 文件、
  `internal/actor` 5 文件、`internal/docker` 4 文件、`internal/stream` 4 文件（含 e2e 门面闭包清单
  与 layout 模块清单）。
- `gofmt -l .`（不含 `tmp/`）干净；`go build ./...`、`go build -tags "gui,desktop,production" ./...`、
  `go vet ./...` 全部无输出。
- `go test ./seelebridge/... -count=1`：**除 `seelebridge/tools` 的既有环境性失败外全绿**
  （下表逐包列出）；`go test ./internal/... -count=1` 全绿（含搬迁后的 6 个包）；
  `go test ./e2e/... -count=1` 全绿。
- 漂移门禁 `scripts/check_readme_refs.py --strict`：本次搬迁**新增 0 条**（现存 12 条里，
  11 条与本方案无关的历史噪声 + 1 条是并发写入方 `store/root` 正在改的用例引用）。

**搬迁后暴露并修掉的两处真实问题**：

1. `internal/config/config_example_test.go` 用 `..\..\..\config\accounts.example.yaml`
   找发行模板（按旧深度三级）。上移一层后路径失效，**用例先红**
   （`open ..\..\..\config\accounts.example.yaml: The system cannot find the path specified.`），
   改成 `..\..` 后绿。这类"相对深度"漂移是纯路径搬迁的典型盲点，只靠 `go build` 抓不到。
2. `internal/model/README.md` 有一句"`seelebridge/internal/*` 按 Go 规则不能被外部 import"
   ——上移后这句话不再成立，已就地更正（形状留在 `contract/dto` 的理由改为"权限判据与适配层
   共用的形状归契约层"，别名与调用点不动）。

**既有失败的环境性证据（不是我这次改动引入的）**：`seelebridge/tools` 的
`TestJobHandlesCarryTheirTableToken` 在本工作区连续 3 次失败（失败面是 Windows 临时目录
`TempDir RemoveAll cleanup` 的文件句柄占用，三个子用例 `process`/`inline`/`subagent` 本身都 PASS）。
在 `git worktree` 出来的**干净 `main`（96fc163b）副本**上同样 3/3 失败 ⇒ 与本轮搬迁无关
（该副本已删除，不留在仓库里）。

| ID | 事项 | 处置 |
|---|---|---|
| D-M4-1 | 首轮替换脚本把 `docs/research/**`、`docs/test/**` 的路径也改写了（这两类是"把当时状态固定下来"的记录）。已 `git checkout` 还原，并把这两类目录加入脚本跳过清单，与 M3 §8.4 登记的口径一致 | 已修 |
| D-M4-2 | plan §2.6 写 `seelebridge/internal/{stream,…}` → `backend/tool/internal/`，**实测不成立**：`internal/model` 有 11 处进口方、`internal/telemetry` 6 处，分布在未来的 engine / tool / multiagent 三个板块；Go 的 `internal` 可见性只覆盖同一父目录，放进 `backend/tool/` 会让另两个板块无法引用 | 待裁决落点（候选 `backend/internal/**`，等 §3.6 的 V 序列一起裁） |
| D-M4-3 | plan §5.7 要求冻结 `teamwork` 域直到该域改动落地且工作区干净。用户 2026-10-16 说明该域改动已完成，且当前工作区除无关文件外干净 ⇒ I 片的冻结按此**解除**。依据是用户声明 + 工作区状态，不是我自己核实的 teamwork 改动内容 | 已解除（若与实际不符请回退 I 片） |
| D-M4-4 | V9（`seelebridge/security` 终局）未获确认 ⇒ B 片不动（plan §5.12：待裁决项不自行补全） | 待裁决 |
| D-M4-5 | V3（`workunit` 归属：`backend/multiagent/workunit` 还是独立 `backend/workunit`）未裁决 ⇒ H/I 片的 `workunit_*` 先不动 | 待裁决 |
| D-M4-6 | J 片不是"搬文件"：`Runtime` 单类型跨 53 文件，拆分需要先定"谁持有 Runtime、各域如何被装配处组合"。建议 J 单独立项、按域多次提交 | 计划性风险 |
| D-M4-7 | M2-D（loop 预警）与 M2-C5（中间件定序）已由用户裁决**延后到 M5 之后**（见 `m2-record.md` D-M2-10）⇒ M4 不碰这两项 | 已知延后 |
