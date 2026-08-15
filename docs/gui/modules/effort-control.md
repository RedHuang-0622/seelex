# Effort 常驻控件详细设计

> 状态：已实现
> 总体架构：[`../architecture.md`](../architecture.md)

## 1. 目标与边界

Effort 控件把 Core 已有的 `lite / medium / high / max` 四档能力变成顶栏常驻滑杆。它负责档位展示、拖动预览、提交状态、失败回滚、可访问性和视觉反馈；它不定义各档 Prompt、循环上限或模型策略。

业务真值仍在 `application.EffortManager`，调用路径仍是 `Bridge.SwitchEffort → Service.SwitchEffort`。前端只有一次操作中的临时预览状态，下一份 Runtime Snapshot 始终可以覆盖它。

主要实现：

- 语义挂载点：`gui/frontend/dist/index.html` 中 `id="effort-control"` 区块（位于 composer，独立于 Runtime modal）；
- Controller：`gui/frontend/dist/effort-control.js:1-77`；
- Composition：`gui/frontend/dist/app.js:41-50`、`app.js:166-177`；
- Core 动作：`application/app.go:213-229`；
- 视觉状态：`gui/frontend/dist/styles.css` 的 `/* Effort */` 段（`.effort-control` 系列选择器）。

## 2. 四档数据模型

`EFFORT_LEVELS` 是唯一有序表：

| index | level | 进度 | 展示 |
|------:|-------|-----:|------|
| 0 | `lite` | 10% | Lite |
| 1 | `medium` | 38% | Medium |
| 2 | `high` | 66% | High |
| 3 | `max` | 100% | Max |

`effortPresentation(level)` 将不可信 Runtime 值归一为 `lite`，并一次性产生 index、label、progress 和 `isMax`。HTML range 只传递 index，Bridge 只接收归一后的 level 字符串。

## 3. 交互状态机

实现位置：`gui/frontend/dist/effort-control.js:22-76`。

```text
Committed(level)
  ├─ input(index) ──→ Preview(level)         只更新视觉，不调用 Bridge
  ├─ change(same) ──→ Committed(level)       无远程调用
  └─ change(new) ───→ Pending(new)
                         ├─ resolve ─→ Committed(new)
                         └─ reject  ─→ Committed(old) + toast

Runtime Snapshot ──→ setLevel(level) ──→ Committed(level)
```

选择 `change` 而不是每次 `input` 调用 Bridge，避免用户拖动时连续修改 PromptStack 和 Engine MaxLoops。Pending 期间 input 被禁用并显示等待光标，阻止并发提交。

## 4. 权威状态与失败恢复

`app.js` 注入 `selectEffort` 端口，该端口依次调用 `SwitchEffort` 和 Snapshot refresh。成功后 Controller 提交新值；Bridge/Core 失败时 Controller 恢复旧 committed 值并把错误交给统一 toast。

`renderRuntime` 每次收到全量 Snapshot 或 `runtime.changed` 都调用 `setLevel`，因此命令行 `/effort`、TUI 或未来远端控制导致的变化也能同步到滑杆。Controller 不持久化业务状态。

## 5. 视觉状态与动效克制

实现位置：`gui/frontend/dist/styles.css` 的 `/* Effort：紧凑分段滑轨 */` 段。

控件是一枚紧凑分段滑轨（Lite/Max 两端标签 + 细轨填充），视觉原则与全局设计系统一致：克制、无装饰：

- 每档只切换填充色：`lite` 绿、`medium` 青、`high` 琥珀、`max` 紫；不使用渐变、辉光、流光或呼吸动画；
- 已移除针筒玻璃反光、刻度点与 `effort-max-aura`，不产生常驻 GPU 动画；
- `--effort-progress` 与 `data-effort` 仍是 Controller 的派生视图，不影响提交逻辑；
- `prefers-reduced-motion: reduce` 关闭全部过渡与动画，颜色状态保留。

色值与进度映射只允许出现在 `:root` 的语义色 token 与 `EFFORT_LEVELS` 中，组件不得硬编码。

## 6. 布局与可访问性

- 控件位于 composer，独立于 Runtime modal；嵌入资源契约在 `gui/bridge_test.go:641-652` 固定该边界（`id="effort-control"` 先于 `id="runtime-modal"`，且 modal 内不得出现 `id="effort-range"`）。
- `role=group` + `aria-label` 描述控件用途；range 使用 `aria-valuetext` 暴露 Lite/Medium/High/Max，而不是裸数字。
- 若 HTML 挂载了 `id="effort-value"`，Controller 会同步其文本；当前未挂载，档位由两端标签与 `aria-valuetext` 表达。
- 键盘可使用方向键和 Home/End 操作原生 range。
- 780px 以下压缩滑杆宽度，provider/model 文本收起但连接状态点保留（`styles.css` 响应式段）。
- Max 不能只靠颜色表达：右端 `Max` 标签常驻，range 的 `aria-valuetext` 同步为 `Max`。

## 7. 错误与边界策略

| 场景 | 处理 |
|------|------|
| Runtime level 未知/空 | 归一为 `lite`，不让 range 落入非法 index |
| 拖动但未松开 | 只预览，不修改 Core |
| 重复选择当前档 | 直接恢复 committed 视图，不调用 Bridge |
| Bridge 尚未就绪/调用失败 | 回滚、解除 disabled、统一 toast |
| 提交中收到重复 change | pending guard 忽略 |
| 用户要求减少动态效果 | 全局 `prefers-reduced-motion` 关闭过渡与动画，颜色状态保留 |

## 8. 自动化证据

- `effort-control.test.mjs:36-41`：四档映射、进度和非法值回退；
- `effort-control.test.mjs:43-52`：Runtime Max 状态、ARIA、CSS selector 数据；
- `effort-control.test.mjs:53-64`：拖动只预览、change 单次提交；
- `effort-control.test.mjs:66-77`：Bridge 失败回滚；
- `application/command_test.go:564-574`：Core `SwitchEffort("max")`；
- `gui/bridge_test.go:104-143`：Bridge 动作委托；
- `gui/bridge_test.go:228-241`：控件在 modal 外且 app.js 使用独立 Controller。

## 9. 审查清单

- 四档顺序是否仍与 Core `orderedLevels` 一致？
- 新的档位是否同步更新 range max、映射测试和 Core 校验？
- `input` 是否保持纯预览，只有 `change` 才调用 Bridge？
- 失败是否回滚到最后一份权威 committed 状态？
- Effort 视觉是否保持克制（无辉光/流光/常驻动画）并支持 reduced-motion？
- Effort 是否仍可在不打开任何 modal 的情况下看到和操作？
