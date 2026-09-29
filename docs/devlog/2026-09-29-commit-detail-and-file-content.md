# 提交记录下钻：这次提交改了哪些文件 + 那个提交时的文件内容（2026-09-29）

> 日期: 2026-09-29 | 范围: `workspace/gitcommit.go`（新）、`workspace/gitread.go`（新）、
> `workspace/readfile.go`、`workspace/gitlog.go`、`workspace/gitchanges.go`、
> `application/contract/dto/gitcommit.go`（新）、`application/contract/workspace_tree.go`、
> `application/contract/workspace_file.go`、`internal/adapters/session_workspace_ports.go`、
> `application/core/workspace_usecase.go`、`gui/bridge.go`、
> `gui/frontend/dist`（`git-log-view.js`、`file-preview.js`、`app.js`、`styles.css`）、
> `docs/gui/modules/right-sidebar.md`、各模块 README
> 回归: `workspace/gitcommit_test.go`（新，17 例：解析纯函数 + 真实仓库集成）、
> `application/core/workspace_tree_usecase_test.go`（1 例新增）、
> `application/core/workspace_file_usecase_test.go`（3 例新增）、
> `gui/bridge_test.go`（2 例新增 + 1 例接线钉子）、
> `gui/frontend/dist/git-log-view.test.mjs`（8 → 19 例）、
> `gui/frontend/dist/file-preview-render.test.mjs`（新，4 例）
> 承接: [2026-09-20-workspace-changes-pane.md](2026-09-20-workspace-changes-pane.md)
> （同一块「资源管理器」子页的第三个面板：那轮的边界写的是「明确不做 diff/行级内容」）

## 1. 目标与边界

用户口径：**「提交记录需要支持点击查看提交的文件详情，以及支持点开看看文件内容」**。

补上的两件事：

1. 点开某一条提交 → 这次提交改了哪些文件（状态、路径、重命名原路径、±行数）；
2. 再点开清单里的一行 → 该文件**在那个提交时**的内容。

边界照旧：文件**清单**是只读元数据（不含补丁、不含 blob 字节）；文件**内容**是一条
受控读取通道，与工作树预览共用同一条可见性边界（相对路径 + containment、忽略目录、
敏感文件名、大小上限、二进制探测）与同一个返回形状（`dto.FileContent`）。两条通道
只有两个区别：字节来自哪里（工作区磁盘 / git 对象库），以及**历史版本没有写入口**。

明确不做：diff/补丁视图（用户要的是"文件内容"）、跨提交对比、blame、暂存/还原等写操作。

## 2. 侦察结论（先用真实 git 钉形状，再写解析器）

本机 git 2.51.0.windows.2，临时仓库上逐条实测（探针脚本不进仓库）：

| 事实 | 实测输出 |
|---|---|
| 头部事实 | `show --no-patch --date=format:%m-%d\ %H:%M --pretty=format:%H\x01%h\x01%an\x01%ad\x01%P\x01%s` → 与提交列表同形，单个提交不带尾换行 |
| 名状态（`-z`） | `A\0added.txt\0M\0bin.dat\0R062\0old name.txt\0new name.txt\0D\0中文名.txt\0` —— 重命名是**原路径在前、新路径在后**两条记录 |
| 行数（`-z`） | `2\t0\tadded.txt\0-\t-\tbin.dat\0 1\t0\t\0old name.txt\0new name.txt\0` —— 二进制是 `- -`；重命名的路径字段为空、原/新各自成一条记录 |
| `--no-patch` 冲突 | `--name-status`/`--numstat` 与 `--no-patch` **不能同时用**（`fatal: options '--name-only', '--name-status', '--check', and '-s' cannot be used together`）→ 头部必须单独一条查询；清单侧用 `--format=` 抑制头部 |
| 合并提交 | 默认 `git show <merge> --name-status` **什么都不给**；加 `--diff-merges=first-parent` 才给出"这次合并把什么带进了主线" |
| 根提交 | 与带不带 `--diff-merges` 无关，全部文件按 `A` 列出 |
| 子目录为 cwd | `-C <子目录> … -- .` 只给子树内的改动，但路径仍是**仓库根基准**（工作区根可能是仓库子目录）→ 与 `GitChanges` 同一套剥前缀 |
| 路径与选项 | `<hash>` 位置会被 git 当选项解析（`--output=` 会报 "option '--output=x' must come before non-option arguments"）→ hash 必须按形状校验，进程侧再叠加 `--end-of-options` 与路径前的 `--` |
| 对象读取 | `cat-file -t`/`-s` 都认 `--end-of-options <hash>:<path>`；`show` 读够就关读端不会挂住（大文件早停可行） |

## 3. 实现（三层各做自己的事）

- **workspace**（`gitcommit.go`）：`GitCommitDetail(root, hash, limit)` 由三条固定 argv
  拼出（头部 + name-status + numstat，两次清单查询除 mode 外逐字相同），
  `GitCommitFileContent(root, hash, relPath, limit)` 走 `cat-file -t` → `-s` → `show`
  三级（先问类型：目录 tree 与子模块 gitlink 不是文件，明确失败而不是把目录清单当正文）。
  路径基准、敏感过滤、`Filtered` 计数与 `GitChanges` 同口径；`-z` 解析是**位置消费**
  （见 §4.4）。四个只读 git 查询的起命令/失败文案收敛到新文件 `gitread.go`
  （`runGitRead` / `resolveGitRoot`），`gitlog.go`/`gitchanges.go` 的两段内联实现随之删掉
  （副作用一处：`GitLog` 的 `Root` 从"调用方原样传入的串"变成绝对路径，与 `GitChanges`
  一致——同名字段在两个结果里语义相同）；`readfile.go` 里抽出纯函数
  `sanitizeWorkspaceRelPath`，成为"工作区可见性边界"的唯一实现。
- **contract / core / bridge**：`GitCommitDetail` 挂 `WorkspaceTreePort`（元数据面），
  `GitCommitFileContent` 挂 `WorkspaceFilePort`（字节面）——与既有的
  `GitLog`/`GitChanges`/`ReadFile` 分层一致；`Service.workspaceFilePort()` 一处解析 root，
  两条读取通道不可能解析到不同的工作区。Bridge 增
  `WorkspaceGitCommitDetail(hash, limit)` / `WorkspaceGitCommitFileContent(hash, relPath, limit)`。
- **前端**：`git-log-view.js` 从"一屏列表"长成**三层视图**（列表 → 提交详情 → 文件内容），
  共用同一个容器与一条容器委托监听；下钻代次（`loadToken`）保证迟到的异步结果不覆盖新
  画面；返回键逐层退回。文件内容的渲染交给 `file-preview.renderReadOnlyContent` —— 它是
  从 `renderLoaded` 里抽出来的**只读渲染入口**（markdown/代码/文本/图片/PDF/Word/二进制
  提示一条分派），`renderLoaded` 自己改为调用它：渲染分派因此只有一份实现。

## 4. 四个值得记下来的决策

1. **历史版本不进「文件详情」抽屉**。抽屉按**路径**认身份（同名文件共用一枚 chip，
   历史版本会与工作区版本串台），而且它**带编辑面**（Ctrl+S 直接写工作区文件）——一次
   保存就能把某个历史版本写回工作区。所以第二层内容在提交记录面板内只读渲染；但"怎么
   渲染一份字节"仍然复用抽屉的渲染器（新增 `file-preview-render.test.mjs` 钉它）。
2. **hash 按形状校验（4~64 位十六进制）**，不接受 `HEAD`/`master~1` 等修订表达式：
   调用方手里就是提交列表下发的完整 hash，接受表达式只会把"能写什么"的口子开大；
   进程侧再叠加 `--end-of-options`（修订参数）与 `--`（路径）两道。
3. **合并提交按首父给差异**（`--diff-merges=first-parent`）：默认 `git show <merge>` 给
   空清单，那会让"点开一个合并提交"看起来什么都没改。
4. **`-z` 名状态按位置消费，不按"像不像状态"猜**：`-z` 下路径是裸字节，一个名叫 `M`
   的文件与状态 `M` 在文本上无法区分。畸形记录只做一件事——不带偏后续记录（半条重命名
   连同已占用的原路径一起跳过）。那条"拿形状去猜"的写法在第一版就被自己的用例否掉了
   （见 §5）。

## 5. 验证（红 → 绿）

先写用例再补齐实现，实现过程中真红过三次（照实记录）：

```text
# 第一轮：解析器在畸形输入上失步 / 分类表越界 / 对 git 范围的预期写错
--- FAIL: TestParseGitNameStatusJunkAndBudget
    dangling rename must produce no record: [{status:M oldPath: path:M}]   # 悬空重命名把下一条状态读成了路径
--- FAIL: TestCommitFileKindLetters
    commitFileKind("U") = "", want "conflicted"    # U 只出现在 git status 的 XY，提交差异里不存在 → 用例期望写错
--- FAIL: TestRepoGitCommitDetailSubdirWorkspaceRootStripsPrefix
    sibling changes must be counted as filtered: filtered=0   # `-- .` 已在查询侧挡掉兄弟路径，不会进 Filtered → 用例期望写错

# 第二轮（全绿）
$ go test ./workspace/ -count=1
ok  github.com/RedHuang-0622/seelex/workspace   21.5s
$ node --test gui/frontend/dist/git-log-view.test.mjs        # tests 19 / pass 19
$ node --test gui/frontend/dist/file-preview-render.test.mjs # tests 4  / pass 4
```

第二轮修好解析器后，`TestParseGitNameStatusJunkAndBudget` 的输入也改成"格式本身可能
出现"的畸形（半条重命名 + 后续正常记录），期望因此是确定的：半条被丢弃、后续两条照读。

全量门禁与结果见 §6。

## 6. 已跑的门禁 / 未取到的证据

已跑（本机 Windows）：

```text
gofmt -l .          # 本次改动文件全部干净；报出的 vendor/、_tmp/、tmp/ 与
                    # application/core/history_safety.go 是**既有**未格式化文件，未触碰
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...                                                      # exit 0
go test ./... -count=1 -timeout=180s      # 67 个包 ok / 0 FAIL（含 e2e）
node --test gui/frontend/dist/*.test.mjs  # tests 535 / pass 535 / fail 0
python scripts/check_readme_refs.py --strict   # 3 处未解析引用，全部在**本次未触碰**的
                                               # README（application/core/README-session.md、
                                               # internal/bootseed/README.md、sessionstore/README.md）
```

未取到的证据：

- **没有像素证据**：本轮改动落进 `gui/frontend/dist`（经 `go:embed` 打包），要看到
  真机效果需要重新构建 GUI 二进制并在桌面上点开——构建会重建 `dist/`（`MEMORY.md`
  铁律：先预警、先查进程与配置、先备份），本轮没有做，也没有需要这么做的用户要求。
  因此"两层下钻在真机上长什么样"目前由 `git-log-view.test.mjs` 的渲染串断言 +
  `gui/bridge_test.go` 的接线钉子佐证。
- **超大仓库未实测**：解析预算（20000 条）与 limit 钳制是按 `-uall`/一次性大提交
  的防御性设计，没有在超大仓库上压过。
- **`--diff-merges=first-parent` 需要 git ≥ 2.31**：更老的 git 上这条查询会报
  "unknown option"，用户会看到 git 的原话（`Result.Error` 展示态），不是静默错。
  本机与 CI 的 git 都远高于这个版本。
