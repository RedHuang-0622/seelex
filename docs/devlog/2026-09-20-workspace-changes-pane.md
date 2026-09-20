# 工作区更改面板：git status 只读元数据跨层落地 + 三处收敛（2026-09-20）

> 日期: 2026-09-20 | 范围: `workspace/gitchanges.go`（新）、`application/contract/dto/gitchanges.go`（新）、
> `application/contract/workspace_tree.go`、`internal/adapters/session_workspace_ports.go`、
> `application/core/workspace_usecase.go`、`gui/bridge.go`、`gui/frontend/dist`（`workspace-changes.js`（新）+
> 第三个 code-pane + 样式）、`docs/gui/modules/right-sidebar.md`、`workspace/README.md`
> 回归: `workspace/gitchanges_test.go`（15 例：解析/分类/基准/过滤/集成）、
> `application/core/workspace_tree_usecase_test.go`（2 例新增）、`gui/bridge_test.go`（1 例新增）、
> `gui/frontend/dist/workspace-changes.test.mjs`（7 例新增）
> 承接: [2026-09-20-frontend-detail-motion-and-explicit-session-submit.md](2026-09-20-frontend-detail-motion-and-explicit-session-submit.md)（资源管理器子页与文件预览抽屉）

## 1. 目标与边界

用户在「资源管理器」子页里能看到工作树与提交记录，但看不到"我改了什么"：
工作树是当前磁盘快照（无状态），提交记录是已入库历史，**未提交的改动在两块面板的
夹缝里**。本轮补第三块面板：工作区更改（暂存/未暂存/未跟踪/冲突）。

边界（与另两块面板一致）：只读元数据——状态字符、路径、重命名原路径、计数。
**不下发 diff、补丁、blob 或任何文件内容**；命令固定 argv、不经过 shell、带超时；
非 git 仓库以 `Result.Error` 返回展示文案（不是 Go error，避免 GUI toast 噪音）。

## 2. 侦察结论（先钉事实，再动手）

- **链路**：`dto → workspace.Repo → WorkspaceTreePort → application.Service → gui/bridge.go`，
  前端 `Bridge.WorkspaceXxx` 一一对应；GUI 侧 `code-panes` 已经有「拖拽调换顺序 +
  localStorage 记忆 + 子页激活按需刷新」的通用骨架，第三块面板只需接进 `CODE_PANES`。
- **git 探针**（本机 git 2.51.0，实测）：`git -C <子目录> status` 会带出仓库里工作区
  **之外**的改动 → 必须带 `-- .` 把范围钉住；`-z` 下重命名是「新路径 NUL 旧路径」两条
  记录；`-z` **不做引号转义**（中文/空格路径原样），默认输出则会给 `\344\270...` 转义
  序列——这是选 `-z` 的直接理由（否则前端还得反解一套 C 风格转义）；`-b` 的分支头在
  `-z` 下同样是首条 NUL 记录；非 git 目录退出码 128 且 stderr 带 `fatal: not a git
  repository`，可直接作为展示文案。

## 3. 实现（三层各自只做自己的事）

- `workspace/gitchanges.go`：`Repo.GitChanges(root, limit)` 执行
  `git --no-optional-locks -C <root> status --porcelain=v1 -b -z -uall -- .`
  （8s 超时；`--no-optional-locks` 只读取状态，不刷新索引、不抢 index 锁），
  `parseGitStatusOutput`（纯函数）解析 NUL 记录，`classifyChange` 把 XY 归一成
  `dto.Change*`（暂存侧优先），`parseBranchHeader` 归一化分支名（空仓库 / 分离头指针）。
- 契约：`dto.WorkspaceChangeEntry`/`WorkspaceChangesResult`；
  `WorkspaceTreePort` 增 `GitChanges`（与 `GitLog` 同级——只读工作区元数据视图都挂这个
  optional 端口）；`WorkspacePort` 转发；`Service.WorkspaceChanges(limit)` 从当前视图
  快照取 root（客户端不能指定路径）；`Bridge.WorkspaceChanges(limit)`。
- 前端：`workspace-changes.js`（`workspaceChangesView` 归一化 + `renderWorkspaceChangesHTML`
  + `createWorkspaceChangesView`，行点击委托打开既有的文件预览抽屉）、`index.html` 第三块
  `code-pane[data-pane="changes"]`、`app.js` 接进 `CODE_PANES` 与
  `refreshWorkTree → refreshChanges`（chat 结束/切换工作区失效，子页激活按需刷新）、
  样式与另两块面板同族。

## 4. 三处必须在后端收敛的差异（不让展示层各自实现）

1. **路径基准**：git status 的路径以**仓库根**为基准，面板以**工作区根**为基准
   （绑定的目录可能是仓库子目录）。实测 `git -C <repo>/deep status -- .` 返回
   `?? deep/nest/leaf.txt`——带着 `deep/` 前缀。`GitChanges` 按
   `rev-parse --show-toplevel` 剥掉前缀，工作区之外的兄弟路径丢弃并计入
   `Result.Filtered`（Windows 大小写不敏感比较）。
2. **可见性边界**：与 `ListTree`/`ReadFile` 一致——路径任一环节命中敏感文件名
   （`accounts.yaml`、`*.local.yaml`）不展示，计入 `Filtered` 并在面板显式提示
   「另有 N 条未展示」，不静默。**目录噪音边界则交给 git 自己的 `.gitignore`**：把
   `ignoreDirNames`（`node_modules`/`dist`/`tmp`…）也套上去会连"被仓库跟踪的 `dist/`
   改动"一起藏掉——那是误报而不是降噪，所以刻意不套。
3. **统计口径**：`Total` 与暂存/未暂存/未跟踪/冲突四个计数覆盖**过滤后的全部条目**
   （含被 limit 截断、未出现在 `Entries` 里的部分）：面板头部说的是工作区状态，不是
   "本屏列了多少行"；列表自身的截断另有 `Truncated` 提示与「条目过多，已截断」文案。

## 5. 验证（红 → 绿）

先写测试再补齐实现，过程中真红过两次（记录在案，不是事后美化）：

```text
# 第一轮（parser 语义没定清 + 相对路径越界返回值不一致）
--- FAIL: TestParseGitStatusOutputSkipsJunkAndTruncatesAtBudget
    unexpected entries: [{path:only-new.txt ...}]        # 悬空重命名吞掉了下一条改动行
--- FAIL: TestRelativeToWorkspace
    relativeToWorkspace("../escape.txt","") = ("../escape.txt",false)   # 越界应返回空串

# 第二轮（全绿）
$ go test ./workspace/... -run 'GitChanges|Classify|BranchHeader|RelativeToWorkspace|WorkspacePathPrefix|SensitiveChange|ParseGitStatus' -count=1
ok  github.com/RedHuang-0622/seelex/workspace        5.184s   # 15 例（含真实 git 仓库集成）
$ node --test --test-reporter=tap gui/frontend/dist/workspace-changes.test.mjs
# tests 7 / pass 7 / fail 0

# 全量
$ go build ./... && go vet ./...            # exit 0
$ go test ./... -count=1                    # exit 0（67 个包 ok，111.0s）
$ node --test gui/frontend/dist/*.test.mjs  # tests 401 / pass 401 / fail 0
$ go test ./e2e/... -count=1                # ok（文档契约/DTO 边界/布局契约）
$ python scripts/check_readme_refs.py --strict   # 123 个 README，0 未解析引用
```

集成用例覆盖的是真 git 仓库（`t.TempDir()` + 本地 `user.*` 配置）：修改/新增暂存/
删除未暂存/`git mv` 重命名/中文名未跟踪/深层目录未跟踪；仓库子目录作为工作区根时
路径被剥成 `nested/leaf.txt` 且工作区之外的改动不出现；被跟踪的 `accounts.yaml`
改动的路径**不出现**且 `Filtered=1`；`limit=2` 时 `Entries` 2 条、`Truncated=true`
而 `Total/Untracked` 仍是全量 3；非 git 目录返回 `Result.Error` 而空/不存在 root
返回 Go error。纯函数用例覆盖 `-z` 原样路径（中文、空格、反斜杠）、悬空重命名丢弃、
解析预算截断、XY 冲突组合（`UU`/`AA`/`DD`/`AU`）、未知状态字符不产生半条改动。

## 6. 未取到的证据 / 未做（如实记录）

- **没有像素证据**：本机此刻有一个 `seelex-gui` 进程在运行（3:12:34 启动），其二进制
  是 2:42 构建的（前端资源经 `go:embed all:frontend/dist` 打包，**不含**本轮改动）。
  为不与用户正在使用的实例抢数据根锁，没有重启它，因此"面板渲染后的样子"目前只有
  `workspace-changes.test.mjs` 的渲染串断言 + `index.html`/`app.js` 的 id 静态一致性
  佐证（`code-pane-changes`/`changes-view`/`changes-count` 两端各命中）。下次 commit
  触发 post-commit 重建后即可在 GUI 里看到（资源管理器子页第三块面板）。
- **冲突态只有纯函数证据**：`UU`/`AA`/`DD` 的分类由表驱动用例覆盖，没有造真实
  merge 冲突仓库跑集成（造它需要在测试里建两条分支 + 冲突文件，收益边际）。
- **大仓库性能未实测**：limit（默认 200/上限 1000）与解析预算（20000 条）是按
  `-uall` 在未收敛仓库上可能一次吐出上百 MB 的防御性设计，没有在超大仓库上压过。
- **明确不做**：diff/行级内容（只读元数据边界）、暂存/还原等写操作（本轮只读）、
  已删除文件的行点击预览（没有字节可读，行不可点）。
