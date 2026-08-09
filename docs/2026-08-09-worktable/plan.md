# 2026-08-09 工作表格（Work Table）工作包

## 目标

右侧工作台把 plan / tasklist / subagent fork 状态统一体现为一个工作表格：
条目点开展开多维表格（阶段/任务/描述/状态/Assignee/Dependency/附件），
支持后端状态返回与任务打点、todo 三态状态更新、subagent 支持与详情上下文
查看。

## 决策

- 独立轻量事件 `worktable.changed`（只带表格，不整份 runtime）。
- 右侧面板直接替换为工作表格（Plan 树 / 待办 / 子代理三个分区移除；节点
  详情弹窗保留）。
- `UpdateWorkItemStatus` 仅支持 todo 三态 pending/doing/done（不新增工具族）。
- 资源并发安全：Actor + Mailbox（todoState）；高并发事件流转：CSP 汇聚
  发布器（channel latest-wins）。
- 前端 reducer 内存优化：`runtime.changed` 不预克隆 plan；`subagent.*`
  路径级结构共享；表格 keyed reconciliation。

## 实施记录

- 后端：`model.WorkItem/WorkTracePoint/WorkTableEvent`、`RuntimeState.WorkTable`、
  `EventWorkTableChanged`、`application/core/work_table.go`、
  `worktable_publisher.go`、`seelebridge/todo_tool.go`（mailbox actor + 三态）、
  `gui/bridge.go` `UpdateWorkItemStatus`。
- 前端：`work-table.js`、`app.js` 接入、`index.html` 替换右侧分区、
  `styles.css` 表格样式、`protocol.js` 增量与结构共享。
- 测试：白盒（投影/actor/发布器）、黑盒（bridge 契约、schema 示例）、
  竞态（`-race`）、A/B（payload 体积）、模糊（fuzz）、冒烟（build/vet/node）。
- 文档：`docs/gui/modules/work-table.md`、schema/example、
  `docs/test/worktable-ab.md`、CHANGELOG、ADR-GUI-020。

## 验收状态

- [x] 后端投影与 `worktable.changed` 增量
- [x] todo 三态与 Bridge 更新入口
- [x] 前端工作表格（展开/筛选/打点/详情/状态按钮）
- [x] 内存优化（无整树深拷贝、keyed reconcile）
- [x] 测试与文档

已知限制：plan/subagent 状态仍由执行器权威管理（手动覆盖未开放）；子代理
行附件 v1 多为空，失败现场 worktree 路径在详情上下文标签中展示。子代理
生命周期经 `SetSubagentTreeObserver` 被动触发工作表格（无需模型调用工具）；
done 节点有界保留（50），显式清空入口 `ClearSubagentTree`。
