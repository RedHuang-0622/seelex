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
