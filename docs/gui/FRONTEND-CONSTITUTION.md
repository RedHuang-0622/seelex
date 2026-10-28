# Seelex 前端宪法 v1.0

> 生效日期：2026-10-28
>
> 状态：**已生效**。本文是 Seelex GUI 前端视觉、动效、加载态、流式反馈和交互状态的项目级事实源。
>
> 上游效力：可访问性规则 > 用户提供的《前端宪法 v1.0》 > 本文 > 设计系统与皮肤契约 > 模块文档 > 单次提示词或评审意见。本文只能收紧全局规则，不能降低全局宪法中的不可豁免条款。

## 0. 适用范围与设计立场

### 0.1 适用范围

本文约束所有会进入 `gui/frontend/dist` 的用户可见界面，包括页面、组件、加载态、空态、错误态、流式输出、面板开合、页签切换、拖拽反馈和动效降级。

前端不是静态截图集合，而是 Agent 工作过程的观察面。动效首先要回答“系统正在做什么、用户动作是否被接收、状态是否已经改变”，其次才考虑视觉表现。

### 0.2 Codex 语义基线

Seelex 借鉴 Codex 类工作台的状态语义，但不复制其具体外观：

1. **状态先于装饰**：执行中优先显示可读的任务状态和局部活动，避免用全屏旋转器代替状态说明。
2. **反馈就地发生**：点击、悬停、展开和错误反馈发生在操作对象附近，不让用户去远处寻找结果。
3. **持续输出可见**：模型正文、工具调用和长任务都应有连续但克制的活动信号；终止后立即稳定。
4. **动画表示变化**：静止页面不循环运动；循环只服务于仍在运行的任务或加载。
5. **输入优先**：用户的新输入、取消、切换和滚动可以打断非必要动画，并让界面收敛到稳定状态。
6. **低装饰密度**：同一视口只保留一个主要活动信号，不在卡片、状态点、背景和文本上叠加多套效果。

### 0.3 术语

- **动作**：用户完成的一次可识别操作，例如点击、切换、提交、滚动或取消。
- **状态语义**：该动作在系统里代表的事实，例如已接收、正在运行、等待输入、失败或完成。
- **动效语义**：用哪一类运动把该事实表达给用户。
- **cadence**：内容或状态按什么节奏出现、合并和收敛。流式输出的 cadence 属于交互节奏，不只属于 CSS。
- **滞回窗口**：进入或退出某个视觉模式前必须持续满足条件的时间窗口，用于避免短脉冲造成闪烁。
- **静止元素**：动画期间不得移动或改变几何的锚点，例如外壳、顶栏、左栏、右栏和输入框边界。

## 1. 语义分类与硬性规则

### 1.1 三类 Purpose

每一处动效必须且只能属于下列一类：

| Purpose | 含义 | Seelex 典型场景 |
|---|---|---|
| `Feedback` | 确认用户动作已被接收，或确认系统仍在持续产出 | 按压、提交确认、工具运行、流式输出、成功/失败状态 |
| `Continuity` | 状态变化时维持空间、层级或对象身份 | 页签切换、弹层开合、列表增量、面板收起 |
| `Delight` | 品牌表达 | 当前默认不使用；新增必须有明确产品理由和单独评审 |

不属于以上三类的效果不得进入生产代码。

### 1.2 Trigger 白名单

每处动效必须声明唯一主触发源：

`load` / `enters viewport` / `hover` / `focus` / `click` / `scroll` / `state change` / `route change`

“页面存在所以自动循环”“为了显得活跃所以呼吸”不是触发源。运行中 shimmer 的主触发源是父对象的 `state change`：state 从稳定切到 running 时开始，离开 running 后必须停止。

### 1.3 语义动作表

| 动作 ID | 用户/系统动作 | Purpose | Trigger | 动效表达 | 静止元素 | 降级 |
|---|---|---|---|---|---|---|
| `A01` | 按下按钮、菜单项、图标键 | `Feedback` | `click` | 100ms 内下沉 1px 或缩到 0.98，颜色/底色同步变化 | 所在布局、相邻控件 | 立即进入按下后的最终颜色，不位移 |
| `A02` | 悬停或键盘聚焦控件 | `Feedback` | `hover` / `focus` | 150ms 内改变底色、文字色或阴影；焦点环不使用动画隐藏 | 控件尺寸与位置 | 立即显示 hover/focus 终态 |
| `A03` | 展开/收起详情、工具参数、推理块 | `Continuity` | `click` / `state change` | 内容以 opacity + 不超过 6px 的 translate 入场；容器几何即时更新 | 所属卡片、滚动容器 | 内容立即显示或隐藏，保留焦点 |
| `A04` | 打开弹窗 | `Continuity` | `click` / `state change` | 遮罩 150ms 淡入；卡片 250ms 从下方 8px、scale 0.98 到终态 | 背景工作区 | 遮罩和卡片立即到终态 |
| `A05` | 关闭弹窗 | `Continuity` | `click` / `state change` | 卡片 150ms 淡出并下移 6px；遮罩 150ms 淡出 | 背景工作区 | 立即关闭，焦点回到触发者 |
| `A06` | 切换会话、页签或主视图 | `Continuity` | `route change` / `state change` | 新视图 250ms 淡入；位移不超过 8px；旧视图在 150ms 内退出 | 顶栏、左栏、右栏、composer | 直接替换内容，不隐藏最终内容 |
| `A07` | 列表首次插入新行 | `Continuity` | `state change` | 小批增量首次插入时 250ms 淡入并上移 6px；会话恢复/历史批量装载不逐行播放，同一 key 的内容替换不重播；兜底结束必须 cancel 动画 | 列表容器与滚动位置 | 新行立即可见 |
| `A08` | 状态从 running 到 done/failed | `Feedback` | `state change` | 150ms 内切换语义底色、文字色和图标；不使用弹跳 | 行高、表格列宽 | 立即切换语义色与状态文字 |
| `A09` | 工具调用运行中 | `Feedback` | `state change` | 工具名字段一次 1.8s 文字 Shimmer；收束态只显示工具类型，展开后显示命令和状态 | 行高、列宽、相邻行 | 去掉扫光，保留工具类型、可访问状态与展开能力 |
| `A10` | 模型持续流式输出 | `Feedback` | `state change` | 按第 3 节的 cadence 合并刷新；不在每个 token 上播放 CSS 动画 | 消息位置、滚动锚点、输入框 | 保留内容更新，移除游标等装饰 |
| `A11` | 长任务超过 10s | `Feedback` | `state change` | 确定性进度或阶段文字；提供取消或后台入口 | 任务归属容器 | 保留文字与进度数值 |
| `A12` | 读取未知结构数据 | `Feedback` | `load` / `state change` | 100ms 延迟后显示状态文字和局部 activity；不伪造骨架结构 | 页面壳与操作区 | 只显示静态状态文字 |
| `A13` | 读取已知结构列表 | `Continuity` | `load` / `state change` | 3–5 项骨架，shimmer 周期 1.8s；与真实行共用容器类 | 滚动容器 | 静态占位 |
| `A14` | 数字或计数变化 | `Feedback` | `state change` | 250ms 内滚动到终值；只能由一个整数段参与 | 所在行与列宽 | 立即写入终值 |
| `A15` | 用户滚动长内容 | `Feedback` | `scroll` | 仅在存在溢出时用 150ms 阴影提示可继续滚动 | 内容几何 | 不显示滚动阴影 |
| `A16` | 提示词、tooltip 或轻量 toast 出现 | `Feedback` | `hover` / `focus` / `state change` | 150ms 淡入，位移动画不超过 2px | 被提示对象 | 立即显示 |

### 1.4 动效作用域与刷新语义

动效只属于**实际变化的最小区域**。动画的边界不得越过该区域扩散到相邻的左右栏、
页签条、输入框或另一张列表；同一个可见性状态被重复声明时必须收敛为 `settle`，
不得重播入场。

| 变化 | 动效范围 | 初始化揭示 | 增量更新 |
|---|---|---|---|
| 应用首次装载 | 首次可见的内容容器 | 允许一次容器级淡入，不做逐行 stagger | 不适用 |
| 切会话 | 对话内容与尾部滚动 | 允许一次对话区场景切换 | 左栏只改选中态；右栏数据未变时完全静止 |
| 列表首次得到数据 | 该列表容器 | 可做一次容器级揭示；批量历史不逐行入场 | 只给首次插入的新 key 行做 A07 |
| 列表数据刷新 | 发生替换的行/值 | 不重播容器入场 | 内容替换不重现入场；新行才入场 |
| 状态 running → done/error | 对应状态文字、图标和局部底色 | 不适用 | A08；不推动父面板 |
| 连续工具调用推进 | 当前工具域的单行摘要 | 不适用 | 新最新调用从下方滚入并 `settle`；历史调用留在轨迹页 |
| LLM 回合结束 | 最后一条 assistant 正文 | 不适用 | 只对该正文做一次轻微 `settle`，不刷新会话、页签或侧栏 |
| Think 内容推进/结束 | 当前思考行 | 不适用 | 思考增量只更新思考行；回合结束再与 LLM 正文局部阻尼落位 |
| IN/OUT 抽屉展开 | 当前详情面板的内容层 | 不适用 | 容器几何即时更新，内容以 opacity + 8px translate 进入；退出 150ms |
| 右栏刷新 | 右栏实际更新的内容 | 不重播右栏视图入场 | 对话区完全静止 |
| 主视图/右栏页签切换 | 该停靠栏的目标视图 | 只有隐藏 → 可见才播 A06 | 另一停靠栏不播动画 |
| 会话列表选择 | 选中行样式 | 不重播列表初始化 | 当前行与上一行更新选中态，其它行静止 |

实现约束：

1. 初始化动效只由 `load` 或容器 `hidden → visible` 触发；普通重渲染和同页签重复点击不是初始化。
2. 批量装载（首次打开、恢复会话、重新同步）只允许容器级揭示，不得给每一行挂延迟。
3. 增量插入只由新 key 触发；同 key 内容替换保留身份，不重播入场。
4. 状态变化在状态所属的最小元素上完成，不能把父视图重新做一遍进入。
5. dock 与左栏页签的可见性变化共用 `viewVisibilityIntent()`；返回 `settle` 时只更新内容，不创建新动画。

### 1.5 禁止动作

以下行为不得进入前端：

- 全屏持续旋转的通用 spinner 代替任务状态。
- 同一视口同时出现多个 shimmer、呼吸点或扫光。
- 用 `width`、`height`、`top`、`left`、`margin`、`padding` 做过渡动画。
- 用 3D 旋转、纸张卷曲、粒子、震动、音效表达普通状态变化。
- 每个 token 触发一次 DOM 重建或一次 CSS 入场动画。
- 无运行状态仍持续播放的循环动画。
- 以 reduced-motion 为由隐藏内容、跳过状态或禁用操作。

## 2. 加载态宪法

### 2.1 按等待时长选择形态

| 预计等待 | 允许形态 | 不允许形态 |
|---|---|---|
| `< 100ms` | 不显示加载指示器 | 闪现 spinner、骨架或 toast |
| `100ms–1s` | 原位状态文字、局部 activity、轻微 opacity 过渡 | 全屏遮罩、假骨架 |
| `1s–10s` | 结构已知用零 CLS 骨架；结构未知用状态文字或不确定进度 | 用灰块伪造未确认布局 |
| `> 10s` | 确定性进度、阶段说明、耗时估计、取消或后台入口 | 无数值、无出口的无限转圈 |

### 2.2 首延迟与状态机

所有加载态遵循同一状态机：

```text
idle
  -> pending       请求已发出，100ms 内不显示指示器
  -> active        100ms 后仍未完成，显示与结构匹配的状态
  -> settled       成功后用最终内容替换活动态
  -> error         失败后显示可恢复原因与动作
```

短于 100ms 的操作只产生最终状态，不播放“出现又消失”的 loader。

### 2.3 状态文案

加载态先说明正在做什么，再提供视觉活动：

| 场景 | 首选文案 |
|---|---|
| 建立连接 | `正在连接` |
| 恢复会话 | `正在恢复会话` |
| 切换会话 | `正在切换会话` |
| 读取历史 | `正在读取历史` |
| 等待模型 | `等待模型响应` |
| 模型生成 | `正在生成回复` |
| 工具运行 | `正在运行 <tool>` |
| 文件读取 | `正在读取 <filename>` |
| 持久化 | `正在保存` |
| 长任务 | `<阶段> · <已用时间>` |

不得只显示“加载中”而不指出对象或阶段。工具名、文件名和阶段名来自既有权威数据，不在前端猜测。

### 2.4 骨架屏

骨架只在结构已经由真实 DTO 或固定页面结构确认时使用：

1. 骨架项为 3–5 个。
2. 骨架与真实内容复用同一布局容器和尺寸约束。
3. 文本行高、头像、缩略图比例、表格列宽必须与最终内容一致。
4. 切换瞬间 CLS 预算为 0。
5. 骨架不是灰色卡片集合；禁止用通用灰块代替未确认布局。

### 2.5 无障碍

- 加载容器必须暴露 `aria-busy="true"` 或等价语义。
- 状态变化使用 `role="status"` 与克制的 `aria-live="polite"`。
- 焦点在加载期间保持可见；加载结束不抢走用户正在编辑的焦点。
- 动画关闭后，状态文字、进度和可操作入口仍完整存在。

## 3. 流式输出 cadence

### 3.1 不可见属性

流式输出的核心不可见属性不是透明度，而是**内容出现的节奏**。它确认系统正在持续产出，并维持用户对进度的感知。

- Purpose：`Feedback`
- Trigger：`state change`
- 状态变化：`waiting -> receiving`

### 3.2 刷新节奏

实现必须把来源 chunk 与渲染 tick 分开：

| 参数 | 基线 | 规则 |
|---|---:|---|
| `STREAM_TICK_MS` | 50ms | 持续流期间最多每 50ms 合并并提交一次 DOM 更新 |
| `STREAM_ENTER_HYSTERESIS_MS` | 100ms | 自动识别流态时需连续两个 tick 或显式 running 状态；短脉冲不切换“接收中”视觉模式 |
| `STREAM_EXIT_HYSTERESIS_MS` | 150ms | 连续 150ms 无新 chunk 才退出临时流态；显式 terminal 事件到达时立即刷新尾包并退出 |
| `STREAM_MAX_COALESCE_MS` | 50ms | 尾包不得因合并超过 50ms 才可见 |

规则：

1. 来源 chunk 先进入缓冲区，再按 tick 合并渲染。
2. 空白输入触发的第一个 chunk 应在下一帧可见，不等待完整 50ms 周期。
3. 高频流不得每个 chunk 都重建整条消息；只更新稳定 key 对应的文本节点或最小 DOM 片段。
4. terminal 事件必须先 flush 尾包，再切换到 done/failed/idle；不得丢字。
5. 自动滚动与文本 flush 共用一次布局批次，避免每个 token 都强制布局。
6. 用户接管滚动后不得持续抢回尾部；只有用户明确回到尾部时恢复跟随。
7. cadence 变化不能改变最终文本、消息 key、event revision 或持久化事实。

### 3.3 视觉表达

- 消息正文本身不做逐 token 淡入、缩放或位移。
- “接收中”只用局部状态文字、消息尾部活动标记或工具卡 shimmer 表达。
- 活动标记在 `STREAM_EXIT_HYSTERESIS_MS` 结束后消失；显式 terminal 事件可立即消失。
- 不使用彩色发光、模糊或每段文本渐变。
- reduced-motion 下保留 chunk 合并与最终文本更新，去掉游标闪烁和其他装饰。

## 4. 动效 token

### 4.1 时长

| Token | 值 | 用途 |
|---|---:|---|
| `--duration-micro` | 100ms | 按压与即时确认 |
| `--duration-fast` | 150ms | hover、focus、tooltip、退出 |
| `--duration-normal` | 250ms | 展开、弹层进入、视图进入 |
| `--duration-slow` | 400ms | 大范围面板或页面 transition |
| `--duration-slower` | 600ms | 只用于一次性页面揭示，当前默认不使用 |

组件不得写任意毫秒值。JS 中只有宪法明确命名的 cadence 常量可以存在于 `motion.js` 或专用流式调度模块，并由测试锁定。

### 4.2 缓动

| 语义 | Token | 规则 |
|---|---|---|
| 进入 | `--ease-out` | 界面进入、展开、内容出现 |
| 退出 | `--ease-in` | 界面关闭、内容退出 |
| 场景内移动 | `--ease-in-out` | 同一对象在场景内 reposition |
| 持续循环 | `--ease-linear` | 只用于 progress 与 running 文字 shimmer |

退出时长应短于对应进入时长，目标约为 75%；实现优先使用相邻 token，例如进入 250ms、退出 150ms。

### 4.3 弹簧物理

按钮、状态切换、弹层、抽屉和列表入场不得用“关键帧闪现”冒充有惯性的状态变化。
物理参数只在 `gui/frontend/dist/spring-motion.js` 的 `SPRING_PRESETS` 定义一次，组件
只按语义名引用；阻尼比按 `ζ = damping / (2 * √(stiffness * mass))` 计算。

| Preset | 产品语义 | `stiffness` | `damping` | `mass` | 时长预算 | `ζ` | 手感 |
|---|---|---:|---:|---:|---:|---:|---|
| `press` | 按压确认 | 1200 | 69.282 | 1 | 100ms | 1.00 | 临界阻尼，快速到位且不回弹 |
| `state` | 状态或提示切换 | 800 | 56.569 | 1 | 150ms | 1.00 | 临界阻尼，立即表达状态事实 |
| `enter` | 弹层、抽屉、视图进入 | 500 | 37 | 1 | 250ms | 0.83 | 轻欠阻尼，只允许克制过冲 |
| `exit` | 弹层、抽屉、视图退出 | 900 | 72 | 1 | 150ms | 1.20 | 过阻尼，不越过终值 |
| `settle` | 新列表行及局部内容落位 | 420 | 30 | 1 | 300ms | 0.73 | 轻微回弹后收敛，不拖慢阅读 |

规则：

1. `ζ < 1` 表示有回弹的欠阻尼，`ζ = 1` 表示最快且不回弹的临界阻尼，
   `ζ > 1` 表示缓慢收敛的过阻尼。普通状态反馈优先临界阻尼；进入可使用
   `0.65 < ζ < 0.85` 的轻欠阻尼；退出必须 `ζ >= 1`，不得在关闭时再弹回来。
2. CSS `linear()` 缓动由阶跃响应采样生成，最后一点必须钉到 `1 100%`，确保
   Web Animations 最终状态与业务状态一致。
3. 组件不得另写 `cubic-bezier()`、关键帧时长或第二套刚度/阻尼/质量数值。
   `durationMs` 是采样与完成边界，不是第二套独立 easing token。
4. `prefers-reduced-motion: reduce` 下直接提交终态；不得播放弹簧，也不得隐藏
   内容、状态文字、焦点或操作入口。
5. 弹簧只动画 `opacity` / `transform`；不得通过弹簧改变 `width`、`height`、
   `top`、`left`、`margin`、`padding` 或容器布局。

### 4.4 位移与编排

- 按压最多 1px 下沉；图标键 scale 不低于 0.94。
- 普通入场最多 6px；页面或视图入场最多 8px。
- 交错间隔 40ms；交错项 3–5 个；整个序列不超过 500ms。
- 同一动作只允许一个承接件：阴影、描边和背景不同时重复强调。
- 动画不得改变外壳、顶栏、左右栏和 composer 的 bounding rect。

### 4.5 属性白名单

普通动效只允许：

`opacity` / `transform` / `color` / `background-color` / `box-shadow`

唯一例外是 A09 文字 shimmer：字形蒙版层可以用 `background-position` 移动亮光，其它组件不得借这个例外动画背景。

`border-color` 新代码应作为静态颜色切换处理，不加入 transition 列表。现有 `border-color` transition 属于迁移债务，下一轮按受影响组件逐步移除。

### 4.6 空间与圆角

间距是布局责任，不是形状责任：

- 组件间距优先由父容器的 `gap`、`padding`、grid/flex 布局承担；禁止用 `margin-top`、`margin-right`、`margin-bottom` 或 `margin-left` 把元素“推到看起来对的位置”。
- **禁止“单侧厚外边距 + 圆角”同时出现**。圆角容器的任一侧外边距超过 `--sp-2`（8px），其对侧不得为 0；需要垂直节奏时使用对称的 `margin-block`，需要局部间隔时使用父容器 `gap`。
- 单侧 `border-left` / `border-right` 状态轨与圆角同样不应拼接成“括号形”；要么使用通栏方角，要么把状态信号放进容器内部。
- 该规则同时约束基础规则、响应式覆盖和组件内联 CSS，不以视觉截图例外。

## 5. Shimmer Effect

### 5.1 唯一用途

Shimmer 只用于工具调用处于 `running` 的加载动画，以及结构已知且仍需持续等待的局部加载条。它不得用于静止卡片、标题、头像、装饰背景或普通 hover。

### 5.2 语义

- Purpose：`Feedback`
- Trigger：`state change`
- 开始：工具状态从 queued/waiting 进入 running
- 停止：工具进入 success/error/cancelled，或视图卸载
- 作用对象：只有该工具条目自己的工具名字段；父工具域运行不得让展开区里的 completed/error/cancelled 子条目继承动画
- 表现：底层文字保留正常不透明色，独立字形蒙版层只提亮经过的笔画；不得出现矩形亮带，也不得遮住点击热区
- 静止元素：行高、列宽、相邻内容、任务列表

### 5.3 动效规格

- 周期：1.8s。
- 缓动：linear。
- 实现：文字节点始终保持正常、不透明的文本色；独立伪元素复制当前文字作为字形蒙版，以 `background-clip: text` 叠加两端透明的亮光，并通过 `background-position` 扫过。禁止把底层文字设为 `color: transparent`，也禁止用矩形背景、`width`、`height` 或 `top` 伪造扫光。
- 作用域：选择器必须钉在 `running` 工具自己的 `.tool-call-summary` 上；不能使用会命中工具域所有后代的宽选择器。
- 基线可见度：渐变峰值与底层对比可辨，但不能盖过状态文字。
- 同一工具摘要行同时最多一条 shimmer。
- 收束态行内只写工具类型；运行状态由可访问名称/`aria-busy` 与工具名文字 shimmer 表达，展开后显示完整状态。对话尾部只保留带文字 shimmer 的 `Thinking… / Loading… / Loading tool…`，不显示 Pico busy spinner 或活动线。

### 5.4 reduced-motion

`prefers-reduced-motion: reduce` 下移除文字 shimmer，保留工具类型、可访问状态、展开与取消能力。

## 6. 组件规则

| 组件 | 主交互语义 | 规则 |
|---|---|---|
| 顶栏连接状态 | 状态反馈 | 100ms 后显示“正在连接/已连接/连接失败”；不旋转整枚图标 |
| 左侧会话行 | 选择与反馈 | 选中由底色和文字层级表达；状态点不在静止行持续动画 |
| 对话消息 | 内容连续 | 首次插入可 250ms 入场；已有消息流式更新不重播入场 |
| 工具摘要行 | 运行反馈 | 收束态只显示工具类型；running 只让工具名字段 shimmer；完成/失败与命令、耗时在展开层可见 |
| Plan 节点 | 执行连续 | 不使用扫过整张卡片的 `top` 动画；running 活动限制在节点状态线或局部 activity 层 |
| composer | 即时反馈 | 提交后按钮状态和状态文案立即变化；不因 loading 改变输入框高度 |
| 消息队列 | 顺序连续 | 插入/重排应可中断；拖拽中使用直接手势反馈，不播放整屏位移 |
| 页签/视图 | 空间连续 | 250ms 进入、150ms 退出；外壳静止 |
| 弹窗 | 层级连续 | 淡入 + 8px translate；关闭时不用反向 3D 旋转 |
| 文件抽屉 | 空间连续 | 从侧边 8px 内进入；抽屉内容滚动不触发重播 |
| 文件树加载 | 局部活动 | 首 100ms 不显示；之后用行内活动线或 10px 状态件，不使用全面板 spinner |
| 对话导航刻度轨 | 定位与预览 | 一条刻度对应一条用户输入；指针跟随只通过邻近刻度的 `transform` 形成波浪，摘要卡第一层用主文本色显示问题，第二层用弱文本色显示该轮最后 LLM 开头；空回复如实留白 |
| toast/tooltip | 反馈 | 150ms 淡入；不阻塞焦点；reduced-motion 下立即显示 |
| 轨迹 | 请求/响应检查 | DevTools Network 式主从布局：左侧可选条目表，右侧固定详情头 + 入参/出参 JSON；选中变化不做行内展开 |

## 7. 代码声明与单一实现

### 7.1 CSS 声明

每处动效规则就近声明：

```css
/* seelex-motion:
   purpose=feedback
   trigger=state-change
   cast=tool-name-text; static=tool-card, rows
   choreography=simultaneous
   reduced-motion=static-status-text
*/
```

豁免必须使用：

```css
/* constitution-exempt: A09 reason=... owner=... expiry=... */
```

不可豁免条款没有豁免入口。

### 7.2 DOM 标记

允许用以下属性让测试和样式共享同一语义：

```html
data-motion-purpose="feedback"
data-motion-trigger="state-change"
data-motion-phase="running"
```

属性只描述表现语义，不得进入 Snapshot、Event 或持久化格式。

### 7.3 单一事实源

- 时长、缓动和 stagger 只在 `styles.css` 的 token 区定义。
- reduced-motion 判断只在 `motion.js` 的 `prefersReducedMotion()` 保留一份。
- 流式 cadence 常量只在专用流式调度模块保留一份。
- CSS 与 JS 不得各自定义一套“流式开始/结束”判据。
- 工具状态判定继续使用后端权威 status 与既有状态词表，不为动画新增第二套 status 映射。

## 8. Review 与门禁

### 8.1 必须通过

| # | 检查项 | Pass 条件 |
|---|---|---|
| 1 | Purpose | 每处动效属于 Feedback / Continuity / Delight 之一 |
| 2 | Trigger | 每处动效有白名单内唯一主触发源 |
| 3 | 语义表 | 行为能在第 1.3 节找到对应动作 ID |
| 4 | 静止元素 | 外壳和布局锚点不发生动画位移 |
| 5 | 属性 | 新动效只使用白名单属性 |
| 6 | 时长 | 使用 token；JS cadence 使用已命名常量 |
| 7 | 缓动 | enter=ease-out；exit=ease-in；loop=linear |
| 8 | reduced-motion | 立即到终态，内容与操作仍完整 |
| 9 | 加载形态 | 与等待时长和结构确定性匹配 |
| 10 | CLS | 骨架与最终内容占位一致 |
| 11 | Shimmer | 只用于局部持续活动，周期 1.8s |
| 12 | 流式 | 50ms 合并、100ms 进入滞回、150ms 退出滞回、terminal 先 flush |
| 13 | 可访问性 | 加载区 aria-busy；状态可读；焦点不受阻 |
| 14 | 单一实现 | 状态词表、reduced-motion 判据和 cadence 无重复 |
| 15 | 空间形状 | 圆角容器没有单侧厚 margin；间距由父布局承担 |

### 8.2 阻断条件

以下任一项失败直接阻断：

- reduced-motion 下内容不可见或操作不可完成。
- 流式 terminal 丢尾包。
- 动画引发真实 CLS。
- 工具运行态没有可读状态文字。
- 同一状态存在两套互相漂移的判据。
- 使用布局属性做动画。

## 9. 迁移纪律

现有实现与新宪法的差距按以下顺序收敛：

1. 先建立 token、语义声明和静态门禁。
2. 再替换工具 running shimmer 与对话尾部活动带。
3. 再实现流式 cadence 调度和滞回测试。
4. 再收敛弹窗、页签、列表与按压反馈。
5. 最后清理布局属性动画、硬编码时长和分散的 reduced-motion 覆盖。

每一批只迁移一个可解释能力。旧实现不得复制为第二套兼容路径；删除清单和针对性测试必须随批次提交。

## 附录 A：机器可读摘要

```yaml
constitution: seelex-frontend-v1
effective: 2026-10-28
precedence: [a11y, global-frontend-v1, seelex-frontend-v1, design-system, module-doc, prompt]

semantics:
  purpose: [feedback, continuity, delight]
  trigger: [load, viewport, hover, focus, click, scroll, state-change, route-change]
  default_delight: disabled

motion:
  duration_ms: {micro: 100, fast: 150, normal: 250, slow: 400, slower: 600}
  easing: {enter: ease-out, exit: ease-in, in_scene: ease-in-out, loop: linear}
  spring_source: gui/frontend/dist/spring-motion.js
  spring_presets:
    press: {stiffness: 1200, damping: 69.282, mass: 1, duration_ms: 100, zeta: 1.0}
    state: {stiffness: 800, damping: 56.569, mass: 1, duration_ms: 150, zeta: 1.0}
    enter: {stiffness: 500, damping: 37, mass: 1, duration_ms: 250, zeta: 0.83}
    exit: {stiffness: 900, damping: 72, mass: 1, duration_ms: 150, zeta: 1.2}
    settle: {stiffness: 420, damping: 30, mass: 1, duration_ms: 300, zeta: 0.73}
  spring_rule: {enter: light-underdamped, exit: overdamped-no-rebound, endpoint: pinned-to-1}
  stagger_ms: 40
  sequence_max_ms: 500
  max_enter_translate_px: 8
  properties: [opacity, transform, color, background-color, box-shadow]
  reduced_motion: snap-to-final-never-hide

loading:
  first_delay_ms: 100
  skeleton_items: [3, 5]
  skeleton_cls_budget: 0
  shimmer_s: 1.8
  shimmer_scope: [tool-running, known-structure-local-loading]

streaming:
  purpose: feedback
  trigger: state-change
  tick_ms: 50
  enter_hysteresis_ms: 100
  exit_hysteresis_ms: 150
  max_coalesce_ms: 50
  terminal: flush-tail-then-settle

tool_running:
  motion: shimmer
  period_s: 1.8
  property: transform
  reduced_motion: static-activity-line

spacing_shape:
  rounded_single_side_margin_max_px: 8
  rounded_single_side_margin_with_zero_opposite: forbidden
  preferred_spacing: [parent-gap, padding, symmetric-margin-block]
  single_side_status_rail_with_radius: forbidden
```
