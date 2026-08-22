# 工作表格反馈打点表（2026-08-22）

> 一次性工作包：本目录只记录本次反馈的修复过程，不冒充长期事实来源；
> 修复落地后同步到 `docs/gui/modules/work-table.md` 与模块 README。

## 汇总表

| 编号 | 反馈 | 目标 | 状态 |
|------|------|------|------|
| WT-1 | 工作表格「查看详情」「查看打点」两个功能丢失 | 恢复入口并加固弹窗内事件委托 | 完成 |
| WT-2 | Assignee 应由系统按 `role:sessionID` 被动识别并自动上名单（tasklist/plan/subagent/task 全覆盖） | 注册表默认身份 + 认领语义，AI 不再提供自由文本 | 完成 |
| WT-3 | 工具调用 in/out（bash 等）视觉差 | 去掉左侧高亮边框；更小圆角；顶部高亮；状态用颜色表达 | 完成 |

## 打点明细（追加式，时间倒序）

| 时间 | 编号 | 操作 | 状态 | 证据 |
|------|------|------|------|------|
| 2026-08-22 | WT-1 | 恢复入口 + 测试 | done | `work-table.js` 行级「详情/打点」保留；`bind()` 新增 `data-plan-node-open → onDetail` 委托并 `stopPropagation`；`work-table.test.mjs` 新增 2 个委托用例（42 用例全过） |
| 2026-08-22 | WT-2 | 实现被动识别 | done | `dto.ActorIdentity(role, sessionID)`；task 注册表 `SetDefaultIdentity` + 创建自动上名单 + `AttachParticipant` 认领语义；`Runtime.newMainSession` 注入 `main:<mainSessionID>`；`syncPlanNodeTask/syncSubagentTask` 被动补全；fork 不再写裸 node id；`go test ./...` 44 包全绿 |
| 2026-08-22 | WT-3 | 视觉重构 | done | `.tool-run` 去掉左 3px 边框 → 顶部 2px 状态高亮；圆角 `--r-xl`→`--r-lg`；头部按状态使用 `--tint-running/done/failed` 底色；状态文字仍走 `--status-*` 色 |
| 2026-08-22 | WT-2 | 调研命名素材 | done | 主会话 `sess_<nano>`（`seelebridge/runtime.go newMainSession`）；子代理 `node-<hash>`（`SubAgentTreeNode.SessionID`）；role 词表 `agent/subagent/goalplan`（`dto.AccountRole`） |
| 2026-08-22 | WT-1 | 定位入口 | done | `work-table.js` 行级「详情/打点」按钮仍在；`app.js` 有全局 `data-plan-node-open` 监听；补显式委托 + 测试 |
| 2026-08-22 | WT-3 | 定位样式 | done | `.tool-run` 左 3px 状态边框 + `--r-xl` 圆角；改为顶部 2px 高亮 + `--r-lg` + 头部状态底色 |

## 验证与构建

- `go test ./... -count=1 -timeout=300s`：44 个包全绿（并行负载下
  `TestRuntimeProjectScopedToolsUseBoundProject` 曾超时抖动，单独复跑通过）。
- `node --test gui/frontend/dist/*.test.mjs`：111 个用例全绿。
- `gofmt -l` 变更文件：无输出。
- 预检（MEMORY.md 铁律）：无运行中 seelex 进程；`dist/seelex-gui-dev/` 仅含
  `seelex-gui.exe`，无 `.seelex/` 与配置副本；仓库根 `.seelex/` 不在
  `make rebuild-gui` 清理范围。
- 重建（done）：`make rebuild-gui VERSION=dev LOCAL_CONFIG=config/accounts.yaml`
  成功；新产物
  `dist/seelex-vdev-windows-amd64-gui/seelex-gui.exe`
  （2026-08-22 19:27 构建，内嵌本次前端改动）+ `seelex-vdev-windows-amd64-gui.zip`
  与 sha256；`config/accounts.yaml` 已按 Dev 流程不透明复制进包内 config。
  `dist/seelex-gui-dev/` 为旧布局遗留目录，本次重建不覆盖（仅含旧 exe）。
