# Plan JSON DSL 可视化前置审查

## 目标

以 `runtime.plan` JSON 作为唯一业务状态源，在 GUI 中派生稳定、可测试的 Plan DSL，并让初始快照与 `runtime.changed` 事件使用同一条渲染链。节点状态点、进度、耗时、输出摘要和依赖边必须随 JSON 更新，不在前端维护第二套 Plan 生命周期。

## 当前链路

1. 后端快照通过 `RuntimeState.Plan` 输出计划、节点和边。
2. `protocol.js` 在收到 `runtime.changed` 后原子替换 `snapshot.runtime`。
3. `app.js` 同时在全量快照和增量事件路径调用 `renderPlan`。
4. 现有 `renderPlan` 仅拍平节点并整体替换 `innerHTML`，没有边、稳定 key、输出/耗时信息，也无法保留未变化节点的 DOM。

## 变更范围

- `gui/frontend/dist/plan-dsl.js`
  - 新增纯函数 `planToDSL(plan)`，完成 JSON 规范化、节点拍平、稳定 key、边关联和展示状态派生。
  - 新增 `renderPlanDSL(dsl)`，输出安全转义的初始标记。
  - 新增 `reconcilePlanDSL(container, dsl)`，按节点/边 key 复用 DOM，仅更新变化字段。
- `gui/frontend/dist/app.js`
  - 删除本地 Plan 拍平和业务展示逻辑。
  - 初始快照及 `runtime.changed` 都调用 JSON → DSL → reconcile 链路。
- `gui/frontend/dist/styles.css`
  - 合并旧 Plan 样式，补充节点卡片、状态点、进度条、依赖边和分支提示。
- `gui/frontend/dist/plan-dsl.test.mjs`
  - 覆盖并行 DAG、嵌套 children、稳定 key、状态迁移、异常字段和 HTML 转义。
- `gui/frontend/dist/protocol.test.mjs`
  - 验证 `runtime.changed` 会替换 Plan JSON，避免渲染读取到旧状态。

## 接口与数据边界

```js
planToDSL(planJSON) -> planDSL | null
renderPlanDSL(planDSL) -> escapedHTML
reconcilePlanDSL(container, planDSL) -> void
```

DSL 只包含展示所需的规范化字段，不拥有生命周期，不反向写入后端 JSON。每次快照或事件到达时重新派生，DOM 仅作为渲染缓存。

## 风险与处理

- 重复/缺失节点 ID：优先使用节点 ID，缺失或重复时加入父路径与序号，保证本次渲染 key 唯一。
- 无效边端点：仍展示边，但标记为 dangling，不阻断整个 Plan。
- 未知状态：归一为 `unknown`，不伪装成成功或等待。
- 文本注入：所有 HTML 字符串都转义；增量更新使用 `textContent`。
- 高频事件：计划 key 不变时复用 board；节点和边按 key 对账，状态变化不重建无关卡片。
- 当前工作区正在进行 `application/*` 包迁移：本变更不覆盖迁移中的后端文件，只消费既有 JSON 合同。

