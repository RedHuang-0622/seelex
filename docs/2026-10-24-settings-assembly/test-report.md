# 设置与角色装配验收记录

## 自动化验证

| 门禁 | 结果 |
|---|---|
| `go test ./... -count=1 -timeout=120s` | 通过 |
| `go vet ./...` | 通过 |
| `go build -tags "gui,desktop,production" ./...` | 通过 |
| `node --test gui/frontend/dist/*.test.mjs` | 852/852 通过 |
| `git diff --check` | 通过 |

## Post-commit

提交 `5b7ea86e` 后执行 `.githooks/post-commit`，成功重建：

- `dist/dev/seelex.exe`
- `dist/seelex-gui-dev/seelex-gui.exe`

构建前已优雅关闭运行中的 dev GUI；没有删除 `.seelex` 会话、定时任务或 `accounts.yaml`。

## 交互冒烟

Computer Use 原生通道在本机返回 `failed to connect native pipe: 系统找不到指定的文件`，
无法直接驱动 Wails 窗口。补充使用同一份 `gui/frontend/dist` 资源、注入宿主 Bridge 桩的无头
浏览器点击冒烟，逐步验证：

- 设置一级分类打开，四个分类齐全；
- 角色页切换到 `teammate`，账号下拉只出现 `teammate-1` / `teammate-2`；
- 选择角色账号触发并收到 `SetRoleAccount` 回执；
- “添加账户”打开独立二级弹窗；
- 项目设置可打开并包含工作环境表；
- 会话固定环境控件独立于项目设置与全局设置；
- 项目工作环境保存触发 `SetWorkspaceEnvironment`。

第一轮冒烟发现并修复了 `app.js` 缺少 `permissionVectorSummary` import 的真实运行时错误；
修复后同一套点击步骤全部通过。

## 已知限制

Wails 原生窗口已成功启动，但 Computer Use helper 不可用，因此最终窗口内的人工点击未能由该
工具执行；DOM/事件路径由浏览器冒烟覆盖，Host/Store/Runtime 数据路径由 Go 用例覆盖。

## 2026-10-24 角色权限与账户 CRUD 二轮验收

- 全仓 Node 前端用例：860/860 通过。
- 全仓 `go test ./... -count=1 -timeout=600s` 通过；Windows 上现有重型扫描/工作树用例
  单包可超过默认 120 秒，因此本轮记录实际使用的包级上限。
- `go vet ./...`、`git diff --check` 与 README 引用检查通过。
- `go build ./...` 与 `go build -tags "gui,desktop,production" ./...` 通过。
- 无头浏览器注入 Bridge 桩后完成点击冒烟：四个角色固定出现；角色卡折叠；继承开关只锁
  角色默认；会话覆盖仍可编辑并回显 `permission_vector`；SSH 路径弹窗写入两条路径；
  账户新增、编辑（空密钥保留）、删除确认均触发对应 Bridge；设置弹窗滚动前后关闭按钮
  相对卡片上沿位置均为 45px。
- 冒烟现场发现并修复 `renderAccountBalanceRows` 悬空调用与后端 `permission_vector` /
  前端 `vector` 字段错位，两处均已由结构用例钉住。
- Computer Use 原生 pipe 仍不可用；上述冒烟使用真实 `gui/frontend/dist` 页面与浏览器
  DOM 事件，Wails 宿主窗口未由 Computer Use 驱动。

## 2026-10-24 单一 SSH 权限与会话固定环境

- 权限向量只保留一枚 `ssh`；旧 `ssh.userspace` 在读取/写入规整时迁移为 `ssh`，
  不再作为独立 chip 或权限组出现。
- 项目设置维护本地/SSH 命名环境；会话首次固定后不可切换，未固定会话明确按本地执行，
  不再借用旧单条 SSH 连接自动转远端。
- mainagent、subagent、teammate 与后台工具共用同一执行树环境绑定；模型工具 schema
  不暴露 `env`，伪造入参不能覆盖会话绑定。
- 环境清单与地址作为请求尾部块注入，位于用户输入之后；工作打点表仍在输入之前。
- SSH 路径白名单支持项目级角色默认与当前会话 agent-role 覆盖；会话覆盖优先，
  未覆盖时回落角色默认，更新权限向量不会擦除路径。
- 子代理权限向量解析使用 `subagent` / `teammate` 这类 agent role，而不是 `impl_ui`
  一类业务成员名；主/子代理因此读取同一份设置页选择。
- 自动化门禁：`go test ./... -count=1 -timeout=600s` 通过；
  `go vet -tags "gui,desktop,production" ./...` 通过；
  `go build ./...` 与 `go build -tags "gui,desktop,production" ./...` 通过；
  `node --test gui/frontend/dist/*.test.mjs` 862/862 通过；`git diff --check` 通过。
- 交互冒烟需在 post-commit 重建 dev GUI 后补记，当前不把自动化门禁冒充窗口点击结果。

### Post-commit 与交互冒烟（2026-10-25）

- 重建前备份 dev 包内 `config/` 与 `plugins/` 到本机临时目录；当时无 Seelex 进程运行。
- `SEELEX_POST_COMMIT_BUILD=1 .githooks/post-commit` 成功重建
  `dist/dev/seelex.exe` 与 `dist/seelex-gui-dev/seelex-gui.exe`。
- 新 GUI 启动成功，窗口标题 `Seelex`、`Responding=true`，WebView2 环境创建成功。
- Computer Use 原生 pipe 仍返回 `os error 2`，无法在 Wails 窗口内注入点击；同一份
  `gui/frontend/dist` 通过本机 Chrome CDP 完成 DOM 事件冒烟。
- 上一版已点击验证：全局设置为「角色与权限 / 会话权限 / 应用」三类；角色固定为
  goalplan / mainagent / subagent / teammate；全局设置内没有项目 SSH 环境表，
  也没有 `ssh.userspace` 权限项。
- 未选中 SSH 的 agent role 点击候选 SSH 会打开「允许路径」弹窗；会话权限页的同一
  入口读取会话覆盖路径（夹具为 `/srv/session`），而非角色默认路径。
- 弹窗标题、说明和按钮均为单一 SSH 口径；弹窗头 `position=sticky`，关闭按钮保持
  上沿位置。
- 项目设置显示「工作环境」，主机占位为「留空 = 本地」；会话环境弹窗列出本地
  `dev` 与 SSH `test`，固定后按钮显示 `本会话环境 · test`，选择框锁为 disabled，
  状态明确说明 agent 与工具入参不能切换。
- 角色卡中的「添加账户」打开独立二级「添加账户」弹窗，含 API Key 字段。

### 2026-10-25 项目环境与会话设置层级修正

- 项目环境读写改为显式项目 ID：项目已登记但会话尚未持久绑定时，仍可读取和写入
  1:N 环境表；会话首次固定环境时同样由当前项目 ID 校验，不再反查 session binding。
- 设置一级分类合并为「角色与权限 / 应用」，只编辑角色默认权限；当前会话覆盖从会话
  标题区「会话权限」独立打开，继承 chip 只锁默认栏。
- 主代理 root 判定先读 `agent` 角色向量；选中权限直接 allow，不再被规则层转成 ask。
- 全权档在 role 向量更窄时仍先短路 `bash` 审批；审批卡的 low/medium/high 显示为
  「低风险 / 中风险 / 高风险」，它是操作风险，不是 composer 的思考强度。
- 上下文窗口从角色设置契约和视觉层级中移除；它只存在于具体账户模型条目内，文案
  使用“甜点上下文”，避免被读成角色级继承项。
- 回归覆盖：项目已创建但会话未绑定时，项目环境读面仍返回该项目的 1:N 环境；
  `SetSessionSSHEnv` 按显式项目 ID 校验环境，不再要求 session binding 先落盘。
- 全仓 `go test ./... -count=1 -timeout=600s`、`go vet -tags "gui,desktop,production" ./...`、
  普通与 GUI production 构建通过；前端 `864/864` 通过。
- post-commit 已重建 dev CLI/GUI；新 GUI 窗口标题 `Seelex`、WebView2 环境创建成功、
  `Responding=true`。浏览器截图复核确认一级分类只剩「角色与权限 / 应用」，设置页
  只编辑角色默认权限；会话覆盖从标题区「会话权限」弹窗进入。账户行内直接展示该
  模型账户的甜点上下文。
