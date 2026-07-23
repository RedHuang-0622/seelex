# GUI 客户端稳定性最终审查

## 变更概览

| 模块 | 设计模式 | 结论 |
|------|---------|------|
| Go Snapshot/Event | Versioned DTO、Observer | 明确协议版本和稳定实体 ID |
| 前端状态 | Reducer、Fallback | 正常事件增量归并，异常自动重同步 |
| 会话渲染 | Keyed Reconciliation、Presentation Model | 主会话区不再整体 `innerHTML` 重建 |
| Snapshot 竞争 | Revision Floor | 权威 Snapshot 已包含的旧事件不会重放 |

## 五轴审查

| 维度 | 状态 | 评分 | 备注 |
|------|:---:|:---:|------|
| 正确性 | 通过 | A | 覆盖协议不兼容、事件缺口、旧事件重放、同 revision 多事件与稳定 key |
| 可读性 | 通过 | A- | reducer、客户端状态、DOM 协调和 Chat 控件已拆分；`app.js` 保持 500 行 |
| 架构 | 通过 | A | Core 仍是唯一状态源；模块依赖单向；Snapshot 保留为恢复路径 |
| 安全性 | 通过 | A | 动态值经转义/安全 Markdown 渲染；工具完整输出不默认进入 DOM；无凭据变更 |
| 性能 | 通过 | A- | 流式 delta 不再反复调用 Bridge Snapshot，不再重建整个会话 DOM |
| Go 专项 | 有条件通过 | B+ | vet/build/test 通过；本机 race 受 CGO 环境限制，由 Ubuntu CI 承担 |

## 审查发现与处置

### 已修复

1. Snapshot 与事件竞争可能重复追加 delta：增加权威 Snapshot revision floor。
2. 分页历史消息缺少 ID：加载时分配稳定消息 ID。
3. 工具 ID 使用 `name-turn`，跨 ChatStream 可能重复：ToolHookBridge 改为唯一序列 ID并配对 start/complete。
4. Runtime 事件浅拷贝可能暴露可变 Plan：增加 Runtime 与递归 Plan 深拷贝。

### 非阻塞限制

1. 仓库尚无真实 Wails WebView 自动化测试；需要本地 GUI 手工验收。
2. application/gui 合并覆盖率为 76.1%，低于通用 80% 建议线；GUI 包为 88.7%。
3. Windows 本机没有 CGO race 环境；现有 Ubuntu CI 已配置全仓 `-race`。

## 亮点

- 会话节点使用后端实体 ID，而非数组位置。
- seq 缺口、未知事件和无法归并 payload 都走统一重同步路径。
- 滚动跟随、历史锚点、思考块和工具 OUT 展开状态均由客户端视图维护。
- 正常流式路径不再产生每 chunk 全量 Snapshot/DOM 成本。

## 最终判断

有条件通过，可用于本地 alpha 验收。正式合并条件为 Ubuntu CI race 通过，并完成真实 WebView 的长输出、历史滚动、工具 IN/OUT、`<think>` 展开和输入队列手工检查。
