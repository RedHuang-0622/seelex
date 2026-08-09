# Plan DAG 可视化渲染规范

> 状态说明：右侧工作台已由「工作表格」（[`modules/work-table.md`](modules/work-table.md)）
> 统一接管；本文仍是 Plan 树状布局渲染与节点详情弹窗的数据/布局规范
> （`plan-dsl.js` 保留为详情数据面），不再描述右侧栏独立 Plan section。

## JSON 数据源

Plan 状态来自 Snapshot `runtime.plan`：

```json
{
  "plan": {
    "entry_node_id": "research",
    "status": "running",
    "nodes": [
      {"id": "research", "label": "research", "kind": "auto", "status": "completed", "depth": 0, "output": "...", "elapsed": "1.9s"},
      {"id": "pros",     "label": "pros",     "kind": "auto", "status": "running",   "depth": 1, "output": "",    "elapsed": ""},
      {"id": "cons",     "label": "cons",     "kind": "auto", "status": "pending",   "depth": 1, "output": "",    "elapsed": ""}
    ],
    "edges": [
      {"from": "research", "to": "pros"},
      {"from": "research", "to": "cons"}
    ],
    "progress": 0.33,
    "elapsed": "3.2s"
  }
}
```

## 可视化映射

### 节点颜色（status → color）

| status | 色值 | 语义 | 效果 |
|--------|------|------|------|
| `pending`  | `#6B7280` 灰 | 未开始 | 虚线边框 |
| `running`  | `#3B82F6` 蓝 | 执行中 | 呼吸动画 |
| `completed`| `#22C55E` 绿 | 成功 | 实心填充 |
| `failed`   | `#EF4444` 红 | 失败 | 抖动动画 |
| `aborted`  | `#F59E0B` 橙 | 终止 | 斜线填充 |
| `skipped`  | `#9CA3AF` 浅灰 | 跳过 | 打叉 |

### 节点图标（kind → icon）

| kind | icon | 说明 |
|------|------|------|
| `auto` | ⚙️ | LLM 自动执行 |
| `manual` | 👤 | 暂停等人审批 |
| `fork` | 🔀 | 分叉执行 |
| `join` | 🔗 | 合并结果 |
| `if` / `switch` | ◆ | 条件分支 |
| `loop` | 🔄 | 循环 |
| `emit` | 📤 | 输出变量 |

### 节点布局

1. 按 `depth` 分层（0 = 入口，1 = 第一层下游，依此类推）
2. 同 depth 节点水平排列
3. 边从底边到底边，用贝塞尔曲线/正交折线

```
       [research]  depth=0
       ↗        ↘
    [pros]    [cons]  depth=1
       ↘        ↗
      [summary]        depth=2
```

### 进度渲染

- `progress` 显示为整体进度条（0.0–1.0）
- `elapsed` 显示已用时间
- 每节点 `output` 在点击/悬浮时展开显示

## Event 同步

当 plan 节点状态变化时，`EventSnapshotChanged` 推送更新。GUI 无需轮询。

| Event kind | 触发条件 | 动作 |
|------------|---------|------|
| `snapshot.changed` | 任意节点状态/进度变化 | 更新 DAG 节点颜色 |
| `interaction.opened` | manual 节点暂停待人响应 | 显示审批对话框 |
| `interaction.closed` | 用户已响应 | 关闭对话框，等待下一次 snapshot update |

## 审批交互（manual 节点）

当 PlanNode.kind == "manual" 且 status == "running" 时：
1. 同时收到 `interaction.opened` 事件
2. Interaction.options 包含 `execute`/`skip`/`abort`
3. 用户选择后调用 `POST /api/session/{id}/interaction/{id}/resolve`
4. resolve 后 plan 自动继续执行
