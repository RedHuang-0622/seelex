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
| B | `seelebridge/security` → `backend/limit/gate` | V9 未确认 ⇒ **暂缓**（§3） |
| C | `seelebridge/{fs,attachment,imageattach,multimodal}` → `backend/tool/io/*` | 无 | **已落地**（dde48d63；`runtime_image.go` 在根包内，随 J 片走） |
| D | `seelebridge/search` + `seelebridge/tools/websearch` → `backend/tool/{search,websearch}` | 无 | **已落地**（31c37b2f） |
| E | `seelebridge/{task,fork,plan,node,worktree}` → `backend/tool/*` | 无（F 先行） | **已落地**（be5e6741 / 78718881 / 40f6bb33 / ea87386f / f9c4aef1） |
| G | `seelebridge/account` → `backend/engine/account`（G1） | 无 | **已落地**（c4c10618）；G2（`mcp`/`plugin`）见 §5.3 |
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

## 5. E / C / D / G1 片：tool 与 engine 的叶包群（均已落地）

### 5.1 删除清单（原位置 → 新唯一位置；全部 `git mv`，同一份实现只换落点）

| 片 / commit | 原位置 → 新唯一位置 | 规模 |
|---|---|---|
| E1 `be5e6741` | `seelebridge/task` → `backend/tool/task` | 3 生产文件 1211 行 + 3 用例 |
| E2 `78718881` | `seelebridge/fork` → `backend/tool/fork` | 4 生产文件 654 行 + 4 用例 |
| E3 `40f6bb33` | `seelebridge/plan` → `backend/tool/plan` | 13 生产文件 2695 行 + 4 用例 |
| E4 `ea87386f` | `seelebridge/node` → `backend/tool/node` | 5 生产文件 955 行 + 1 用例 |
| E5 `f9c4aef1` | `seelebridge/worktree` → `backend/tool/worktree` | 1 生产文件 1121 行 + 10 用例 |
| C `dde48d63` | `seelebridge/{fs,attachment,imageattach,multimodal}` → `backend/tool/io/{同名}` | 6 生产文件 812 行 |
| D `31c37b2f` | `seelebridge/search` → `backend/tool/search`；`seelebridge/tools/websearch` → `backend/tool/websearch` | 9 生产文件 819 行 |
| G1 `c4c10618` | `seelebridge/account` → `backend/engine/account` | 4 生产文件 648 行 |

每片一次"移动 + 引用改写 + README/门禁清单跟随 + 局部测试"的循环，引用改写规模：
plan 31 文件、node 23、fork 17、account 16、task 14、worktree 27、search+websearch 11、
io 组 6（含 e2e 门面闭包清单与 `layout_test.go` 的模块清单）。

### 5.2 每片的验证口径（都一样，故不重复列）

`gofmt -l`（不含 `tmp/`）干净 → `go build ./...` → `go vet ./...` 无输出 → 该包自身
`go test` → 进口方所在包（`./seelebridge`、`./seelebridge/session`、`./seelebridge/tools`、
`./application/core` 等按片）→ `go test ./e2e/...` → `scripts/check_readme_refs.py --strict`
**新增条目为 0**。已知既有红只有两处：`application/core` 的基线 3 例、`seelebridge/tools`
的 1 条环境性失败（D-M4-11）。

### 5.3 还没做的（按阻塞性质分类，不是遗漏）

1. **等裁决**：`seelebridge/security`（V9 / B 片）、`seelebridge/workunit` 归属（V3 / H 片）、
   `seelebridge/mcp` 与 `seelebridge/plugin` 的落点（plan 让它们进 `backend/engine/{mcp,plugin}`，
   而顶层 `mcpstack/`、`plugin/` 也指向同一目录 ⇒ 需要"主包 + 适配子目录"的命名裁决）、
   `internal/*` 的终局落点（D-M4-8）。
2. **需要先拆再搬（不是纯移动）**：`seelebridge/scheduler`（plan §2.5/§2.12.1 要求拆成
   actor+触发判定 / `FileStore` / `Persistence` 接口三处）、`seelebridge/tools`（13 文件 5076 行，
   按注册表 / 路由 / 作业 / 权限门控拆）、`internal/mapper`（plan §2.10 要求删，删它要改
   `runtime_live.go`）。
3. **J 片（根包拆分）**：`seelebridge` 根包仍是 53 个生产文件 / 13,373 行，`Runtime` 单类型跨文件；
   这一片决定"seelebridge 目录消失"能否达成，需要先定"谁持有 `Runtime`"。
4. **I 片（teamwork）**：用户已说明该域改动完成（D-M4-3），可以搬；但它仍依赖 `workunit`
   的归属（V3），所以排在 H 之后更省事。

## 6. 收尾三个提交与跨 commit 审计

### 6.1 收尾提交

| commit | 内容 | 为什么需要它 |
|---|---|---|
| `e525aa6e` | 补 M4-G1 漏暂存的根级用例注释引用 | 账号那一片引用改写覆盖了根级 `README.md` 与 `repro_session_background_test.go`，但提交时只暂存了目录级改动 ⇒ `c4c10618` 那棵树的注释仍指向已搬走的包（**编译不受影响，但"每个提交自洽"被破坏**）。教训：暂存要按"改写脚本报告的文件清单"逐条来，不能按目录粗筛 |
| `da34e1c1` | 散文引用跟随：66 处 `seelebridge/<已搬包>` → 新落点（30 个文件） | 搬运器覆盖的是 import 路径、带斜杠的盘相对路径、带引号的包路径；注释与 README 句子里**直接点名包**的写法（`见 seelebridge/task.NextAutoID`、`` `seelebridge/fork` ``）漏了。规则加了词边界：`seelebridge/task` 不会误伤仍在的根包文件 `seelebridge/task_registry.go`（`_` 属词内字符） |
| `301c3f09` | 修 plan/task 域在 2026-P2b（`26de0f09`）时代遗留的 6 处根包文件名引用 | 既有漂移（不是本轮引入）：那些文件在 plan/task 下沉子包时就不在仓库根了。顺带**如实登记**：`ARCHITECTURE_REVIEW.md` 原先给节点预算上限写的守卫用例 `plan_input_fuzz_test.go` 不存在，该分支当前**没有测试覆盖**，不冒充覆盖 |

### 6.2 跨 commit 悬空引用审计（`tmp/reshape/m4_commit_audit.py`）

判据：某包在提交 C 的树里已不存在（已搬走）⇒ C 的树里不应再出现 `seelebridge/<pkg>`
字面量（时间封存文档除外）。做法是 `git grep` 每个提交的树，逐包检查。

- **HEAD 的残留 = 0**（此前 6 处是根包文件名误匹配，已在 6.1 的第三个提交里改掉）。
- 历史提交里仍有散文残留（越早的提交越多）——这是**已提交的历史**，不重写；能保证的是：
  每个提交的**编译**都是干净的（import 路径当次就改对了），散文引用在 `da34e1c1` 收口。

### 6.3 本轮结束时的实况

- `seelebridge/` 从 23 个子目录缩到 **8 个**：`internal`（只剩 `mapper`）、`mcp`、`plugin`、
  `scheduler`、`security`、`teamwork`、`tools`、`workunit`（外加 `.seelex/` 数据目录）。
- 搬出 12 个包（含 `internal/*` 6 个）：tools/computer、internal/{actor,config,docker,model,stream,telemetry}、
  task、fork、plan、node、worktree、fs、attachment、imageattach、multimodal、search、tools/websearch、
  account、session。
- 根包 **53 个生产文件 / 13,373 行未动**（J 片的活），`Runtime` 仍是单类型跨文件。

| ID | 新增偏差 | 处置 |
|---|---|---|
| D-M4-12 | 散文引用是一类独立于"路径字面量"的漂移面：`见 seelebridge/xxx.Fn`、`` `seelebridge/yyy` `` 这类写法既有编译器看不见，也**部分**被 README 门禁漏掉（门禁只查带文件后缀的字面量与 md 链接） | 已补工具 `tmp/reshape/m4_bare_refs.py`（带词边界）+ 跨 commit 审计 |
| D-M4-13 | 提交时漏暂存根级文件 ⇒ 单个提交的树里出现悬空注释（编译不受影响） | 已补 `e525aa6e`；后续按脚本报告清单逐条 `git add` |

## 7. 追加三片（B / T1 / T2）与两条新偏差

| commit | 内容 | 证据要点 |
|---|---|---|
| `531de9f6` | **B 片**：`seelebridge/security`（7 生产文件 909 行）→ `backend/limit/gate/`（包名仍为 `security`） | V9 的前置依据：那次 security 改动已落地（`554cd976`，2026-10-15 23:39，改 `process_tree_windows.go` 与其用例），且该目录无未提交改动。引用改写 16 文件（含 `seelebridge/{runtime,scheduler,tools}`、`backend/tool/worktree`、`internal/docker`、`tool_full_chain_test.go`、e2e 两条清单） |
| `77430c23` | **T1 片**：`seelebridge/json_object.go`（176 行）+ 用例 → `internal/jsonobj/`，入口改名 `decodeJSONObjectLenient` → `DecodeLenient` | plan §2.5 点名的第一块"模块级叶子"。facade 闭包按"新模块可 +1"抬到 71（自身闭包 = 纯标准库）；新包 README 里对原文件的引用带"已移除"标记，漂移新增 0 |
| `fd96de6f` | **T2 片**：删除 `seelebridge/internal/mapper`（19 行 + README），唯一函数 `ToolEventToDTO` **就地内联**进唯一调用方 `runtime_live.go` 的 `toolEventToDTO` | plan §2.10 明写"删"。删后 `seelebridge/internal/` 整个目录消失；facade 闭包允许清单删条目、**上限跟着降** 71 → 70 |
| `421a1201` | 修 `531de9f6`/`da34e1c1` 两处**毯式替换的语义损伤** | 见 D-M4-15 |
| `17c8da1c` | 决策简报（四项待裁决 + J 片设计前提，各附证据与建议） | [m4-decision-brief.md](m4-decision-brief.md) |

| ID | 新增偏差 | 处置 |
|---|---|---|
| D-M4-14 | `backend/limit/gate/` 的**目录名与包名不一致**（目录按 plan §2.8 取名 `gate`，包名保持 `security`） | 只挪目录不动标识符（§5.1"改名不是搬迁"）；是否把包名统一到 `gate` 留给用户（M3 有过 `sessionstore`→`store` 这类**成对改名**的先例，但那要连字符串形式的门禁一起改） |
| D-M4-15 | **毯式路径替换会伤到语义**：plan/历史的引述句被一起改掉（`backend/limit/README.md` 那条"规划，尚未实现：seelebridge/security → gate/"被改成自指废句；`loop_test.go` 注释"迁移前 seelebridge/fork"里的**历史名**也被替换） | 两处已按语义重写（`421a1201`）；其余 66+7 处逐行看过。后续规则：时间/计划引述句里的旧路径**不要**跟着改 |
| D-M4-16 | facade 闭包上限一次上抬（`internal/jsonobj` 新模块 +1：70 → 71），同片 T2 又因删包降回 70 | 上下限都写进常量处的理由，来源可查 |

**本片结束时的实况**：`seelebridge/` 只剩 **6 个包**——`mcp`、`plugin`、`scheduler`、`teamwork`、
`tools`、`workunit`（外加 `.seelex/` 数据目录）；`seelebridge/internal/` 已整体消失。
根包仍是 53 个生产文件 / 13,373 行，等 J 片。

**收尾全量验证**（`go build -tags "gui,desktop,production" ./...` + `go test ./... -count=1`）：

- 带标签构建通过。
- 全量测试只有两个失败集合：`application/core` 的**基线 3 例**（本轮开工前即红）与
  `seelebridge/tools` 的 `TestJobHandlesCarryTheirTableToken` / `TestBgManageRefusesHandlesFromOtherTables`。
- 后两条已被证明是**负载敏感的 Windows 临时目录句柄问题**，不是搬迁引入：① 它们在
  `-run` 单跑与机器安静时整包跑**都通过**（本轮实测：安静时 `go test ./seelebridge/tools/`
  ok 38.3s）；② 高负载时在**干净 `main`（96fc163b）副本**上同样失败（3/3）。失败面是
  `TempDir RemoveAll cleanup` 的 `unlinkat ... being used by another process`，子用例本身全 PASS。

## 8. scheduler 拆分（两片）与四条新偏差

### 8.1 持久化面先拆（`91f1ee2e`）

| 原位置 | 新唯一位置 | 内容 |
|---|---|---|
| `seelebridge/scheduler/persistence.go` 的 `ScheduledTaskRecord` | `contract/dto/scheduled_task_record.go` | JSONL 行形状（一行一变更 + `deleted` 墓碑） |
| 同文件的 `Persistence` 接口 | `contract/scheduled_task_store.go` | 端口改名 `ScheduledTaskPersistence`（泛名不进契约层，D-M4-18） |
| 同文件的 `FileStore` / `NewFileStore` / `Append` / `Load` / `Path` | `store/schedule/file_store.go` | 逐字同一份实现；端口断言写在实现自己的包里（D-M4-19） |
| 同文件的纯 FileStore 用例 | `store/schedule/file_store_test.go` | 残尾丢弃、缺文件 = 空集、未配置路径 = 空操作 |
| 依赖 `State` 夹具的六条用例 | 留在调度器包（现 `backend/engine/schedule/global_jsonl_test.go`） | 经 `State` 的集成面（定义/取消/编辑/重启恢复） |

**plan §2.12.1 有一处内部矛盾**（D-M4-17）：它要求 `Persistence` 接口 → `contract/`、同时
`ScheduledTaskRecord` → `store/schedule/`。端口签名用到记录，而契约层不能反向依赖 store
——两者不能同时成立。本轮按"形状随端口走"把记录放进 `contract/dto`（它本来就是线上/落盘
形状），`store/schedule` 只 import 契约层。

### 8.2 引擎侧整包搬（`62722544`）

`seelebridge/scheduler/**` → `backend/engine/schedule/**`，包名 `scheduler` → `schedule`；
文件名 `scheduler.go/scheduler_test.go` → `schedule.go/schedule_test.go`，
`persistence_test.go` → `global_jsonl_test.go`。接线面只有 `seelebridge/{runtime.go,ports.go}`
两处：**字段名也叫 `scheduler`**（`r.scheduler.X`）必须原样保留——改包名不等于改字段，
毯式替换会在这里出错。

| ID | 新增偏差 | 处置 |
|---|---|---|
| D-M4-17 | plan §2.12.1 的"接口进 contract + 记录进 store"不能同时成立 | 记录随端口进 `contract/dto`，理由写进两个文件的头注 |
| D-M4-18 | 端口在契约层改名 `Persistence` → `ScheduledTaskPersistence` | 契约层不出现泛名；实现与调用点同批改 |
| D-M4-19 | 端口断言写在**实现自己的包**里（plan 说写在装配处） | 换实现改不动这条断言；接口漂移立刻编译失败 |
| D-M4-20 | **故意不**把 `store/schedule` 登记进 `e2e/store_boundary_test.go` 的 I6 scope | 它的直接依赖 = 标准库 + `contract` 系（干净）；`go list -deps` 的传递闭包会被 `contract` 自身的污染带脏，那笔账已由 `contractClosureAllowList` 单独管着，抄第二份就是"同一件事判两处"。理由写在常量处，与 `store/workspace` 同款 |
| D-M4-21 | 门禁的"任意同名文件"兜底会**掩盖**既有悬空引用：`scheduler_test.go` 一改名，`application/core/agentteam/README.md` 里三行"已删除文件名单"立刻显形 | 已把那两行并成一行并与"已删除"标记同行（语义不变，漂移门禁反而少两条） |
| D-M4-22 | plan §2.12.1 还想把 `store/schedule_events.go` 搬进 `store/schedule/` | **本轮未搬**：它是 `storeEngine` 的结构事件写方法（`scheduleEventCommit`），与 store 的单写者纪律、`Key`/`structuralEventKind` 内部类型绑在一起；搬出去要暴露 store 内部实现，属"先拆再搬"，需要你点头 |

**本片结束时的实况**：`seelebridge/` 只剩 **5 个包**——`mcp`、`plugin`、`teamwork`、`tools`、
`workunit`（外加根包与 `.seelex/` 数据目录）。facade 闭包上限因 `store/schedule` 新模块 +1
（70 → 71），同片删 `mapper` 时降过一次，现在这张表的每次上下调都能在常量处读到理由。

## 9. T3：`seelebridge/tools` 整包归位（`4f6832fa`）

规模：**13 个生产文件 5076 行 + 33 个用例文件**，是 M4 里最大的一包。它是"一包多域"（注册表 / 路由 /
异步作业 / job 契约 / 权限主体 / 可见性策略 / 壳事实），plan §2.6 要求切成 `backend/tool/{registry,jobs}`
再加权限段并入 `backend/limit/policy`。本轮先做**整包物理归位**（`seelebridge/tools` →
`backend/tool/registry`，包名 `tools` → `registry`），二次拆分留作后续切片——理由：包内这些文件
互相引用私有类型，直接切会让一个 commit 同时背"搬迁 + 解耦"两件事。

**证据**：进口方只有 **18 个文件 / 54 个调用点**（搬前实测），所以可以干净改名而不是留兼容别名。
`gofmt` 干净、`go build ./...`、`go vet ./...` 无输出；`go test ./backend/tool/registry/...` ok（36.9s，
含原先那条环境敏感的作业句柄用例）；`go test ./seelebridge` ok；`go test ./e2e/...` ok；
根包 `-run 'Permission|ToolMeta|Job|Async'` ok；README 漂移 12 条（新增 0）。

| ID | 新增偏差 | 处置 |
|---|---|---|
| D-M4-23 | 包名 `registry` 与 `Runtime` 的**字段名** `registry`（Seele 工具注册表）在同一文件里并存 | 导入方统一别名 **`toolreg`**：把"字段遮蔽包名"这类静默错误一次掐掉（`runtime_tools.go` 一个文件里两者都要用） |
| D-M4-24 | 首轮替换脚本按"跨行别名"匹配，把上一行的标识符当别名：`internal/telemetry/diagnostic.go` 的 Seele import 被吃掉、e2e 清单元素被写成 `toolreg "路径"` | 构建立刻报错；`git checkout` 还原 18 个文件后改**逐行**规则（只有 import 行才加别名；列表元素只换引号内的路径）。教训与 D-M4-15 同族：路径替换要么按行、要么按"引号内的字面量"，不能用跨行正则 |
| D-M4-25 | plan §2.8 要把 `permission_policy.go`（员工主体/授权面）并入 `backend/limit/policy/` | **本轮未做**：它与 `registry_state.go` 的 `PermissionGate`（执行门）互锁，属于"先拆再搬"，排 T4 |

**本片结束时的实况**：`seelebridge/` 只剩 **4 个包**——`mcp`、`plugin`、`teamwork`、`workunit`
（外加根包与 `.seelex/` 数据目录）。T4 计划：`registry` 内部再切 `backend/tool/jobs`（异步执行 /
job 契约 / 作业工具）+ 权限判据段并入 `backend/limit/policy`。

## 10. G2：`mcp` / `plugin` 归位；T4 的耦合实测

`seelebridge/{mcp,plugin}` → `backend/engine/{mcp,plugin}`（`068ba79e`）：两包共 3 个生产文件
714 行 + 3 用例，进口方只有 `seelebridge/{runtime.go,ports.go,runtime_role_plugins.go}`、
`runtime_tools_test.go`、`backend/tool/registry/role_plugins_test.go` 与 e2e 清单，属纯搬迁。
落点说明：final nesting（决策简报的方案 A/B）仍待裁决——本轮先落在板下，
无论选哪个方案板级落点都在这，之后若选 A 只需再挪一层。

**本片结束时的实况**：`seelebridge/` 只剩 **2 个包**——`teamwork` 与 `workunit`（外加根包
52 个生产文件与 `.seelex/` 数据目录）。这两包都不是"能直接搬"的状态：

- `workunit`：等 V3 裁决（归属），且与根包的 6 个 `workunit*` 文件同域；
- `teamwork`：与根包的 11 个 `runtime_teamwork*.go`、`workflow_job_*` 同域，要随 J 片一起动。

### 10.1 T4（`registry` 二次拆分）的耦合实测——它是"拆"不是"搬"

`backend/tool/registry` 现有 13 个生产文件 5076 行，plan §2.6 要求切成
`backend/tool/{registry,jobs}` 再加权限段并入 `backend/limit/policy`。实测跨组引用：

- **作业组（7 文件，2720 行）引用注册表组的 14 个符号**：`Router`（type）被 5 个作业文件用、
  `Deps`、`ResolveShellFact`/`DialectHint`（shell_fact.go），以及 `router.go` 的
  `newProcessTreeCommand` / `resolveNodePath` / `scopedBashCommand` / `scopedToolTimeout` /
  `serialBashBudgetLabel` / `sessionKey` / `startWithProcessTree`。
- **权限组（`permission_policy.go` 601 行 + `policy.go` 172 行）引用另外两组 8 个符号**：
  `PermissionGate` / `Enforce` / `Middleware` / `rebuildLocked` / `sessionFromContext`
  （`registry_state.go`）、`Write`（`async_run.go`）、`sessionID`（`job_contract.go`）。

⇒ 直接按文件切会让一个 commit 同时背"搬迁 + 抽接口"，且要在 8–14 个私有符号上做归属判断。
本轮按 §5.12 不自行补全：把切法与建议写进
[m4-decision-brief.md](m4-decision-brief.md) §6，等裁决。

## 11. J1：根包整体落位 `backend/engine/assemble`（`436058df`）

**删除清单**：`seelebridge/*.go`（52 个生产文件 **13,373 行** + **121 个用例文件**）+
`seelebridge/README.md` → `backend/engine/assemble/**`（174 个文件，`git mv` 保留历史；
包名 `seelebridge` → `assemble`）。导入方统一保留**显式别名 `seelebridge`**，所以
**35 个进口方、197 个调用点零改动**——这一步只动"包在哪"，不动"谁怎么调它"。

**为什么先做这一步**：plan §2.5 给 4 个装配文件点名的落点就是 `backend/engine/assemble/`；
本步是 **staging**（先整体落位、再按域下沉，即决策简报 §4 的 B→A）。它**不决定类型结构**，
也不与任何最终方案冲突——无论各域最终怎么分，这两个方向的移动都要发生。J 的类型拆分
（谁持有 `Runtime`）仍待裁决，本步没碰。

**搬迁暴露的两类"按层数写死"的路径**（`go build` / `go vet` 都看不见，跑用例才现形）：

1. `assembly_chain_real_partition_test.go` 的 `const realPartitionRoot = "../plugins"`
   ——4 条真划分链路用例全红（`没有 curated.yaml`），改为从仓库根拼；
2. 同一文件的 `plugin.LoadCuratedFromRoot(filepath.Join("..", "plugins"))` —— 复用上一条的根。

顺手把仓库里既有的 `e2e.repoRoot()`（写死"测试包在仓库下第 1 层"）收成**一处实现**：
新增 `internal/testutil.RepoRoot()`（从 CWD 向上找第一个 `go.mod`，与层数无关），
e2e 的 `repoRoot()` 改为转调它——一处实现、两个调用方（AGENTS §8.3）。

**引用改写**：35 个进口方；58 个文件的 165 行现行文档/注释；assemble README 自身的 3 条相对
链接（`../backend/limit/overlay/README.md` → `../../limit/overlay/README.md` 等）；e2e 门面闭包
清单与 layout 模块清单换名字。历史叙述 24 行按 D-M4-15 原样保留。

| ID | 新增偏差 | 处置 |
|---|---|---|
| D-M4-26 | 路径替换的历史标记是**启发式**：`gui/README.md` 那行只因正文里出现"真实请求历史"被判成历史，漏改（门禁抓到） | 已单独修正；规则收紧为"标记须与路径同句且指代迁移"，但仍是启发式——**门禁是最后一道保险** |
| D-M4-27 | `docs/arch` 里 23 行指向"从未在现行树里存在过的旧文件名"（`seelebridge/actor.go`、`seelebridge/todo_tool.go`、`seelebridge/plugins.go` …） | **既有漂移**（先于本轮），与本方案无关；登记不动（门禁不查 docs/arch）。要清就单独立项，且需要逐行判断当时的意图 |
| D-M4-28 | 别名 `seelebridge` 现在指向包 `assemble`（35 个文件） | 过渡命名：等 J2 下沉完成、门面收窄后统一改标识符（一次机械改名），现在改只会制造 197 处无谓改动 |

**证据**：`gofmt` 干净；`go build ./...`、`go vet ./...` 无输出；`go test ./backend/engine/assemble`
**55.7s ok**（含 4 条真划分链路用例）；`go test .`（根包）**148s ok**；
`./application/...` 除基线 3 例外全 ok；`./internal/...`、`./gui/...`、`./tui/...`、
`./store/...`、`./e2e/...` 全 ok；README 漂移 **12 条（新增 0）**。

**现状**：`seelebridge/` 只剩 **`teamwork`、`workunit` 两个包**与 `.seelex/` 数据目录。
J2（把 assemble 里的各域下沉到 engine/tool/multiagent）需要"谁持有 `Runtime`"的裁决；
`workunit` 需要 V3；`teamwork` 需要 J2 一起做。

| ID | 事项 | 处置 |
|---|---|---|
| D-M4-1 | 首轮替换脚本把 `docs/research/**`、`docs/test/**` 的路径也改写了（这两类是"把当时状态固定下来"的记录）。已 `git checkout` 还原，并把这两类目录加入脚本跳过清单，与 M3 §8.4 登记的口径一致 | 已修 |
| D-M4-2 | plan §2.6 写 `seelebridge/internal/{stream,…}` → `backend/tool/internal/`，**实测不成立**：`internal/model` 有 11 处进口方、`internal/telemetry` 6 处，分布在未来的 engine / tool / multiagent 三个板块；Go 的 `internal` 可见性只覆盖同一父目录，放进 `backend/tool/` 会让另两个板块无法引用 | 待裁决落点（候选 `backend/internal/**`，等 §3.6 的 V 序列一起裁） |
| D-M4-3 | plan §5.7 要求冻结 `teamwork` 域直到该域改动落地且工作区干净。用户 2026-10-16 说明该域改动已完成，且当前工作区除无关文件外干净 ⇒ I 片的冻结按此**解除**。依据是用户声明 + 工作区状态，不是我自己核实的 teamwork 改动内容 | 已解除（若与实际不符请回退 I 片） |
| D-M4-4 | V9（`seelebridge/security` 终局）未获确认 ⇒ B 片不动（plan §5.12：待裁决项不自行补全） | 待裁决 |
| D-M4-5 | V3（`workunit` 归属：`backend/multiagent/workunit` 还是独立 `backend/workunit`）未裁决 ⇒ H/I 片的 `workunit_*` 先不动 | 待裁决 |
| D-M4-6 | J 片不是"搬文件"：`Runtime` 单类型跨 53 文件，拆分需要先定"谁持有 Runtime、各域如何被装配处组合"。建议 J 单独立项、按域多次提交 | 计划性风险 |
| D-M4-7 | M2-D（loop 预警）与 M2-C5（中间件定序）已由用户裁决**延后到 M5 之后**（见 `m2-record.md` D-M2-10）⇒ M4 不碰这两项 | 已知延后 |
| D-M4-8 | `seelebridge/internal/*` 的**终局**落点：本轮按编译器约束落在模块根 `internal/*`（过渡解，见 §4）。终局是否再下沉进某个板块，等 J 片（根包拆分）之后收口 | 待收口 |
| D-M4-9 | plan §2.6 写 `seelebridge/search` 与 `seelebridge/tools/websearch` **都进** `backend/tool/websearch/`——一个目录装不下两个包。本轮按"一包一目录、目录名 = 包名"落成 `backend/tool/search/` 与 `backend/tool/websearch/`，语义仍是同一板块 | 需点头 |
| D-M4-10 | **相对链接深度漂移**（本轮抓到的第二类纯路径盲点）：README 里的 `../../x` 在搬迁后少一级/多一级。门禁抓到两处（`backend/tool/search` 的 `../../docs/research/...`、`backend/tool/websearch` 的 `../../search/README.md`），已改为 `../../../` 与 `../`。`go build` 与 `go vet` 都看不见这类错 | 已修 |
| D-M4-11 | `seelebridge/tools` 的 `TestJobHandlesCarryTheirTableToken` 在当前环境**稳定失败**（失败面是 Windows `TempDir` 清理时的句柄占用，三个子用例本身全 PASS）。在干净 `main` 副本上同样 3/3 失败 ⇒ 与本轮搬迁无关，不在本轮修 | 既有环境性 |

## 12. J2：按域下沉（J2a + J2b）——"谁持有 Runtime"的可执行答案

J1（§11）只把根包整体落位，没动类型结构。J2 才是 M4 的出口条件所在：
**`Runtime` 的 352 个方法与 `tools` 的 5,076 行按域分到各板块且无跨板块回指**。

本记录给出 J2 的**可执行答案**（不再等裁决）：一个域 = 四步，每片一个 commit。

1. **端口先行**：在目标包定义宿主端口（只列这个域真正要问的几格读面）。
2. **状态随方法走**：域状态（队列、绑定、映射）搬进目标包的一个类型，不再躺在 `Runtime` 字段里。
3. **装配侧只留两件事**：持有域组件 + 决定它的**拓扑位置**（挂在链的哪一层、什么时候包装）。
4. **用例跟着被测对象走**，断言逐条不变；只钉"装配接线"的用例留在 assemble。

判据仍是 plan §5.1 的"行为零变化"：不动判定、不动协议、不动持久化格式；`Runtime` 的公开面
**允许增加只读访问器**（宿主端口需要），不允许出现第二个名字（同款实现只能有一处）。

### 12.1 J2a：`backend/engine/obs`（`5e502dda`，已落地）

`request_log.go`（184 行）+ `session_telemetry.go`（68 行）**完全不提 `Runtime`**（方法、参数、
字段都没有），所以是 J2 里唯一"纯搬"的一片。导出面收口：`requestLogger → RequestLogger`、
`openRequestLogger → OpenRequestLogger`、`requestLogEnv → RequestLogEnv`，并新增
`NewRequestLogCompleter` / `NewRequestLogStreamCompleter`（私有字段留在包内）。调用点只有
`assemble/runtime.go`（4 处）与 `internal/adapters/engine_port.go`（3 处）。门面闭包上限 71 → 72。

### 12.2 J2b：`backend/tool/io/media`（第一片"带宿主端口的域下沉"）

**删除清单（原位置 → 新唯一位置）**

| 原位置 | 新唯一位置 | 说明 |
|---|---|---|
| `backend/engine/assemble/runtime_image.go`（**181 行 / 7 个函数**，已删除） | `backend/tool/io/media/media.go` | 属于域面的六格改为 `SessionMedia.{Attach,PendingCount,StoreMedia,AttachPending,LoadMedia,ProjectIDFor}` |
| `Runtime.images *imageattach.Registry` 字段 | `SessionMedia.pending`（私有、唯一一份队列状态） | 装配器不再直接持有队列；入队路径只剩 `SessionMedia.Attach` 一处 |
| `Runtime.durableHistoryRouter()`（10 个调用点） | `Runtime.HistoryRouter()`（**同一份实现**，只去掉第二个名字） | 它同时成为 `media.Host` 的一格（AGENTS §8.3） |
| `wrapImageAttachments` 的方法体 | `media.SessionMedia.Wrap`（返回挂好的一对）；方法壳留在 `runtime.go` | 拓扑位置（request log 之内、`agent.NewWithComponents` 之前）是装配决策，不进域包 |
| 用例 `TestStoreSessionMediaWritesIntoExecutingSessionPartition`、`TestMediaProjectIDForResolution`、`TestAttachSessionImageQueuesForExecutingSession` | `backend/tool/io/media/media_test.go`（**断言逐条不变**，夹具从 `&Runtime{...}` 换成 `stubHost`） | 同一份用例换落点 |
| 用例 `TestWrapImageAttachmentsWiresMediaLoader` | **留在 assemble**，改为断言"装配器把 `Wrap` 的结果装回了 `completer` 且 Loader 非空" | 它钉的是装配接线，不是域行为 |

**唯一新增公开面**：`media.Host`（两格：`HistoryRouter` / `SessionProjectID`）、`media.New(host)`、
`Runtime.SessionProjectID`；新增用例 `TestWrapKeepsStreamChain`（流式链保留 Stream/StreamEvents、
返回同一个包装、未装配不造包装）与 `TestNilMediaIsInert`（零值四格：空操作 / 显式错误）。

**证据（实跑）**：`gofmt -l backend e2e` 干净；`go build ./...`、`go vet ./...` 无输出；
`go test ./backend/tool/io/media/...` ok；`go test ./backend/engine/assemble/...` ok（41.9s）；
`go test ./backend/tool/computer/... ./internal/adapters/...` ok；`go test ./e2e/...` ok
（含门面闭包 ratchet I1 与模块 README 门禁）；全量 `go test ./... -count=1` 的**失败集合与改动前
逐条相同**（`backend/tool/registry` 的 D-M4-11 环境性失败 + `application/core` 基线 3 条）；
`scripts/check_readme_refs.py --strict` **12 条（新增 0）**。

**新偏差**

| ID | 新增偏差 | 处置 |
|---|---|---|
| D-M4-29 | 闭包清单那条"新模块必须是干净叶子"的口径，`backend/tool/io/media` 不完全满足：它要用 `store` / `backend/tool/computer` / `imageattach` / `internal/{model,telemetry}` 才能做归属与落盘 | 记偏差而不拆散它：这些下游**全在既有清单内**，闭包只多它自己一格，上限 72 → 73 并在常量处写明理由。"干净叶子"是给"抽判据"写的口径，域下沉不适用 |
| D-M4-30 | plan §2.6 把 `runtime_image.go` 指向 `backend/tool/io/`，但那只是目录（四个子包各占一层，自己没有包） | 落成 `backend/tool/io/media/`（一包一目录，目录名 = 包名），与 C 片的 `io/{fs,attachment,imageattach,multimodal}` 同款 |
| D-M4-31 | 迁移操作本身踩的坑：Python `Path.write_text` 在 Windows 上把 `\n` 写成 `\r\n`，6 个被批量改名的文件从 LF 变 CRLF，`gofmt -l` 立刻报红（内容 diff 只有 1 行，可逆） | 已按"工作树 = LF"归一化；教训：**批量改写 Go 文件后必须跑 `gofmt -l` 并归一化行尾**，这是纯路径改写唯一的机械保险 |
| D-M4-32 | `Runtime` 因域下沉新长出两条导出访问器（`HistoryRouter` / `SessionProjectID`） | 宿主端口的必然代价，且是收窄方向：域包拿到的是两格只读事实，不是整个 `Runtime`。与 K 片（删 `ports.go`）无耦合 |

## 13. M4-W：`workunit` 归位（契约 + 唯一实现合成一包）

M4 只剩两个包（`workunit`、`teamwork`）。本片做 `workunit`：plan §2.7 点名"`seelebridge/workunit`
（4 文件 595）+ 根包 `workunit_*.go`（5 文件 1065）+ `runtime_unit_recovery.go`（99）→
`backend/multiagent/workunit/`"；本片落地为**独立 `backend/workunit/`**（偏差见 D-M4-W1）。

### 13.1 删除清单（原位置 → 新唯一位置）

| 原位置 | 新唯一位置 | 说明 |
|---|---|---|
| `seelebridge/workunit/{contract,classify,progress,session}.go`（4 生产文件）+ 4 个用例 + README | `backend/workunit/` 同名文件（`git mv`，**包名不变、内容零 diff**） | 纯路径搬迁，22 处 import 里的包限定符零改动 |
| `backend/engine/assemble/workunit_parent.go`（200 行，`lifecycleHost` = 生命周期的唯一一份实现） | `backend/workunit/lifecycle.go`（类型 `Host`） | 唯一实现的判据（重入 / 合并只做一次 / 分类 / 策略 / 恢复粒度）一字未改；只剩"同包化"改写：包名、自指 import 去掉、`workunit.` 限定符去掉、端口与构造导出 |
| `workunit_assembly.go` 里的三格端口**接口声明**（`sceneFace` / `unitFace` / `recordFace`） | `backend/workunit/ports.go`（`SceneFace` / `UnitFace` / `RecordFace`，**导出**） | 接口跟着实现走；装配处已在另一个包，不导出就注入不进来 |
| 装配侧的常量 `teamUnitStatusRunning`（= `workunit.StatusRunning.String()`）在实现里的引用 | `lifecycle.go` 直接写 `StatusRunning.String()`（**同一个词、同一份词表**） | 实现包不再认识装配侧的常量名 |
| `backend/engine/assemble/workunit_parent.go` 在两条 e2e 门禁清单里的路径项 | `backend/workunit/lifecycle.go` | 见 §13.3 |

**留在装配处的四类**（有意，不是漏搬）：`hostPorts`（三格端口的唯一实现，具体类型只允许在这里）、
`newLifecycleHost`（唯一的构造点）、两个**注册点**（`nodeWorkUnit` / `teamUnit`，含 `Unit` 读数与
层词表）、teammate 的**记录面**与恢复策略。判据是 AGENTS §8.5「层差异只允许出现在装配表与策略的
调用点上」+ 依赖方向：`backend/tool/worktree` 依赖本包的两个哨兵，装配侧反过来 import `worktree`
——两半不能互换位置，否则成环。

### 13.2 依赖方向与"为什么两半同包"

```text
backend/tool/worktree ─┐
backend/multiagent/subagent ─┼─→ backend/workunit（契约 + 唯一实现 + 三格端口）
internal/telemetry ─┤                   ↑ 注入
backend/tool/{node,registry} ─┘    backend/engine/assemble（hostPorts 适配 + 注册点 + 记录面）
seelebridge/teamwork ─┘
```

`backend/workunit` 的依赖只有 stdlib + `store` + `contract/dto` + `Seele/jobs` ——**不含任何
Seelex 实现包**（`lifecycle.go` 只 import stdlib，`ports.go` 只 import `context`）。这是"实现不认识
具体宿主"的可机械断言形态，也是 `e2e/workunit_ports_test.go` 的 ①② 两条判据扫的对象。

### 13.3 门禁改造（本片唯一"非机械"的改动）

原门禁把三个落点写成**仓库路径常量**（`backend/engine/assemble/workunit_parent.go` 等），
每次搬迁都要改门禁——域还要继续下沉（J2 续做 / M5），这成了一段"赶不上搬迁的门禁"。本片改成
**按包名 + 标记定位**：

1. `packageDir`：读仓库根 `go.mod` 的 `module` 行，把 import path 翻成目录（第三方模块直接判红）；
2. `locateMarkerFiles` / `locatePackageFile`：在包目录里按**标记**认生产文件（
   `var _ Lifecycle = (*`、`var _ Jobs = (jobs.Manager)(nil)`、`(*hostPorts)(nil)`），
   命中**恰好一个**才算过（零个 = 断言没了；多个 = 标记不再唯一）；
3. 新增 `TestWorkunitGateLocatorCatchesMissingMarkers`：对照喂**临时假目录**与假 import path
   （标记不存在 / 命中两处 / `_test.go` 不算生产文件 / 目录不存在 / 第三方模块），
   验的是"定位判据本身"而不是"我肉手改坏跑了一次"。

同一片里跟随的两处**清单式**门禁（它们是"谁在这格里干什么"的登记，不是定位器）：
`e2e/subagent_status_vocabulary_gate_test.go` 的「记录状态」清单项、以及由它派生的
`e2e/state_enum_cast_gate_test.go` 文件集（后者自动跟随前者，无需改）。

### 13.4 证据（实跑）

| 检查 | 结果 |
|---|---|
| `gofmt -l .`（排除 `tmp/`） | 空 |
| `go build ./...` / `go build -tags "gui,desktop,production" ./...` | 均通过 |
| `go vet ./...` | 无输出 |
| `go test ./backend/workunit/...` | ok（1.9s，含 3 个基准编译） |
| `go test ./backend/engine/assemble/` | ok（44.4s） |
| `go test ./backend/tool/... ./backend/multiagent/... ./internal/telemetry/... ./seelebridge/...` | 全 ok（`backend/tool/registry` 的 `TestJobHandlesCarryTheirTableToken` 触发一次 D-M4-11 的 Windows `TempDir` 环境性失败，**单跑该用例即 ok**，与搬迁无关） |
| `go test ./e2e/` | ok（含门面闭包 ratchet、模块 README 门禁、两条新老门禁与阴性对照） |
| `go test ./... -count=1 -timeout=900s` | **失败集合 = 基线 3 条**（`application/core` 的 `TestContextBudgetSkipsCompactionWithoutMargin`、`TestRunningSessionAccumulatesAllRoundsInOrderControl`、`TestColdRestoreTailFirstRequestKeepsTranscriptOrder`），与改动前逐条相同 |
| `node --test gui/frontend/dist/*.test.mjs` | 778/778 |
| `python scripts/check_readme_refs.py` | 12 条（**新增 0**） |
| 改动形状（`git diff --cached -M --numstat`） | 8 个文件 **0 行 diff** 的纯改名；21 个文件各 1 行（import 路径）；`lifecycle.go` 56/55 全为"同包化"改写（无一行逻辑变化）；`ports.go` 新增 69 行 |

### 13.5 本片新偏差

| ID | 偏差 | 处置 |
|---|---|---|
| D-M4-W1 | **落点偏差（已裁决后落地）**：plan §2.7 写 `backend/multiagent/workunit/`，实际落 `backend/workunit/`。理由：进口方横跨 tool / multiagent / internal 三块，且 `backend/tool/worktree` 反向依赖本包的两个哨兵（`ErrUncommittedChanges` / `ErrMergeBlockedByMain`）——放 multiagent 会让 tool 板出现跨板回指（M4 出口条件之一正是"无跨板块回指"）。任务书给出的裁决即"独立 `backend/workunit/`" | 已按此落地；plan 的 §2.7 那一行与 §3.6 的 V3 因此按本记录收口 |
| D-M4-W2 | 生命周期实现下沉后，三格端口**必须导出**（装配处已在另一个包），于是新增 `SceneFace` / `UnitFace` / `RecordFace` / `NewHost` / `ErrHostUnavailable` 五个名字。它们替代的是原来**同包私有**的 `sceneFace` / `unitFace` / `recordFace` / `newLifecycleHost` / `errLifecycleHostUnavailable` | 记偏差而不留私有别名：同一件事不许有第二个名字（AGENTS §8.3）。不动任何行为、协议与持久化格式 |
| D-M4-W3 | 端口门禁从"写死路径"改成"按包名 + 标记定位"后，**门禁的失效形态变了**（路径写错 → 标记找不到）。若没有对照，这种门禁可能"静默少扫一个文件却全绿" | 已补定位器阴性对照（§13.3 第 3 条，5 个对照用例） |
| D-M4-W4 | `teamUnitStatusFor`（收尾分类 → 记录终态词）**故意留在**装配包 `workunit_team.go`：它是"这一层自己的词表"，属 AGENTS §8.5 的"层差异只允许出现在装配表与策略的调用点"。若后人想把它搬进契约包，先看这条 | 记录性偏差（不是待办） |
| D-M4-W5 | `hostPorts` / 注册点 / teammate 记录面 / 恢复策略仍在 `backend/engine/assemble`：它们本质是**宿主适配**（要 `Runtime` 的编排、现场、账本读写）。彻底离开装配包要等 node/subagent/teamwork 三个板块各自下沉（J2 续做） | 登记为 J2 续做项；`backend/engine/assemble/README.md` 的 workunit 一节已写明"本包只剩注册/装配五文件" |
| D-M4-W6 | 搬迁后 `lifecycle.go` 的日志前缀仍是 `seelebridge: `（`log.Printf("seelebridge: 判定 %q 是否已收尾失败…")`）。改它属"行为可观察面变化"，与 plan §5.1 的"行为零变化"冲突 | 保留，与既有搬迁同款（`backend/engine/plugin` / `backend/tool/plan` / `store/legacy_storage.go` 都是这个状态）；要不要统一前缀应作为**一次独立的行为变更**处理 |

### 13.6 本片结束时的实况

`seelebridge/` 只剩 **`teamwork` 一个包**（6 生产文件 + 15 个用例，≈2,580 行）与 `.seelex/`
（mcp trace 运行数据，**不是包**；保留/迁移/删除待用户裁决，本轮不擅自动）。根包 53 个文件早随 J1
落位 `backend/engine/assemble`，其中 J2a/J2b/J2c（obs / media / workunit 域）已下沉。
下一片 = `teamwork` 片（`seelebridge/teamwork` + 根包 `runtime_teamwork*.go` 13 文件 ≈3,700 行 +
`teamwork_job_signals.go` + `runtime_teammate_session_live.go` → `backend/multiagent/teamwork/`），
做完 `seelebridge/` 目录即消失（M4 出口条件的第一个标志）。

## 14. M4-TW1：`teamwork` 整包归位（`seelebridge/` 目录清空）

plan §2.7 给 `teamwork` 点名的落点就是 `backend/multiagent/teamwork/`，本片照此**整包 `git mv`**。
它不涉及 `Runtime` 拆分（那是 J2 续做），因此是纯路径搬迁——判据只有一条：**内容零 diff**。

### 14.1 删除清单（原位置 → 新唯一位置）

| 原位置 | 新唯一位置 | 说明 |
|---|---|---|
| `seelebridge/teamwork/`（6 生产文件：`coordinator.go` 555、`items.go` 1003、`items_gate.go` 145、`teamwork.go` 278、`executor.go` 62、`planlock.go` 42、`store.go` 45 行）+ 16 个用例 + README | `backend/multiagent/teamwork/` 同名文件 | `git mv`，**包名 `teamwork` 不变**；`--numstat` 显示 22 个文件 `0 0`（纯改名） |
| 31 个 `.go` 的 `import "…/seelebridge/teamwork"` 与注释里的落点 | 同上，改为 `…/backend/multiagent/teamwork` | 全是 1–2 行改写：assemble（22）、main.go、`application/core`、`store`、`contract/dto`、根包用例、`e2e` 两条门禁 |
| 两处"带文件后缀"的注释引用（`backend/tool/worktree/worktree_vanished_scene_repro_test.go`、`store/teamwork_items.go`） | 同上 | 词边界脚本看不见这种形态，手工补（D-M4-TW1） |
| 门禁里的历史路径：门面闭包 allow list 条目、workunit 端口门禁的 `forbiddenPackages` 键与阴性对照样例 | 改为新路径 | allow list **条数与上限不变**（改名不是新模块）；`e2e/layout_test.go` 的 `repositoryModules` +1 |

### 14.2 证据（实跑）

| 检查 | 结果 |
|---|---|
| `gofmt -l .`（排除 `tmp/`） | 空（搬迁后 23 个文件需要 import 重排，已 `gofmt -w`） |
| `go build ./...` / `-tags "gui,desktop,production"` | 均通过 |
| `go vet ./...` | 无输出 |
| `go test ./backend/multiagent/...` | ok（`teamwork` 4.5s + `subagent` 7.1s） |
| `go test ./backend/engine/...` | 全 ok（`assemble` 56.9s，含 teamwork 域的全部接线用例） |
| `go test ./store/... ./internal/adapters/... ./backend/workunit/...` | 全 ok（`store` 91.5s） |
| `go test .`（根包，含 `teamwork_headless_smoke_test.go` / `tool_full_chain_test.go`） | ok（189.8s） |
| `go test ./e2e/` | ok |
| `go test ./... -count=1 -timeout=900s` | **失败集合 = 基线 3 条**（`application/core`） |
| `node --test gui/frontend/dist/*.test.mjs` | 778/778 |
| `python scripts/check_readme_refs.py` | 12 条（**新增 0**） |
| 目录结构 | `seelebridge/` 下 **`.go` 计数 = 0**（只剩 `.seelex/` 运行数据与不被 git 跟踪的空 `workunit/` 目录） |

### 14.3 本片新偏差

| ID | 偏差 | 处置 |
|---|---|---|
| D-M4-TW1 | **词边界脚本的第三类盲点**：`seelebridge/teamwork/items.go` 这种"路径 + 文件后缀"的形态不被 `(?<![\w/])…(?![\w/])` 命中（前瞻把 `/` 排除掉了）。前两类盲点是相对链接深度（D-M4-10）与散文里的裸包名 | 手工补两处，并把"收尾必查：`rg <旧路径>`（不加词边界）"写进本片流程；以后搬迁脚本按"先宽后窄"两遍扫 |
| D-M4-TW2 | `backend/multiagent/teamwork` 仍 import `application/core/agentteam`（**既有方向**，非本片引入）。plan §2.7 要求 `agentteam/` → `backend/multiagent/team` | 登记为后续片；本片不动它（动了就是行为变更，不属于"物理归位"） |
| D-M4-TW3 | 同域**执行体**仍在装配包：`runtime_teamwork*.go`（11 文件）、`teamwork_job_signals.go`、`runtime_teammate_session_live.go`、`runtime_teamwork_board*.go`、`workunit_team*.go` | J2 续做项：它们要 `Runtime` 的会话绑定 / 计划存储 / 角色会话句柄，属宿主适配，按 J2 模板（端口先行 → 状态随方法走 → 装配侧留持有与拓扑）逐域下沉 |
| D-M4-TW4 | 提交时发现工作区里有**并行的无关改动**（`.githooks/*`，由上一级代理在同目录进行中） | 按 AGENTS §7「不混入无关文件」处理：`git restore --staged .githooks` 后只提交本片文件；未触碰、未丢弃对方的工作树改动 |

### 14.4 里程碑位置

**M4 出口条件的第一个标志（`seelebridge` 目录消失）已达成**：`seelebridge/` 下不再有任何 Go 包，
`go list ./...` 里不再出现 `seelebridge`。M4 剩下的部分不再是"搬 seelebridge"，而是
**J2 续做**（把 `assemble` 里的各域按端口下沉到 engine/tool/multiagent）与 **K 片**
（删 `ports.go` 的 1020 行逐行委托 + 11 条兼容别名）。两者都不阻塞 M5 的前置条件
（M5 = 中间层切割 + `Service` 门面收窄 + `main.go` < 400 行），因此下一片可二选一：

1. **K 片**：删 `ports.go`，让契约层闭包变干净（现在它为了两个别名反向依赖 assemble）；
2. **M5 起步**：`application/core` 按域切分（先 `kernel/state`）+ `main.go` 的注册段迁 `wiring/registry/`。
