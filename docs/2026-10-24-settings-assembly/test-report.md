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
- 项目设置可打开并包含 SSH 环境表；
- 会话 SSH 控件不在项目设置弹窗内；
- 项目 SSH 环境保存触发 `SetWorkspaceEnvironment`。

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
  角色默认；会话覆盖仍可编辑并回显 `permission_vector`；SSH 用户空间弹窗写入两条路径；
  账户新增、编辑（空密钥保留）、删除确认均触发对应 Bridge；设置弹窗滚动前后关闭按钮
  相对卡片上沿位置均为 45px。
- 冒烟现场发现并修复 `renderAccountBalanceRows` 悬空调用与后端 `permission_vector` /
  前端 `vector` 字段错位，两处均已由结构用例钉住。
- Computer Use 原生 pipe 仍不可用；上述冒烟使用真实 `gui/frontend/dist` 页面与浏览器
  DOM 事件，Wails 宿主窗口未由 Computer Use 驱动。
