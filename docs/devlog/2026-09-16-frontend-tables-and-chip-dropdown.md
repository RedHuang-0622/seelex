# 前端表格化与权限档芯片下拉（2026-09-16）

## 背景（用户口径）

三处"不像人用的"：

1. **Goal 活动栈**是一堆竖排 `div`，多帧叠起来看不出"哪一帧是现在"、"栈上还有什么"；
2. **Agent Team 角色记录**是两条竖排车道（main 车道 / 自身车道），**看不出"哪一回合属于谁"**
   ——用户直接画了一张表：`main` 一行、`tl` 一行，一列一个回合；
3. **切权限档要先点开运行状态弹窗，再在大面板里找档位**——"同时切换权限的方式
   如同下拉个 chip 一样简单，而不是点开出现个大面板让我选择"。

外加一处分发物瑕疵：Dev GUI 包解压出来的根目录是 `.stage-seelex-vdev-windows-amd64-gui`。

## 改了什么

| 面 | 文件 | 变更 |
|---|---|---|
| Goal 活动栈 | `gui/frontend/dist/goal-stack-view.js` | `renderGoalStack` 改为渲染**专用表格**（`<table class="excel-grid goal-stack-table" data-goal-stack-table>`，列：帧/状态/目标/陈述/验收/最近进度/更新）；`renderGoalFrame` 输出 `<tr>`（`data-goal-frame` / `data-goal-status` / `data-goal-frame-active`），栈底→栈顶、栈顶标 active；新增 `formatGoalTime`（容忍秒/毫秒戳，解析不出来显示 `—`） |
| 角色记录 | `gui/frontend/dist/agent-team-view.js` | `renderRoleRecordLanes` → `renderRoleRecordTable`：**一行一条车道 × 一列一个回合**；单元格分类 `is-main`（主会话自己的回合）/ `is-shared`（入伙后的共享回合）/ `is-own`（它自己的行或未同步 draft）/ `is-outside`（入伙前占位 `—`）/ `is-empty`（该车道此回合为空，`·`）；表头 `车道 + seq` |
| 权限档 | `gui/frontend/dist/{permission-tier.js,app.js,index.html,styles.css}` | chip 点击**就地展开下拉**（`.perm-picker` > `#perm-toggle` + `#perm-menu`，向上展开）；条目模型 `permissionTierMenuItems`（与运行状态里的列表同源）；唯一提交路径 `applyPermissionTier`；键盘 `↑/↓`（纯函数 `nextTierIndex` 循环）、`Enter`/空格提交、`Esc`/点浮层外关闭；`togglePermissionMenu` 维护 `aria-expanded`/`aria-haspopup`；`▾` 作为可下拉提示 |
| 分发物 | `scripts/build-gui.ps1` | 压缩前把暂存目录 `.stage-<pkg>` 改名成 `<pkg>`，归档根目录与发布包口径一致；压缩后两个临时目录都清掉 |

## 为什么这么做（不是审美）

- 活动栈与角色记录都是**多条同构记录**：表格是唯一能"逐行对齐字段、逐列对齐回合"的形态，
  而且直接复用工作表格/定时任务表那套 `excel-grid` 样式，不新造视觉语言。
- 下拉 > 面板：切档是**高频小动作**，面板把它当成"进入一次配置页"；下拉把动作压回一步，
  同时保留运行状态弹窗里的档位列表作为总览口（两处同源，不会各记一份选中态）。
- 归档根目录是分发物的"第一眼"：`.stage-*` 是构建脚本的内部暂存名，不该被用户看到。

## 不变量（未被这次改动破坏）

1. **渲染层零回写**：Goal 面板与 Team 面板仍是纯渲染件，没有任何回写记录/前缀/档位的入口。
2. **`data-*` 钩子名沿用**：`data-goal-frames`、`data-goal-frame*`、`data-record-cut/outside/visible/own`、
   `data-role-record-table`——前端测试与 headless 断言的口径不变。
3. **档位事实源不变**：`snapshot.runtime.permission_tiers`（目录）+ `snapshot.runtime.permission_tier`
   （本会话生效档），旧 `full_access` 仍是派生位。
4. **运行状态弹窗的档位列表保留**（总览口），与下拉共用 `applyPermissionTier`。

## 证据（本次实测）

| 项目 | 命令 | 结果 |
|---|---|---|
| 前端用例 | `node --test gui/frontend/dist/**/*.test.mjs` | 316 passed / 0 failed（2.30s） |
| Go 全量 | `go test ./... -count=1` | 65 包 `ok`，0 `FAIL`（151.6s） |
| Go 编译 | `go build ./...` | exit 0 |
| 暂存构建 | `scripts/seelex-flow.ps1 -Stage Stage` | `dist/stage-gui/seelex-gui.exe` 28.4 MB，SHA256 `FD4A0F5F…E197158` |
| 无头冒烟 | `scripts/seelex-flow.ps1 -Stage Smoke` | version=`dev` PASS；headless boot `ready=True` PASS |
| 可分发包 | `scripts/build-gui.ps1 -Version dev -BuildKind Dev` | `dist/archive/seelex-vdev-windows-amd64-gui.zip` 10.5 MB；**归档根目录 = `seelex-vdev-windows-amd64-gui`**（修复前为 `.stage-seelex-vdev-windows-amd64-gui`） |
| 包内 exe 携带新前端 | `findstr /C:"<marker>"` | `goal-stack-table`=2、`role-record-table`=2、`perm-menu-item`=6、`perm-picker`=3（与暂存 exe 一致） |

## 未做 / 风险

- **未执行 Deploy**：基线进程 `dist/seelex-gui-dev/seelex-gui.exe`（PID 125820）正在运行，
  归位需要先退出该进程再跑 `scripts/seelex-flow.ps1 -Stage Deploy -Yes`。
- 未跑 `gui/*_live_probe_test.go`（需真实模型凭据）；它们才是"team work 在真实模型下跑通"的证据面。
- 归档根目录修复只验证了 Dev 包；Publish 路径由 `seelex-flow.ps1` 的 Release 分支构建，口径本就正确
  （`seelex-v0.0.2_release-windows-amd64-gui`），未受影响。
