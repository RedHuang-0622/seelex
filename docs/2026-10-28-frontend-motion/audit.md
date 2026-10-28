# 前端动效迁移审计（2026-10-28）

审计范围：`gui/frontend/dist/styles.css`、`motion.js`、`components.js`、
`conversation-view.js`、`chat-view.js`、`app.js`、`trajectory.js`、
`trajectory-view.js` 及相关 Node 测试。

事实源：[前端宪法](../../gui/FRONTEND-CONSTITUTION.md)。Snapshot、Event、Schema、
持久化格式和业务状态所有权均未改变。

## 迁移前

| 类别 | 发现 |
|---|---|
| 时长 token | `--dur-fast:160ms`、`--dur-med:240ms`、`--dur-slow:400ms`，组件混用旧名；只有 `--ease-out`。 |
| 通用 spinner | `.runtime-spinner` 使用 `runtime-spin 1.4s linear infinite`；文件树 `.tree-loading` 使用 `worktree-spin .8s`。 |
| 工具运行态 | 只有 `RUN` 文本，没有独立 activity 层，也没有工具运行 shimmer。 |
| 布局属性动画 | Plan 节点用 `top` 做 `trace-sweep`；分隔条过渡 `left`；effort 活塞过渡 `width`；计划进度条过渡 `width`；wheel dash 过渡 `width`；文件预览声明 `display` transition。 |
| 3D | 弹窗使用 `perspective:1400px`、`rotateX(7deg/5deg)`。 |
| 裸时长/缓动 | `120ms ease`、`.12s ease`、`180ms ease`、`1.4s ease`、`1.4s linear`、`.8s linear`。 |
| 流式 | 每个 `message.delta` 都直接进入整份 conversation 渲染；稳定消息 HTML 变化时 `replaceWith` 整条 article。 |
| 滚动 | 已有 `followsTail`，但渲染路径没有独立纯判据固定“用户接管后不抢底部”。 |
| 轨迹 | 左侧条目和 IN/OUT 详情都塞在同一个 `details` 行内；没有左侧列表/右侧 inspector 的主从布局。 |
| 圆角 + 单侧 margin | 多处圆角容器使用 `margin-bottom`、`margin-top` 或单侧 shorthand 定位。 |

## 迁移后

| 动效/过渡 | 实现 | reduced-motion |
|---|---|---|
| 按压 | `transform` / `box-shadow`，100ms | 立即终态 |
| hover/focus/tooltip | 颜色、背景、阴影、opacity，150ms | 立即终态 |
| 展开 chevron | `transform`，250ms | 无过渡 |
| 弹层进入 | overlay 150ms；card opacity + `translateY(8px)` + scale，250ms | 立即出现 |
| 弹层退出 | opacity + `translateY(6px)` + scale，150ms | 立即关闭 |
| 视图进入/退出 | opacity + `translateY(8/-6px)`，250/150ms | 直接替换 |
| 列表首次插入 | opacity + `translateY(6px)` / 背景色，250ms | 立即显示 |
| 工具 running | 卡内 activity 层 `translateX`，1.8s linear | 静态活动线 |
| 对话尾部 | 可读状态文字；接收中才附局部活动线 | 保留文字与静态线 |
| tooltip | opacity + 最多 2px 位移，150ms | 立即显示 |
| 定位高亮 | `box-shadow`，400ms 内结束 | 立即高亮终态 |

所有 `transition` / `animation` 时长和缓动均使用 token；JS 的 CSS 兜底时长在
`motion.js` 命名，流式 cadence 在 `stream-cadence.js` 命名。CSS keyframes 只写
`opacity`、`transform`、`background-color`、`box-shadow`，不再包含布局属性。

## 删除 / 合并

- 删除 `runtime-spin`、`runtime-pulse` 与 `.runtime-spinner`。
- 删除 `worktree-spin`，文件树改为局部静态活动线。
- 删除 `trace-sweep`，Plan running 改为静态语义线。
- 删除弹窗 `perspective`、`rotateX`，改为 2D opacity/translate/scale。
- 删除 `left`、`width`、`display` transition；进度变化直接落终值。
- 删除轨迹行内 `details` 详情路径；详情唯一落点为右侧 `renderTrajectoryInspector`。
- `prefersReducedMotion()` 仍是唯一 JS 判断；cadence 常量仍是唯一流式判据。

## 遗留债务

`border-color` transition 仍存在于旧按钮、列表与卡片规则中。本轮不把它们纳入
新的时长语义；按宪法 §4.4，后续按受影响组件逐步改成静态颜色切换。

## 验证

- `node --test gui/frontend/dist/motion-contract.test.mjs`
- `node --test gui/frontend/dist/stream-cadence.test.mjs`
- `node --test gui/frontend/dist/tool-running-motion.test.mjs`
- `node --test gui/frontend/dist/shape-spacing.test.mjs`
- `node --test gui/frontend/dist/trajectory.test.mjs`
- 仓库要求的完整前端/Go 验证命令见 `gui/frontend/README.md`。
