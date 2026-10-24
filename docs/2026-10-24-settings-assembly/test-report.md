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
