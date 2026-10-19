# 三个方向缺陷修复（2026-10-19）

> 一次性工作包。三件事互相独立，**分别属于三个方向**：前端工作区（多仓库 git）、前端渲染
> （HTML 沙箱缓冲）、后端工具记录（阶梯提交与会话隔离）。
>
> 本文件是**方案与验收口径**，不是"已实现"的事实来源。实现是否落地，以代码与测试为准。
> 落地后仍需按 `AGENTS.md` §2/§3 更新对应模块 README。

## 0. 结论速览

| 编号 | 方向 | 病灶 | 改动面 | 风险 |
|---|---|---|---|---|
| A | 多仓库 git | git 查询只认"当前 workspace 根"这一处仓库，根下多个独立仓库看不见、切不到 | contract/dto、store/workspace、application/core、gui/bridge、gui/frontend | 低（只读；新增一条只读扫描） |
| B | HTML 沙箱 | 消息行 HTML 一变就**整行重建**，`<iframe srcdoc>` 被销毁重建，新帧加载完成前是空白 → 闪屏 | gui/frontend（html-embed / reconcile / 新增缓冲模块） | 中低（渲染时序，需要可测的纯决策函数） |
| C | 工具记录 | ① 子代理那条计入的是**当前视图会话**（切会话即串写）；② 叶子单元没有自己的暂存，完成即写主侧；③ 落盘点在一回合末尾，草稿/未物化也照写 | application/core、middle/view/subagent、backend/engine/assemble、store/record、测试 | **高**（跨会话数据归属），必须红灯先行 |

三件事的交付顺序：**A 与 C 并行（文件面不重叠）→ 合并后再做 B**（A/B 都会碰
`gui/frontend/dist/{app.js,index.html,styles.css}`，串起来做避免 worktree 合并冲突）。

## 1. 现状证据（读代码确认，未运行验证）

标注「已确认」= 本文件写作时直接读到该行；标注「待复现」= 有代码线索但需要红灯用例坐实。

| # | 结论 | 证据 | 状态 |
|---|---|---|---|
| 1 | git 只读用例没有任何"选仓库"参数，一律用当前 workspace 根 | `application/core/workspace_usecase.go:94` `WorkspaceGitLog(limit int)`、`:99` `WorkspaceChanges(limit int)`、`:104` `WorkspaceGitCommitDetail(hash, limit)`；均转 `components.workspace.*` | 已确认 |
| 2 | 桥也不透传仓库 | `gui/bridge.go:1114` `WorkspaceGitLog(limit int)` 等四条同形 | 已确认 |
| 3 | 前端只打一发"当前仓库"的查询 | `gui/frontend/dist/app.js:1201` `invoke("WorkspaceGitLog", 20)`；面板容器 `code-pane-gitlog` / `code-pane-changes` | 已确认 |
| 4 | 仓库里**没有**仓库发现（BFS/`.git` 扫描）实现 | 全仓 `"\.git"` 只出现在忽略表、worktree 管理与测试白名单（`store/workspace/tree.go:32` 等） | 已确认 |
| 5 | 目录遍历已有唯一忽略表可复用 | `store/workspace/tree.go` `ignoreDirNames`（`.git`/`.seelex`/`dist`/`node_modules`/`.venv`/`tmp`/`.idea`/`__pycache__`） | 已确认 |
| 6 | HTML 沙箱块是 `srcdoc` iframe，每次重新生成整段 HTML | `gui/frontend/dist/html-embed.js` `renderHtmlEmbed` / `renderSandboxFrame` | 已确认 |
| 7 | 会话行 reconcile **在 html 字符串变化时整行替换**（`elementFromHTML` + `node.replaceWith`），行内 iframe 必然重建、重新加载 → 空白帧 | `gui/frontend/dist/conversation-view.js` `reconcile()` | 已确认 |
| 8 | 子代理工具记录写的是**当前视图会话**（`Snapshot.Session.ID`），不是它所属的主会话 | `middle/view/subagent/coordinator.go` `HandleSubagentToolEvent` 里 `c.Snapshot.Session.ID` → `recordToolOutcome` | 已确认（串写风险坐实） |
| 9 | 子代理事件无 `NodeID` 时**整条丢弃**（先早退再记账），fork/非 Plan 子代理的调用不会进台账 | 同文件开头 `if NodeID=="" || ID=="" { return }` | 已确认 |
| 10 | teammate 那条用 `RoleToolActivity.MainSessionID`（正确），空则丢 | `application/core/service.go` `HandleRoleToolActivity` | 已确认 |
| 11 | 工具记录只在内存累加；落盘点三处：回合末尾、草稿物化、停服全量 | `application/core/chat.go:436`、`session_draft.go:301`、`service_input.go:363` | 已确认 |
| 12 | 子代理/teammate 的工具活动回调**已装配**（不是没接线） | `wiring/bootstrap/bootstrap.go:408` `SubagentToolCallback`/`RoleToolCallback`；`backend/engine/assemble/runtime_deps.go:66` | 已确认 |
| 13 | 子代理工具记录跨会话串写会导致"切到 B 后 B 的看板出现 A 的计数" | 由 #8 推导，需红灯用例复现 | 待复现 |

结论：#12 说明"钩子没伸过去"不是装配缺失，而是**归属键错**（#8/#9）与**没有阶梯提交**——
叶子单元的读数直接写进"当前视图会话"的累计，视图一动就串。

## 2. 方向 A：多仓库 git（BFS 发现 + 子页标签）

### A.1 现象

工作区根下若干目录各自是 git 仓库（或根本身不是仓库、子目录才是），面板只看得到一个仓库
（= 当前 workspace 根视角），其余仓库的提交/改动根本进不去。资源管理器能翻到目录，但 git
面没有"切到那个仓库"的入口。

### A.2 设计

**契约（`contract/dto/gitrepos.go` 新增）**

```go
type GitRepoRef struct {
    Rel     string `json:"rel"`               // 相对工作区根，"/" 分隔；根视角 = "."
    Name    string `json:"name"`              // 展示名：basename；".." 视角用工作区名
    Primary bool   `json:"primary"`           // 默认展示的那一个（前端初始选中）
    Branch  string `json:"branch,omitempty"`  // 当前分支（读不到留空，不阻塞列表）
}

type GitReposResult struct {
    Root      string       `json:"root"`
    Repos     []GitRepoRef `json:"repos"`
    Truncated bool         `json:"truncated"` // 达到深度/条目预算后截断（≠ 没有更多）
    Scanned   int          `json:"scanned"`   // 进过的目录数（"扫了多深"要能说清）
    Error     string       `json:"error,omitempty"` // 非致命展示文案（未绑工作区等）
}
```

**端口（可选口，别动既有接口）**

新增 `contract.WorkspaceGitReposPort`（`GitRepos(root string, limit int) (dto.GitReposResult, error)`），
由 `store/workspace` 的 Repo 实现，Application 用类型断言启用（与 `WorkspaceTreePort` 同款
"有就有、没有就显示不可用"）。**不要往 `WorkspaceTreePort` 里加方法**——加方法会打断所有
既有实现与测试假体。

**扫描器（`store/workspace/gitrepos.go`）**

BFS，逐层入队；单条规则表：

| 规则 | 取值 | 理由 |
|---|---|---|
| 起点 | 工作区根 | 只扫绑定工作区内部 |
| 仓库判定 | 目录内存在 `.git`（目录**或文件**） | `.git` 文件 = worktree/子模块，`git -C` 一样可用 |
| 是否下钻仓库内部 | **下钻**（跳过 `.git` 本身） | 仓库里再套独立仓库很常见；不下钻就漏 |
| 剪枝 | 复用 `ignoreDirNames` + 隐藏目录（`.` 开头） | 与目录树同一张表，不写第二份 |
| 深度上限 | 6 | 深过 6 层的仓库不是"根目录下的仓库" |
| 目录预算 | 20000 | 防大仓库把一次面板刷新拖成 IO 风暴 |
| 仓库上限 | `limit`（默认 64，钳到 1..256） | 前端标签栏也放不下更多 |
| 顺序 | 先 `Primary`，其余按 `Rel` 字典序 | 读数确定、标签位置稳定 |
| 错误 | 读不了的目录跳过并继续 | 权限问题不该让整次扫描失败 |

**主仓库（Primary）判定**（两条，按序）：

1. 根目录含 `.git` → `Rel="."`、`Primary=true`；
2. 否则根所在的**祖先仓库**（`git -C root rev-parse --show-toplevel` 成功且 ≠ 根）→
   也记为 `Rel="."`、`Primary=true`（沿用现有"以工作区根为视角"的语义，路径仍以根为基准）；
3. 都没有 → 无 Primary；前端选 `Repos[0]`（最浅 → 字典序）。

**应用用例（`application/core/workspace_usecase.go`）**

- 新增 `WorkspaceGitRepos() (dto.GitReposResult, error)`：工作区根来自后端当前 workspace，
  未绑定/端口缺席 → 返回空 `Repos` + `Error` 文案（不报错）。
- 既有四条 git 查询**改签名**加 `repo string`：
  `WorkspaceGitLog(repo string, limit int)`、`WorkspaceChanges(repo, limit)`、
  `WorkspaceGitCommitDetail(repo, hash, limit)`、`WorkspaceGitCommitFileContent(repo, hash, relPath, limit)`，
  内部把 `repo` 相对路径解析成**绝对仓库根**再交给既有 `workspace.GitLog(root, ...)` 等
  （端口签名不变，语义不变）。
- `repo` 解析必须走同一条 containment 判据：空/`"."` → 工作区根；绝对路径、`..` 逃逸、
  软链逃逸一律 `error`（**不是** `Result.Error`：这是调用方传错参数）。

**桥（`gui/bridge.go`）**：新增 `WorkspaceGitRepos()`；四条查询方法按上面新签名转发。
`gui/frontend/dist/app.js` 的 `invoke("WorkspaceGitLog", repo, 20)` 同步改。

**前端（`gui/frontend/dist/`）**

- 新增 `git-repos.js`（纯函数 + 渲染）：`buildRepoTabs(repos, selectedRel)` 产出标签模型
  （`primary` 在前，其余作为**子页标签**）；`resolveSelectedRepo(repos, selectedRel)` 处理
  "选中的仓库消失了"（回落 Primary / 第一项）。
- 面板头部加一条仓库标签栏：**只显示一个仓库的 git**（选中的那个），其余仓库藏在子标签里
  等用户翻。只有一个仓库时不显示标签栏（保持现观感）。
- 选择状态按工作区 ID 记忆（`gitRepoSelectionByWorkspace`），工作区切换即重置；快照刷新
  不重置；仓库列表变化时用 `resolveSelectedRepo` 收敛。
- 两个子面板（提交列表 / 更改）共用同一个选中仓库；标签栏放在 `code-panes` 头部，
  计数（`git-log-count` / `changes-count`）随仓库刷新。

### A.3 边界与安全

- 只读：不新增任何写 git 的能力（不改 index、不 commit）。
- 路径：所有相对路径先进 containment 判据，再落 `filepath.Join`；不下发绝对路径给前端。
- 预算：深度/目录数/仓库数三重上限 + `Truncated` 如实上报（"还有更多没扫完"必须说出来）。
- 隐私：忽略表与敏感文件名判据沿用 `store/workspace` 既有那份，不复制。

### A.4 验收

后端单测（`store/workspace/gitrepos_test.go`，全部临时目录现造）：

1. 根是仓库 → 1 条 `Rel="."`、`Primary`；
2. 根不是仓库、`a/`、`b/` 各是仓库 → 2 条、字典序、无 Primary 时第一条被前端选中；
3. 仓库里套仓库 → 内层也出现（不因外层是仓库就停）；
4. `.git` 是**文件**（worktree/子模块形态）→ 识别；
5. `node_modules`/`.venv`/`dist`/`tmp` 下的假仓库 → 不出现；
6. 深度超限 → 出现 `Truncated=true`；
7. `limit` 钳制与截断；
8. 空/未绑定根 → `Error` 文案、`Repos` 空、不 panic。

应用层单测：`repo` 解析（`.`/相对/绝对/`..` 逃逸/软链）与四条查询转发到正确绝对根。

前端单测（`gui/frontend/dist/git-repos.test.mjs`）：标签模型、单选、选中项消失回落、
单仓库不出标签栏。

模块 README：`store/workspace/README.md`、`application/core/README-*.md`（工作区段）、
`gui/README.md`（桥方法表）同步。

## 3. 方向 B：HTML 沙箱渲染缓冲（单缓冲 → 双缓冲）

### B.1 现象与根因

`seelex-html` 围栏渲染成 `<iframe sandbox="allow-scripts" srcdoc=...>`。会话行在
`conversation-view.js` 的 `reconcile()` 里，只要该行 html 字符串变了就

```js
const replacement = elementFromHTML(item.html);
node.replaceWith(replacement);          // 整行换掉 → 行内 iframe 全部重建
```

新 iframe 的 `srcdoc` 要重新解析、布局、加载，加载完成前是空白（宿主背景透出 = 白/深底闪一下）。
流式输出期间 html 每帧都变，于是每帧重建一次帧 → 连续闪屏。

结论：**不是"没做缓冲"这一件事**，而是"帧的实例身份每帧都变"。修法必须同时解决
**身份复用**与**挂载时序**两件事。

### B.2 设计（两层，先单后双）

**① 稳定帧身份（必要，解决"为什么每帧都重建"）**

- `renderHtmlEmbed` 给每个 `<iframe>` 打 `data-embed-key`：`<行 key>#<块序号>`（`markdown.js`
  按一条消息内 `seelex-html` 围栏的出现序号给）。没有稳定 key 时退回内容哈希（稳定但不复用，
  只保证不抖动）。
- `reconcile` 换行前先"领养"旧行里的帧：`Map<embedKey, iframe>`。新行 HTML 解析出来后，
  凡是 key 与 `srcdoc` 都相同的帧**原样搬过去**（`adoptNode`/`replaceWith` 前后），不重新加载。

**② 缓冲挂载（单缓冲）**

新增 `gui/frontend/dist/embed-buffering.js`：

- 纯决策（可在 node:test 无 DOM 环境测）：
  `planEmbedMounts(prev, next, opts) -> { adopt:[], mount:[], defer:[], unmount:[] }`
  判据：key 命中且 `srcdoc` 相同 → adopt；key 命中但内容变 → mount（旧帧保留到新帧 load）；
  内容还在流式变化（`opts.streaming` 或 `now - lastChangeAt < settleMs`）→ defer；消失 → unmount。
- DOM 适配：`createEmbedFrameBuffer({ mountRoot, settleMs = 150, maxPending = 4 })`
  - `mount`：先在**离屏容器**建 iframe、赋 `srcdoc`、等 `load`（+ `requestAnimationFrame` 一帧）
    再交换进可视位置；交换前旧帧仍在原位（占位高度来自 `--embed-height`，不塌陷）。
  - `defer`：先放一个同高占位（`data-embed-pending="1"`），内容稳定后一次性挂载。
    **流式期间不重建任何帧**。
  - 版本令牌：每次挂载带自增 token，`load` 回来时 token 过期就丢弃并卸载该帧（防抖/防串）。
  - 卸载：行被移除 / key 消失时，清掉 pending 帧与监听（不泄漏 iframe 与 `load` 回调）。

**③ 双缓冲（②不够时的兜底，不预设一定需要）**

同一 `figure` 内保留两层：`figure.html-embed` 下放 `.html-embed-frame-wrap`（`position:relative`），
新旧帧绝对定位重叠；新帧 `load` 完成后再移除旧帧（一次 `opacity` 过渡）。开关：
`createEmbedFrameBuffer({ doubleBuffer: true })`，默认 `false`；实测单缓冲仍有闪烁时改默认。

**判定"是否奏效"的办法**（无浏览器 CI）：

- 单测层面：断言"内容不变的帧不产生新的 iframe 元素"、"内容变化的帧旧元素在新帧 `load`
  之前仍在树上"、"流式期间挂载次数 = 0"。
- 手工验证：在会话里发一条含两个 `seelex-html` 块的消息，观察流式期间帧区域是否有白闪；
  记录进 `docs/devlog/`（一条即可，含日期与版本）。

### B.3 验收

- `node --test gui/frontend/dist/*.test.mjs` 全绿，新增 `embed-buffering.test.mjs`：
  `planEmbedMounts` 六种输入（新增/内容变/流式中/稳定/消失/顺序变化）；
  假 DOM 下 adopt 不重建、mount 等 load、defer 不挂载、卸载清理。
- `html-embed.test.mjs` 补 `data-embed-key` 与 `--embed-height` 占位断言。
- 不改沙箱口径：`sandbox="allow-scripts"`、CSP、不给 `allow-same-origin` —— 一条都不许动。
- `gui/README.md` / `gui/frontend/README.md` 同步渲染时序说明。

## 4. 方向 C：工具记录阶梯提交（subagent/teammate → main → 存储）

### 4.1 病灶（已确认 + 待复现）

1. **归属键错**：子代理那条按"当前视图会话"记账（`coordinator.go`），切会话即串写（#8）。
2. **叶子没有自己的暂存**：工具一完成就写进会话累计，主侧被高频写；回调链上没有"这一级
   先把数据管好、确定了再上交"的台阶。
3. **早退吞事件**：无 `NodeID` 的子代理事件整条丢弃（#9）。
4. **落盘时机过松**：回合末尾就落盘（`chat.go:436`），草稿还没物化也在写。
5. 跨会话串写（#13）—— 由 1 推导，需要红灯用例复现，红在哪修在哪。

### 4.2 目标形态（用户口径原文）

> 先自我维持数据，确定之后提交变更到上一级。链路：`subagent/teammate -> mainagent -> 工具记录存储`。
> draft 落实了才做工具记录的存储；做完了才返回，而不是让工具记录写繁忙。

落成三条纪律：

- **归属键显式**：任何一笔读数都必须带 `(ownerSessionID, unitID)`，`ownerSessionID` 只从
  **执行上下文**取；`view session` 永不作兜底（取不到就丢弃并计数）。
- **阶梯提交**：叶子自己攒（本地暂存，不碰主侧锁、不碰磁盘）；叶子自己的回合收口（"draft
  落实"）时**一次性、同步**提交给主会话累计并清空暂存；主会话只在**真相源提交点**落盘。
- **不写繁忙**：提交是同步的（返回即已入账，不是丢给 goroutine）；工具热路径只做本地累加。

### 4.3 设计

**4.3.1 `store/record` 增加"叶子暂存"**

```go
// Stage 是一个执行单元（子代理节点 / teammate 角色会话）的本地暂存。
type Stage struct {
    unitID    string   // "subagent:<nodeID>" / "role:<roleSessionID>"
    sessionID string   // owner 主会话（只由装配处注入，不从视图读）
    acc       *Accumulator // 复用既有累加器（不写第二份判据/词表）
    seq       uint64   // 每次 Commit 自增；跨重启的幂等靠 seq
}
func (s *Stage) Record(tool string, ok bool, reason string)   // 纯本地
func (s *Stage) TakeForCommit() (seq uint64, stats []dto.ToolRecordStat) // 同步交出 + 清空
```

暂存有界：单单元工具名条目上限（`limits` 里取一个上限或常量 256），超出把新工具名并入
`other` 之外**不丢计数**（只丢"名字的细分"），并在看板 `LedgerError` 之外不新增字段——
这条只写注释与测试，不扩展 DTO。

**4.3.2 主侧合并（`application/core/tool_record.go`）**

```go
// CommitUnitToolRecord 是叶子→主 的唯一入口：同步、幂等、只碰累加器锁。
func (service *Service) CommitUnitToolRecord(sessionID, unitID string, seq uint64, stats []dto.ToolRecordStat)
```

- `sessionID` 空 → 丢弃（**不回落视图会话**，与 jobs 事件同口径）；
- `(sessionID, unitID)` 记 `lastSeq`，`seq <= lastSeq` → 幂等忽略；
- 合并进该会话的 `toolRecords.accumulator(sessionID)`；
- 关掉计数开关 → 直接返回（叶子也不该攒：开关在叶子的 Record 入口就判）。

**4.3.3 两条叶子链落点**

- **子代理**（`middle/view/subagent/coordinator.go` + `backend/engine/assemble/runtime_subagent*`）：
  - 事件里必须带它所属主会话（沿用/补齐 Runtime 已有的 main session 归属，不要在 coordinator
    读 `Snapshot.Session.ID`）；
  - `HandleSubagentToolEvent` 先把读数写进本节点 `Stage`（无 NodeID 时用 `subagent:<id>` 兜底，
    **不再整条丢**），Plan 投影的早退分支挪到记账之后（现状已是顺序，但归属键要改）；
  - 节点进入终态（completed/failed/aborted）时提交一次。
- **teammate**（`application/core/service.go` `HandleRoleToolActivity`）：
  - 读数进 `role:<roleSessionID>` 暂存，键 = `event.MainSessionID`；
  - 角色回合收口（该角色这一轮结束）时提交；`MainSessionID` 空 → 丢弃 + 一个可观测计数
    （诊断日志，不进快照）。

**4.3.4 落盘门（`application/core`）**

- `FlushToolRecord(sessionID)` 增加一道门：**会话必须是已物化的真实会话**（草稿不落盘）。
  判据复用会话域已有的"草稿 vs 真实会话"判定，不新造一格。
- 落盘前先"抽干本会话所有叶子暂存"（`drainStagesFor(sessionID)`），保证提交点不丢数据；
  叶子未收口就落盘的情况（进程要退、会话要释放）由这一步兜住。
- 保留：草稿物化提交点（`session_draft.go:301`）、停服全量（`service_input.go:363`）、
  会话释放路径。
- `chat.go:436` 的回合末尾落盘：**保留，但过门**——真实会话仍一回合一次（崩溃只丢一回合），
  草稿会话一次都不写。这条同时满足"draft 落实了才存储"与"不写繁忙"。

**4.3.5 回收**

- 会话释放/删除：连带丢弃该会话的全部叶子暂存与 `lastSeq`（避免"删了会话下次同名 ID 复用
  到旧 seq"）。
- 视图切换：**什么都不做**（读数不跟视图走）。

### 4.4 会话隔离边界测试矩阵（必须全绿，红一条都不算完）

| # | 场景 | 期望 |
|---|---|---|
| 1 | 会话 A 的子代理跑着，视图切到 B，A 的工具完成后回写 | A 的累计 +1；B 一分不动 |
| 2 | A、B 各自的子代理并发完成 | 各归各位，无交叉 |
| 3 | teammate `MainSessionID` 为空 | 丢弃，不落到任何会话（含视图会话） |
| 4 | 同一单元重复提交（同 `seq`） | 只计一次 |
| 5 | 同一单元 `seq` 递增两次提交 | 两次都计入（增量语义），不重复历史 |
| 6 | 草稿会话连跑两回合 | `tool-record.jsonl` 写入次数 = 0 |
| 7 | 草稿物化后 | 恰好 1 次写入，且内容含子代理/teammate 的读数 |
| 8 | 离开卖点前未提交的叶子暂存 | 落盘前被抽干，计数不丢 |
| 9 | A→B→A 来回切 | A 的看板只显示 A 自己；`LedgerError` 不串 |
| 10 | 删除会话 A | A 的暂存/lastSeq 回收；B 不受影响 |
| 11 | 计数开关关闭 | 叶子不攒、主不记、不写盘 |
| 12 | 同一会话内 主代理 + 子代理 + teammate 交替 | 三类计数都进同一格，失败原因只归一一次 |

测试实现要点：

- 用**同步假体**（现有 `service_fakes_test.go` 那套），不用 sleep；Windows 本地
  `CGO_ENABLED=0` 不跑 `-race`，所以断言要确定性（用 channel/Once 收集，不靠时序）。
- 落盘次数用计数 fake 的调用数断言，不用读真实文件。
- 串写用例必须**真的切视图**（`switchView`/`switchToSession` 既有用例手法），不是直接改字段。

### 4.5 验收

```
go build ./...
go vet ./...
go test ./application/core/ -run 'ToolRecord' -count=1
go test ./store/record/ ./middle/view/subagent/ ./backend/engine/assemble/ -count=1
```

外加：`application/core/README-tool.md`、`store/record/README.md`、`README-subagent.md`
（子代理段）同步"阶梯提交"口径；`docs/arch/` 不新增（这是一次缺陷修复，不是新架构）。

## 5. 交付编排

三个方向 = 三个 teammate（一角色一成员，一工作项一 worktree）：

| 里程碑 | 工作项 | 负责人（角色） | 文件地块 |
|---|---|---|---|
| M1 | `tool-record-stage`（C） | `recorder` | `store/record/`、`application/core/`、`middle/view/subagent/`、`backend/engine/assemble/` |
| M1 | `git-multi-repo`（A） | `reposcout` | `contract/dto/`、`store/workspace/`、`application/core/`（工作区用例）、`gui/bridge.go`、`gui/frontend/dist/` |
| M2 | `embed-buffer`（B） | `renderer` | `gui/frontend/dist/`（html-embed / conversation-view / 新增 embed-buffering / markdown / app.js / styles.css） |

- M1 内两项并行（改的面不重叠）；M2 在 M1 全部验收后开始（A/B 抢同一批前端文件，
  串行做避免 worktree 合并冲突）。
- 每份派活都带：`$desktop-discipline` 红线（不动用户窗口、不合成输入）、不 commit/不 push、
  只改自己地块、给出"改了哪些文件 + 跑了什么命令 + 结果"。

## 6. 不做什么

- 不重构 git 只读通道的 argv/超时口径（`store/workspace/gitread.go` 是唯一实现，不动）。
- 不改 HTML 沙箱的安全口径（`sandbox`/CSP/不给 `allow-same-origin`）。
- 不给工具记录新增 DTO 字段做"跨会话汇总"（用户没要；汇总另开）。
- 不改 Plan/teamwork 语义；本次只碰读数链路。
- 不为本次改动新增 `docs/arch/` 决策文档（一次性工作包）。

## 7. 风险

| 风险 | 触发 | 处理 |
|---|---|---|
| A 的 BFS 在大仓库上变慢 | 目录预算估小 | 三重预算 + `Truncated`，实测后再调常量 |
| A 与 B 抢前端文件 | 并行 M1 | 已用里程碑隔开 |
| B 的帧复用引入"交互状态丢失"（用户点开的 `details`、拖拽位置） | 换行重建 | 复用帧元素本身，不重建 → 状态天然保留；测试覆盖 |
| C 的落盘门写错 → 读数只在内存 | 草稿判定用错 | 用例 #6/#7 先红后绿；`LedgerError` 语义不变 |
| C 的同步提交阻塞回合 | 提交里做了 IO | 提交只碰内存锁；磁盘只在落盘点 |
