# AI Agent 前端界面设计调研报告

> 调研日期：2026-07-18
> 调研范围：Codex CLI、Claude Code、Cursor、Windsurf 等主流 AI Agent 前端
> 附加专题：DSL 卡片渲染设计方案（含讲解/图表/跳转扩展）
> 设计风格：新拟物设计 (Neumorphism) × 低饱和度色彩体系

---

## 目录

1. [产品定位与概述](#1-产品定位与概述)
2. [布局架构](#2-布局架构)
3. [组件体系](#3-组件体系)
4. [设计令牌 / 设计系统](#4-设计令牌--设计系统)
5. [交互与动效](#5-交互与动效)
6. [数据流与状态管理](#6-数据流与状态管理)
7. [DSL 卡片渲染方案](#7-dsl-卡片渲染方案)
8. [可访问性与国际化](#8-可访问性与国际化)
9. [性能目标与低端机适配](#9-性能目标与低端机适配)
10. [技术栈选型](#10-技术栈选型)

---

## 1. 产品定位与概述

### 1.1 背景

AI 编码 Agent 正从「聊天辅助」进化为「深度协作开发环境」。当前主流形态分为三类：

| 形态 | 代表产品 | 技术路线 | 核心交互 |
|------|----------|----------|----------|
| **终端 TUI** | Codex CLI、Claude Code | Rust+ratatui / React+Ink | 键盘驱动的流式对话 + 工具卡片 |
| **IDE 插件** | Cursor、Windsurf | Electron + VS Code fork | 内嵌编辑器 + 侧边栏对话 |
| **桌面 GUI** | Codex macOS App | Native + App Server 协议 | 窗口化多面板 + 鼠标交互 |

### 1.2 设计哲学

| 原则 | 说明 |
|------|------|
| **新拟物质感** | 软阴影 + 微浮雕深度营造「嵌入感」，而非扁平或毛玻璃 |
| **低饱和度舒适** | 色系与 CLI 品牌一致但大幅降低饱和度，长时间使用不疲劳 |
| **心流优先** | 最小化上下文切换，AI 能力自然嵌入工作流而非打断 |
| **可审计透明** | 每个 Agent 动作有记录、可审查、可回滚 |
| **渐进式披露** | 先概览后详情的分层信息展示 |
| **双向协同** | 用户既可以审批 Agent 操作，也可以直接编辑结果 |
| **低配友好** | 零模糊/零毛玻璃，动效纯 CSS 实现，兼顾低性能设备 |

### 1.3 与竞品的差异化定位

| 维度 | Codex CLI | Claude Code | Cursor | Windsurf | **我们的目标** |
|------|-----------|-------------|--------|----------|---------------|
| 交互范式 | 终端 TUI | 终端 TUI | IDE 插件 | IDE 插件 | 桌面 GUI + SDK |
| 设计风格 | CLI 原始 | CLI 原始 | 扁平/毛玻璃 | 毛玻璃 | **新拟物 + 低饱和度** |
| 架构模式 | 单体 Rust | Node.js+Ink | Electron | Electron fork | **插件化 + App Server** |
| DSL 支持 | 无标准化 | 无标准化 | 无标准化 | 无标准化 | **A2UI 协议 + DSL Card** |
| 扩展性 | MCP | MCP | MCP | MCP | **MCP + 插件 SDK** |
| GPU 依赖 | 无 | 无 | 中（模糊/透明） | 中 | **零 GPU 动效** |

---

## 2. 布局架构

### 2.1 整体页面分区

采用 **三栏自适应布局**，以新拟物的软阴影分割区域而非硬边框：

```
┌──────────────────────────────────────────────────┐
│  Status Bar   [mode] [model] [● status] [CLI logo]│
├──────────┬────────────────────────┬──────────────┤
│          │   ┌──────────────────┐  │              │
│  L-Side   │   │  欢迎屏 / 对话流  │  │  R-Side     │
│  (nav/    │   │  (卡片流式渲染)   │  │  (面板)     │
│   explorer)│   └──────────────────┘  │              │
│          │                        │  - 工具调用链  │
│  ═══ 凹陷浮雕 ═══                 │  - 文件 Diff   │
│  - 会话列表                        │  - 卡片讲解     │
│  - 文件树                          │  - 图表全屏    │
│  - 插件面板                        │              │
│          │                        │              │
│          │   ┌──────────────────┐  │              │
│          │   │  Input Bar (凸起) │  │              │
│          │   │  > /command ...  │  │              │
│          │   └──────────────────┘  │              │
├──────────┴────────────────────────┴──────────────┤
│  Terminal / Output Panel (可折叠/可拆分)          │
│  └── 凸起面板，内部凹陷命令行区域                   │
└──────────────────────────────────────────────────┘
```

### 2.2 欢迎屏 / 启动界面

CLI 品牌语言在此集中体现，其余界面保持低饱和度克制：

```
┌────────────────────────────────────────────┐
│                                            │
│              ╔══════════════╗              │
│              ║  CLI LOGO    ║  ← 高饱和度  │
│              ║  (品牌标识)   ║     品牌色    │
│              ╚══════════════╝              │
│                                            │
│         Seelex — AI Coding Agent           │
│         ~ 让代码流动起来 ~                   │
│                                            │
│    ┌────────────────────────────────┐      │
│    │  最近会话                        │      │
│    │  ┌────┐ ┌────┐ ┌────┐          │      │
│    │  │项目A│ │项目B│ │项目C│ 新会话  │      │
│    │  └────┘ └────┘ └────┘ │       │      │
│    └────────────────────────────────┘      │
│                                            │
│    [快速开始]  [导入项目]  [文档]            │
│                                            │
└────────────────────────────────────────────┘
```

- **Logo**: 使用 CLI 的高饱和度/高对比度品牌色，是新拟物界面中唯一保留高饱和的区域
- **背景**: 新拟物基底色，软阴影版式
- **最近会话**: 凸起卡片，带新拟物招牌的双阴影
- **动画**: 纯 CSS 渐入 + 卡片交错入场（无 GPU 合成层）

### 2.3 布局模式

四种核心布局模板：

| 布局 | 场景 | 结构 |
|------|------|------|
| **Workspace** | 默认开发 | 左导航(凹陷) + 对话流(平) + 右面板(凸起) |
| **Console** | 日志/调试 | 全屏凸起终端面板，内嵌凹陷命令行 |
| **Settings** | 配置管理 | 左凹陷菜单 + 右凸起表单 |
| **Split** | 对比/多人 | 左右/上下凸起面板，中间凹陷分隔线 |

### 2.4 响应式断点

| 断点 | 宽度 | 行为 |
|------|------|------|
| `compact` | < 640px | 单列，侧栏自动隐藏 |
| `medium` | 640-1024px | 可显左栏，右栏悬浮 |
| `expanded` | 1024-1440px | 三栏满配 |
| `wide` | > 1440px | 最大宽度限制，居中内容 |

### 2.5 各区域功能定义

| 区域 | 新拟物质感 | 功能 | 交互要点 |
|------|-----------|------|----------|
| **Status Bar** | 凸起浮雕条 | 模型信息、运行状态、模式切换、CLI 品牌标识 | 点击切换，实时状态指示 |
| **L-Side** | 凹陷容器 | 会话列表、文件树、MCP 面板 | 拖拽排序，右键菜单 |
| **Main Content** | 平嵌底板 | Agent 消息流（卡片容器）、用户输入框 | 流式渲染、自动滚动、历史回溯 |
| **R-Side** | 凸起浮动面板 | 当前上下文详情：工具调用链、卡片讲解、图表 | 可折叠、双栏同步高亮 |
| **Terminal** | 凸起大面板内嵌凹陷终端 | 命令输出/终端模拟 | 支持分屏、搜索、ANSI 颜色 |
| **Input Bar** | 凸起 | 用户输入、命令、@提及 | 支持多行、"/" 命令菜单 |

---

## 3. 组件体系

### 3.1 分层结构（原子设计）

```
┌─────────────────────────────────────┐
│    页面级 / Page                    │  凸起底板
│  App, WorkspaceLayout, WelcomePage  │
├─────────────────────────────────────┤
│    组织级 / Organism                │  凸起/凹陷面板
│  ConversationStream, ToolPanel,     │
│  DiffViewer, TerminalPane           │
├─────────────────────────────────────┤
│    分子级 / Molecule                │  凸起卡片 + 浮雕细节
│  MessageCard, ToolCallCard,         │
│  ApprovalCard, DslCard,             │
│  InputBar, StatusBar                │
├─────────────────────────────────────┤
│    原子级 / Atom                    │  微凸起/纯文本
│  Button, Input, Badge, Spinner,     │
│  Icon, Text, Divider, Tag           │
└─────────────────────────────────────┘
```

### 3.2 新拟物组件质感分类

| 质感 | 视觉特征 | 适用组件 | CSS 实现 |
|------|----------|----------|----------|
| **凸起 (Pushed-out)** | 左上浅阴影 + 右下深阴影 | 卡片、按钮、面板、输入框 | `box-shadow: -4px -4px 8px var(--n-98), 4px 4px 8px var(--n-75)` |
| **凹陷 (Pushed-in)** | 左上深阴影 + 右下浅阴影 | 编辑器区、终端区、List container | `box-shadow: inset -3px -3px 6px var(--n-98), inset 3px 3px 6px var(--n-75)` |
| **悬浮 (Hover lift)** | 激活时阴影扩大/位移 | Hover 状态的卡片、按钮 | `box-shadow: -6px -6px 12px var(--n-98), 6px 6px 12px var(--n-75)` |
| **按下 (Pressed)** | 凸起→凹陷过渡 | 按钮点击态、选中态 | 切换为 inset shadow |
| **平嵌 (Flat inset)** | 极浅阴影或纯色 | 底板、背景区 | `background: var(--n-92)` |

### 3.3 核心组件目录

#### 消息流组件

| 组件 | 新拟物质感 | 状态 | 功能说明 |
|------|-----------|------|----------|
| `UserMessage` | 凸起卡片 | 发送中/已发送/失败 | 用户输入，可编辑重发 |
| `AgentMessage` | 平嵌块 | 生成中/完成/中断 | Markdown 渲染，含代码块(凹陷)高亮 |
| `ReasoningBlock` | 凹陷容器 | 折叠/展开 | Agent 推理过程（渐进披露） |
| `ToolCallCard` | 凸起小卡片 | 运行中/成功/失败 | Tool 调用卡片，含参数/结果 |
| `ToolCallChain` | 平嵌链式 | 展开/折叠 | 多个 ToolCall 的时序链条 |
| `ApprovalCard` | 高亮凸起卡片 | 待审批/已批准/已拒绝 | 文件变更/命令执行审批 |
| `DiffBlock` | 凹陷内嵌 | 行内/展开 | 结构化 diff 渲染，语法高亮 |
| `DslCard` | 凸起卡片 | 按 DSL 类型动态渲染 | DSL 驱动的通用卡片（见第 7 章） |

#### 工具卡片类型

| Tool Card | 质感 | 渲染内容 | 交互 |
|-----------|------|----------|------|
| `BashToolCard` | 凸起 + 内嵌终端(凹陷) | stdout 实时流 + exit code | 复制、展开/折叠 |
| `EditToolCard` | 凸起 | 文件 diff 预览 | approve/reject、查看上下文 |
| `ReadToolCard` | 凸起 | 文件内容预览 | 行号、语法高亮、跳转 |
| `SearchToolCard` | 凸起 | 搜索结果列表（含文件跳转） | 预览上下文、定位 |
| `PlanToolCard` | 凸起 | 结构化方案 | approve/reject、编辑 |
| `QuestionCard` | 凸起 | 选择题/MultiSelect | 单选/多选/文本输入（凹陷选项） |
| `SubAgentCard` | 凸起 | 子任务状态 | 展开详情、中止 |
| `MCPServerCard` | 凸起 | MCP 调用状态 | 重试、配置 |
| `ExplainCard` | 凸起 + 讲解气泡 | DSL 卡片讲解（见 7.6） | 文本高亮联动、翻页 |
| `ChartCard` | 凸起 | 内嵌图表（见 7.7） | 过滤/缩放/导出 |
| `LinkCard` | 凸起 | 网页/文件跳转（见 7.8/7.9） | 预览/跳转 |

### 3.4 新拟物按钮体系

| 类型 | 质感 | 用途 | 交互反馈 |
|------|------|------|----------|
| `Primary` | 凸起 + 品牌色 | 主要操作（发送、批准） | 按下→瞬间凹陷→弹起 |
| `Secondary` | 凸起 中性色 | 次要操作 | 同上，阴影幅度小 |
| `Ghost` | 无阴影纯文字 | 非强调操作 | hover 微凸起 |
| `Icon` | 凸起小圆 | 图标按钮 | 按下→凹陷 |
| `Danger` | 凸起 + 语义色 | 删除/拒绝 | 按下→凹陷 + 红色加深 |

### 3.5 组件的四种状态

```
┌────────┐    ┌────────┐    ┌────────┐    ┌────────┐
│ Empty  │ -> │Loading │ -> │  Done  │ -> │ Error  │
│ (空态) │    │ (加载) │    │ (完成) │    │ (错误) │
└────────┘    └────────┘    └────────┘    └────────┘
                   │                            │
                   └─── 重试 ────────────────────┘
```

- **空态(Empty)**: 凹陷占位区 + 引导操作（无转圈，用纯 CSS 呼吸脉冲）
- **加载态(Loading)**: 凸起骨架屏（Skeleton），带 CSS 渐变动画（非 GIF/JS 驱动）
- **完成态(Done)**: 正常内容渲染，凸起/平嵌按组件类型
- **错误态(Error)**: 错误信息 + 重试按钮（凸起高亮）

### 3.6 边缘态规范

| 场景 | 处理方式 |
|------|----------|
| 超长消息 (>100行) | 折叠 + "展开全文" 按钮（凹陷条） |
| 超长工具输出 | 截断 + "查看完整输出"（凸起按钮） |
| 流式中断 | 保留已接收内容 + "继续生成"按钮 |
| 网络离线 | 状态栏显示离线指示（凹陷 badge） |
| 大量并发工具 | 时序链折叠为摘要 + 展开查看详情 |

---

## 4. 设计令牌 / 设计系统

### 4.1 色彩体系（低饱和度 × 新拟物）

#### 核心设计思路

CLI 品牌色保留其 **色相（Hue）**，但大幅降低 **饱和度（Saturation）** 和调整 **明度（Lightness）**，使色彩在界面上呈现「低饱和度莫兰迪风」，既保留品牌识别度又长时间观看舒适。

```
CLI 品牌色源   →  低饱和度映射
#FF6B35 (高饱)  →  #D4926A (降饱和 60%)
#00D4AA (鲜绿)  →  #7BB7A7 (降饱和 70%)
#7C3AED (亮紫)  →  #9B87B8 (降饱和 65%)
```

#### 中性色板（Neutral — 新拟物基底）

新拟物的灵魂在于中性色板——底色既不是纯白也不是纯黑，而是带有轻微暖/冷倾向的中灰色，让双阴影有自然的「光照感」。

| 阶值 | Light (暖灰) | Dark (冷灰) | 用途 |
|------|-------------|-------------|------|
| 0 | — | `#141416` | 最深背景 (dark) |
| 5 | — | `#1A1A1E` | 页面背景 (dark) |
| 10 | — | `#212125` | 卡片/面板背景 (dark) |
| 15 | — | `#28282D` | 悬浮背景 (dark) |
| 20 | — | `#303036` | 边框/分割线 (dark) |
| 25 | — | `#38383E` | 次级表面 (dark) |
| 40 | — | `#585860` | 次要文字 (dark) |
| 60 | — | `#888894` | 占位/禁用 (dark) |
| — | — | — | — |
| 80 | `#C8C8C8` | — | 次要文字 (light) |
| 85 | `#D6D6D6` | — | 边框/分割线 (light) |
| 90 | `#E5E5E5` | — | 悬浮背景 (light) |
| 92 | `#EBEBEB` | — | 页面底板 (light) |
| 95 | `#F0F0F0` | — | 卡片/面板背景 (light) |
| 98 | `#F8F8F6` | — | 页面背景 (light，微暖) |

> 注意没有纯白 (`#FFFFFF`) 或纯黑 (`#000000`)——新拟物依赖「非极端值」来产生阴影层次。

#### 语义色板（低饱和度 × 6 色系）

每个色系仅保留 **3 个关键阶值**（深/中/浅），而非 18 阶，旨在降低决策负担。

| 色板 | 变量前缀 | 阶 30 (深) | 阶 50 (主色) | 阶 90 (浅) | 角色 |
|------|---------|-----------|-------------|-----------|------|
| Brand (CLI 衍生) | `--p-` | `#7A6B5E` | `#C4A88C` | `#F0E6DC` | 品牌色、CTA、链接 |
| Secondary | `--s-` | `#6B6B8A` | `#9B97B8` | `#E4E2EE` | 次要强调 |
| Success | `--su-` | `#4A7A6A` | `#7BB7A7` | `#D4EBE4` | 成功状态 |
| Warning | `--wa-` | `#8A7A50` | `#C4B080` | `#EDE6D0` | 警告 |
| Error | `--e-` | `#8A504A` | `#C4807A` | `#EDD8D6` | 错误 |
| Info | `--i-` | `#5A6A8A` | `#8A9FBF` | `#D8E2F0` | 信息 |

#### Token 用途映射（含新拟物阴影色）

| CSS Token | Light 值 | Dark 值 | 用途 |
|-----------|---------|---------|------|
| `--bg-canvas` | `--n-98` | `--n-5` | 页面最底层背景 |
| `--bg-surface` | `--n-95` | `--n-10` | 卡片/面板表面 |
| `--bg-elevated` | `--n-98` | `--n-15` | 凸起元素表面 |
| `--bg-inset` | `--n-92` | `--n-15` | 凹陷元素背景 |
| `--text-primary` | `--n-20` | `--n-90` | 正文 |
| `--text-secondary` | `--n-60` | `--n-60` | 次要文字 |
| `--text-disabled` | `--n-80` | `--n-40` | 禁用文字 |
| `--border-subtle` | `--n-85` | `--n-20` | 极弱分割 |
| `--shadow-light` | `#FFFFFF` | `#2A2A30` | 新拟物浅阴影（左上光源） |
| `--shadow-dark` | `#C0C0C0` | `#0A0A0E` | 新拟物深阴影（右下） |
| **→ 品牌区 ↑** | | | |
| `--brand-logo` | CLI 原始色 | CLI 原始色 | **仅 Logo/欢迎屏使用，高饱和度** |

#### 颜色使用铁律

| 规则 | 说明 |
|------|------|
| **Logo 区例外** | 欢迎屏/启动界面/状态栏品牌图标使用 CLI 原始高饱和度——这是唯一「高饱和孤岛」 |
| **语义色降饱和** | 所有语义色（成功/警告/错误/信息）饱和度 ≤ 35%，避免刺眼 |
| **纯黑禁用** | 最深色用 `#141416` 而非 `#000000`；最浅用 `#F8F8F6` 而非 `#FFFFFF` |
| **不透明优先** | 全界面避免 `rgba()` 透明叠加——降低合成层压力 |
| **色相一致性** | 亮暗模式切换只变明度不变色相（Brand 保持暖调，Success 保持冷绿调） |

### 4.2 新拟物阴影系统

新拟物的核心是 **双阴影**——一个浅色方向光阴影（左上）和一个深色阴影（右下）：

```css
/* 凸起（卡片/按钮默认态） */
.neumorphic-raised {
  background: var(--bg-surface);
  box-shadow:
    -4px -4px 8px var(--shadow-light),   /* 左上来光 */
    4px 4px 8px var(--shadow-dark);       /* 右下背光 */
}

/* 凹陷（输入区/终端/列表容器） */
.neumorphic-pressed {
  background: var(--bg-inset);
  box-shadow:
    inset -3px -3px 6px var(--shadow-light),
    inset 3px 3px 6px var(--shadow-dark);
}

/* 悬浮抬起 */
.neumorphic-hover {
  box-shadow:
    -6px -6px 12px var(--shadow-light),
    6px 6px 12px var(--shadow-dark);
}

/* 点击按下（瞬间过渡） */
.neumorphic-active {
  box-shadow:
    inset -2px -2px 4px var(--shadow-light),
    inset 2px 2px 4px var(--shadow-dark);
}
```

#### 阴影层级表

| 层级 | 凸起偏移 | 凹陷偏移 | 模糊半径 | 用途 |
|------|---------|---------|----------|------|
| xs | ±2px | ±1.5px | 4px | 小按钮、badge、icon |
| sm | ±3px | ±2px | 6px | 小卡片、输入框 |
| md | ±4px | ±3px | 8px | **默认卡片、面板** |
| lg | ±5px | ±4px | 12px | 悬浮态、弹窗 |
| xl | ±6px | — | 16px | 模态框、浮动面板 |

> **关键性能决策**: 所有阴影使用 `box-shadow`（纯 CPU 栅格化），**禁止**使用 `filter: drop-shadow()` 或 `backdrop-filter: blur()`——后者触发 GPU 合成层，在低端机上掉帧。

### 4.3 排版规范

| Token | 值 | 用途 |
|-------|-----|------|
| `--font-sans` | system-ui, -apple-system, sans-serif | 界面文本 |
| `--font-mono` | JetBrains Mono / Fira Code / monospace | 代码、终端输出 |
| `--font-size-xs` | 11px | 状态栏、辅助信息 |
| `--font-size-sm` | 12px | 次要文字、时间戳 |
| `--font-size-base` | 13px | 正文、消息 |
| `--font-size-lg` | 15px | 卡片标题 |
| `--font-size-xl` | 18px | 对话标题、大标题 |
| `--font-size-2xl` | 24px | 页面标题 |
| `--line-height-tight` | 1.3 | 标题 |
| `--line-height-normal` | 1.5 | 正文 |
| `--line-height-relaxed` | 1.75 | 长文阅读 |
| `--font-weight-regular` | 400 | 正文 |
| `--font-weight-medium` | 500 | 强调 |
| `--font-weight-semibold` | 600 | 子标题 |
| `--font-weight-bold` | 700 | 主标题 |

### 4.4 间距系统（8pt 网格 + 4pt 微调）

| Token | 值 | 用途 |
|-------|-----|------|
| `--space-0.5` | 2px | 微型间距（新拟物阴影留白） |
| `--space-1` | 4px | 图标间距、inline 空隙 |
| `--space-2` | 8px | 紧凑内边距、小卡片间距 |
| `--space-3` | 12px | 卡片内边距 |
| `--space-4` | 16px | 卡片内边距、段落间距 |
| `--space-5` | 20px | 区块间距 |
| `--space-6` | 24px | 大区块间距 |
| `--space-8` | 32px | 节/区域间距 |
| `--space-10` | 40px | 页面级间距 |
| `--space-12` | 48px | 页面级大间距 |

### 4.5 圆角规范

新拟物圆角应「柔和但不过度」——过大的圆角会破坏阴影的光照感：

| Token | 值 | 用途 |
|-------|-----|------|
| `--radius-none` | 0 | 工具卡片内嵌块 |
| `--radius-sm` | 4px | 小按钮、标签 |
| `--radius-md` | 6px | **默认卡片、输入框、面板** |
| `--radius-lg` | 8px | 大卡片、对话框 |
| `--radius-xl` | 12px | 模态框 |
| `--radius-full` | 9999px | Badge、Tag、圆形图标按钮 |

### 4.6 图标体系

| 来源 | 说明 | 新拟物适配 |
|------|------|-----------|
| **Lucide Icons** | 主图标库，2px 描边风格 | 配合凸起/凹陷使用描边图标而非填充 |
| **CLI 品牌图标** | 仅用于欢迎屏/状态栏 | 保留原始高饱和色 |
| **Code icons** | 编程语言/框架图标 | 降饱和后使用 |
| **Status badges** | 自定义状态指示 | 小凸起圆点 + CSS 呼吸动画 |

---

## 5. 交互与动效

### 5.1 动效设计原则

| 原则 | 说明 |
|------|------|
| **纯 CSS 优先** | 所有动效使用 CSS `transition` 和 `@keyframes`，避免 JS 动画库 |
| **零 GPU 合成** | 不使用 `filter`, `backdrop-filter`, `will-change: transform` 触发合成层 |
| **仅操作 `opacity` 和 `box-shadow`** | 这两个属性只触发重绘不触发布局——最高效 |
| **尊重 `prefers-reduced-motion`** | 系统设置减少动效时，保留必要过渡但移除装饰性动画 |
| **60fps 保障** | 动画帧率低于 30fps 时自动降级为瞬间切换 |

### 5.2 新拟物质感动效

新拟物最有特色的交互反馈是 **按压/弹起** 的物理感：

```css
/* 按钮按压过渡 */
.neumorphic-btn {
  transition: box-shadow 0.15s ease, transform 0.15s ease;
  box-shadow: -4px -4px 8px var(--shadow-light), 4px 4px 8px var(--shadow-dark);
}

.neumorphic-btn:hover {
  box-shadow: -5px -5px 10px var(--shadow-light), 5px 5px 10px var(--shadow-dark);
}

.neumorphic-btn:active {
  transform: scale(0.97);  /* 微缩小 → 模拟物理按入 */
  box-shadow:
    inset -2px -2px 4px var(--shadow-light),
    inset 2px 2px 4px var(--shadow-dark);
}

/* 卡片悬浮上浮 */
.neumorphic-card {
  transition: box-shadow 0.2s ease, transform 0.2s ease;
}

.neumorphic-card:hover {
  transform: translateY(-1px);
  box-shadow: -6px -6px 12px var(--shadow-light), 6px 6px 12px var(--shadow-dark);
}
```

### 5.3 页面转场与入场

| 场景 | 实现方式 | 时长 | 说明 |
|------|---------|------|------|
| 面板展开/折叠 | CSS `max-height` + `opacity` transition | 200ms | 无 layout 抖动 |
| 标签页切换 | CSS `opacity` + `transform: translateX` | 150ms | 非 active 面板 `opacity: 0; pointer-events: none` |
| 模态弹窗 | CSS scale + opacity | 200ms | `transform: scale(0.97) → scale(1)` |
| 欢迎屏入场 | staggered fade-in | 每项 100ms 交错 | 卡片、标题、按钮依次淡入 |
| 状态栏更新 | `opacity` fade | 100ms | |

### 5.4 消息流动效

| 场景 | 实现 | 说明 |
|------|------|------|
| 新消息插入 | `opacity: 0→1` + `translateY: 8px→0` | 保持 scroll anchor |
| 流式内容追加 | 光标脉冲 `@keyframes blink` | 纯 CSS `border-color` 交替 |
| 消息过渡 | `max-height` 动画 | 防布局抖动 |
| 卡片状态变更 | 微阴影变色 | 成功→阴影带绿调，失败→带红调，0.3s 过渡 |

### 5.5 状态指示动画（低性能友好）

所有「正在运行」的指示器使用纯 CSS，不含 JS 定时器：

```css
/* 呼吸脉冲 — 用于 ToolCall 运行态 */
@keyframes breathe {
  0%, 100% { box-shadow: -3px -3px 6px var(--shadow-light), 3px 3px 6px var(--shadow-dark); }
  50%     { box-shadow: -3px -3px 6px var(--shadow-light), 3px 3px 6px var(--p-90); }
}

/* 加载骨架屏 — 渐变动画 */
@keyframes shimmer {
  0%   { background-position: -200% 0; }
  100% { background-position: 200% 0; }
}
.skeleton {
  background: linear-gradient(90deg, var(--n-90) 25%, var(--n-95) 50%, var(--n-90) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s ease-in-out infinite;
}
```

### 5.6 键盘导航

| 快捷键 | 功能 |
|--------|------|
| `Enter` | 提交输入 |
| `Shift+Enter` | 换行 |
| `Tab` | 焦点在面板间切换（可见凸起/凹陷焦点环） |
| `↑/↓` | 消息历史滚动 |
| `PgUp/PgDn` | 翻页 |
| `Ctrl+C` | 中断当前 Agent 操作 |
| `Ctrl+D` | 退出/关闭面板 |
| `/` | 命令菜单（凸起弹出） |
| `Ctrl+P` | 快速打开文件（凹陷输入框弹出） |
| `Ctrl+R` | 重新生成 |
| `Ctrl+Z` | 撤销上一步 Agent 操作 |
| `Ctrl+Click` | 文件跳转（DSL 卡片内） |
| `Escape` | 取消/关闭当前弹窗 |

### 5.7 鼠标交互

| 交互 | 新拟物反馈 | 行为 |
|------|-----------|------|
| 悬停 | 阴影扩大 + 微抬起 | 显示操作按钮 |
| 点击 | 凸起→凹陷→弹起 | 执行操作 |
| 双击 | — | 编辑模式（消息/卡片） |
| 右键 | — | 上下文菜单（凸起浮动） |
| 拖拽 | 卡片跟随 + 原位置凹陷占位 | 调整面板顺序 |
| Ctrl+悬停 | 高亮边框动画 | 文件链接预览 |

### 5.8 异步操作反馈

| 场景 | 用户感知 | 实现 |
|------|----------|------|
| Agent 思考 | 思考块 + 呼吸脉冲 | CSS `@keyframes breathe` + 状态文本 |
| Tool 调用中 | ToolCallCard 凸起 + 呼吸 | box-shadow 动画 |
| 审批等待 | ApprovalCard 高亮凸起 + 输入区缩小(凹陷) | 阴影置换 |
| 操作成功 | 阴影微妙绿调 + 图标过渡 | CSS transition 0.3s |
| 操作失败 | 阴影微红调 + 错误信息 | CSS transition + 凹陷错误块 |

---

## 6. 数据流与状态管理

### 6.1 整体数据架构

```
┌──────────┐     JSON-RPC     ┌──────────────┐
│  Frontend │ ◄─────────────► │  Agent Core  │
│  (UI)    │   (stdio/SSE)   │  (Backend)   │
└──────────┘                  └──────────────┘
```

**协议核心原语**：

| 原语 | 说明 | 生命周期 |
|------|------|----------|
| `Item` | 原子 I/O 单元（消息/Tool/审批/差异/DSL 卡片） | `started` → `*delta`(可选) → `completed` |
| `Turn` | 单次用户输入触发的 Agent 工作单元 | 包含一组 Item 序列 |
| `Thread` | 持久化会话容器 | 可创建/恢复/分支/归档 |

**事件流**：

```
User Input → Turn/started → Item/started → Item/delta → Item/completed
                                                              │
                                                     ┌────────┴────────┐
                                                     │ 无审批需求: 继续 │
                                                     │ 有审批需求: 暂停 │
                                                     └────────┬────────┘
                                                              │
                                                    Approval Event → 继续/中止
```

### 6.2 前端状态管理方案

| 层级 | 方案 | 说明 |
|------|------|------|
| 全局状态 | Zustand | 轻量、TypeScript 友好、支持 middleware |
| 服务端状态 | TanStack Query | 缓存/重试/乐观更新/SSE 订阅 |
| 本地 UI 状态 | React useState/useReducer | 组件内临时状态（如折叠/展开） |
| 路由/URL 状态 | React Router | 持久化到 URL 参数 |
| Form 状态 | React Hook Form + Zod | 表单验证与交互 |

#### 状态分层图

```
┌─────────────────────────────────────────┐
│          Zustand Store (Global)          │
│  session: Thread[], currentThread: ID   │
│  settings: Theme, Layout, Mode          │
│  dslRegistry: CardType[]                │
└─────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────┐
│       TanStack Query (Server Cache)     │
│  useQuery('thread', id) → items[]      │
│  useMutation('sendMessage') → turn     │
└─────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────┐
│     React State (Local Components)      │
│  isExpanded, inputValue, selectedTab   │
│  cardFocusId, annotationPage           │
└─────────────────────────────────────────┘
```

### 6.3 与后端 API 的通信模式

| 模式 | 用途 | 技术 |
|------|------|------|
| JSON-RPC 请求/响应 | 创建会话、查询历史 | HTTP POST |
| SSE 流式响应 | Agent 消息流、Tool 输出 | EventSource |
| WebSocket | 实时状态更新、多客户端同步 | ws |
| JSONL over stdio | TUI 本地进程通信 | child_process stdio |

#### SSE 消息格式（DSL 卡片扩展后）

```json
// Item 开始
{"type": "item/started", "id": "item_123", "kind": "dsl_card"}

// DSL 卡片渲染指令
{"type": "item/delta", "id": "item_123",
 "dsl": {
   "cmd": "updateComponents",
   "surface": "main",
   "components": { ... }
 }}

// DSL 数据更新
{"type": "item/delta", "id": "item_123",
 "dsl": {
   "cmd": "updateDataModel",
   "path": "/chart/data",
   "value": [...]   // 图表数据流式追加
 }}

// 卡片讲解焦点同步
{"type": "item/delta", "id": "item_123",
 "dsl": {
   "cmd": "explainHighlight",
   "componentId": "diff_block_1",
   "highlightRange": {"start": 10, "end": 20},
   "annotation": "此处修改了数据库连接字符串"
 }}

// Item 完成
{"type": "item/completed", "id": "item_123"}
```

### 6.4 缓存策略

| 数据类型 | 策略 | TTL | 说明 |
|----------|------|-----|------|
| 会话历史 | 持久化 (IndexedDB) | 永久 | 支持离线查看和历史搜索 |
| 文件内容 | LRU 缓存 | 5 min | 避免重复读取 |
| 搜索结果 | 内存缓存 | 30s | 快速导航 |
| 用户偏好 | localStorage | 永久 | 主题、布局、快捷键 |
| API 响应 | TanStack Query cache | 按需失效 | 保证数据新鲜度 |
| 图表数据 | 内存缓存（最新 3 份） | turn 结束 | 避免重复 SSE 数据重解析 |

### 6.5 离线支持

| 功能 | 实现 |
|------|------|
| 离线阅读历史 | IndexedDB 存储最后 N 条会话 |
| 离线编辑 | 本地暂存，上线后同步 |
| 网络状态指示 | 状态栏凹陷 badge 显示连接状态 |
| 断线重连 | 指数退避 WebSocket 重连 |

---

## 7. DSL 卡片渲染方案

### 7.1 什么是 DSL 卡片渲染

DSL（Domain-Specific Language）卡片渲染是一种 **声明式 UI 生成模式**：后端/Agent 以结构化 DSL（JSON/JSONL）描述 UI 结构，前端运行时将其渲染为原生交互卡片，无需前端硬编码每种卡片类型。

**本项目扩展 A2UI v0.9 协议**，加入 4 个 Agent 高频需要的卡片能力：
1. **卡片讲解（Explain）** — Agent 对卡片内容逐段解释，高亮联动
2. **嵌入图表（Chart）** — 数据可视化卡片
3. **网页跳转（WebLink）** — 从 Agent 结果直接跳转外部 URL
4. **文件跳转（FileLink）** — 从 Agent 结果直接定位项目文件+行号

### 7.2 核心架构

```
┌──────────────────────────────────────────────────┐
│                Agent / Backend                    │
│  Generates: Item + DSL payload (JSON schema)    │
└────────────────────┬─────────────────────────────┘
                     │ SSE / WebSocket / stdio
┌────────────────────▼─────────────────────────────┐
│              DSL Render Engine                    │
│                                                    │
│  1. Stream Reader  ─→  2. Message Parser          │
│                              │                     │
│  3. Schema Validator ─→  4. Card Registry         │
│                              │                     │
│  ┌────────────────────────────┐                   │
│  │  5. Card Layout Composer   │ ← ExplainEngine   │
│  │     ChartEngine            │     LinkEngine    │
│  └────────────────────────────┘                   │
│           │                                       │
│  6. Native UI Renderer (Neumorphic themed)       │
└──────────────────────────────────────────────────┘
```

### 7.3 DSL 规范设计

#### 核心命令

| 命令 | 说明 | 扩展来源 |
|------|------|----------|
| `createSurface` | 创建 UI 容器（独立组件树 + 数据模型） | A2UI v0.9 |
| `updateComponents` | 定义/更新 UI 组件（扁平邻接表模型） | A2UI v0.9 |
| `updateDataModel` | 更新应用状态（JSON Pointer Path） | A2UI v0.9 |
| `deleteSurface` | 移除 UI 容器 | A2UI v0.9 |
| **`explainStart`** | **开始卡片讲解模式，指定目标组件** | **本项目扩展** |
| **`explainHighlight`** | **高亮讲解段落，标注文本/代码范围** | **本项目扩展** |
| **`chartAppendData`** | **向图表追加数据流** | **本项目扩展** |
| **`navigateRequest`** | **请求前端执行页面/文件跳转** | **本项目扩展** |

#### DSL 消息完整示例（含扩展能力）

```json
{
  "cmd": "updateComponents",
  "surface": "main",
  "components": {
    "explain_card": {
      "type": "ExplainCard",
      "props": {
        "title": "代码变更讲解",
        "targetComponent": "diff_block_1",
        "steps": [
          { "id": "step_1", "label": "连接字符串",
            "highlight": { "lines": "10-20" },
            "content": "此处修改了数据库连接，从硬编码转为环境变量" },
          { "id": "step_2", "label": "错误处理",
            "highlight": { "lines": "25-35" },
            "content": "新增了重试逻辑，最多 3 次指数退避重试" }
        ]
      },
      "children": ["diff_block_1", "chart_1", "links_1"]
    },
    "diff_block_1": {
      "type": "DiffBlock",
      "props": {
        "diff": "--- a/config.ts\n+++ b/config.ts\n@@ -10,15 +10,22 @@",
        "language": "typescript"
      }
    },
    "chart_1": {
      "type": "Chart",
      "props": {
        "title": "性能对比",
        "chartType": "bar",
        "data": { "$bind": "/chart/performance" },
        "dimensions": ["修改前", "修改后"],
        "metrics": ["响应时间(ms)", "吞吐量(req/s)"]
      }
    },
    "links_1": {
      "type": "LinkGroup",
      "props": {
        "style": "horizontal"
      },
      "children": ["link_web", "link_file"]
    },
    "link_web": {
      "type": "WebLink",
      "props": {
        "label": "查看 API 文档",
        "url": "https://docs.example.com/api",
        "icon": "external-link"
      }
    },
    "link_file": {
      "type": "FileLink",
      "props": {
        "label": "跳转到 config.ts",
        "filePath": "/src/config.ts",
        "line": 42,
        "column": 10
      }
    }
  }
}
```

### 7.4 数据绑定系统（不变）

```json
// 1. 绑定式（动态）
{"content": { "$bind": "/chart/performance" }}

// 2. 字面式（静态）
{"content": "Hello, World"}

// 3. 双向绑定
{"value": { "$bind": "/form/name" },
 "onChange": { "$action": "updateDataModel", "path": "/form/name" }}

// 4. ⭐ 图表数据流绑定
{"data": { "$bind": "/chart/live_data", "mode": "append" }}
```

### 7.5 卡片注册表设计（扩展后）

| 字段 | 说明 | 示例 |
|------|------|------|
| `type` | 组件类型标识 | `ExplainCard`, `Chart`, `WebLink` |
| `schema` | prop 验证 schema | JSON Schema (Zod 导出) |
| `component` | React 组件引用 | `<ExplainCard>` |
| `defaults` | 默认 props | `{ collapsible: true }` |
| `states` | 支持的状态列表 | `['loading', 'ready', 'error']` |
| `engine` | 可选渲染引擎 | `chartEngine`, `explainEngine` |

#### 完整卡片注册表

```typescript
const CARD_REGISTRY = {
  /** 容器类 */
  Card:      { component: Card,      schema: CardSchema,
               defaults: { variant: 'neumorphic-raised' } },
  Section:   { component: Section,   schema: SectionSchema },
  Stack:     { component: Stack,     schema: StackSchema },

  /** 内容类 */
  Text:      { component: Text,      schema: TextSchema },
  Code:      { component: CodeBlock, schema: CodeSchema },
  Markdown:  { component: Markdown,  schema: MarkdownSchema },
  Image:     { component: Image,     schema: ImageSchema },

  /** 交互类 */
  Actions:   { component: Actions,   schema: ActionsSchema },
  Input:     { component: TextInput, schema: InputSchema },
  Select:    { component: SelectCard,schema: SelectSchema },
  Button:    { component: Button,    schema: ButtonSchema },

  /** 工具类 */
  DiffBlock: { component: DiffBlock, schema: DiffSchema },
  ToolCall:  { component: ToolCallCard, schema: ToolCallSchema },
  Approval:  { component: ApprovalCard, schema: ApprovalSchema },
  SearchResults: { component: SearchResults, schema: SearchSchema },

  /** 数据类 */
  Table:     { component: DataTable, schema: TableSchema },
  List:      { component: DataList,  schema: ListSchema },

  /** ⭐ 本项目扩展卡片 */
  ExplainCard: { component: ExplainCard, schema: ExplainCardSchema,
                 engine: 'explainEngine',
                 description: '卡片讲解：Agent 分步解说代码变更/分析结果' },
  Chart:       { component: Chart,       schema: ChartSchema,
                 engine: 'chartEngine',
                 description: '嵌入图表：柱状图/折线图/饼图/热力图' },
  WebLink:     { component: WebLink,     schema: WebLinkSchema,
                 engine: 'linkEngine',
                 description: '网页跳转：外部 URL 打开/预览' },
  FileLink:    { component: FileLink,    schema: FileLinkSchema,
                 engine: 'linkEngine',
                 description: '文件跳转：项目文件定位到行' },
  LinkGroup:   { component: LinkGroup,   schema: LinkGroupSchema,
                 description: '跳转链接组：水平/垂直排列多个 Link' },

  /** 状态类 */
  Loading:   { component: LoadingCard, schema: LoadingSchema },
  Error:     { component: ErrorCard,   schema: ErrorSchema },
  Empty:     { component: EmptyCard,   schema: EmptySchema },
};
```

### 7.6 ⭐ 卡片讲解能力（ExplainCard）

#### 设计目标

Agent 在生成复杂回复（如代码变更、架构分析、Bug 排查）时，需要像「导师」一样逐段讲解。ExplainCard 将讲解步骤与目标卡片联动，实现「讲到哪里，亮到哪里」。

#### 交互流程

```
┌──────────────────────────────────────────────┐
│  代码变更讲解                   [步骤 1/3]    │  ← ExplainCard 头部
├──────────────────────────────────────────────┤
│                                              │
│  步骤条 (凸起)                               │
│  ● 连接字符串   ○ 错误处理   ○ 性能影响      │
│                                              │
├──────────────────────────────────────────────┤
│  讲解内容 (凸起)                              │
│  "此处修改了数据库连接，从硬编码转为环境变量。  │
│   主要改动有 3 点：                           │
│   1. 新增 ConfigService 注入                 │
│   2. 使用 process.env.DB_URL 替代字面量       │
│   3. 连接失败时有降级策略"                    │
├──────────────────────────────────────────────┤
│                                              │
│  ┌──────────────────────────────────────┐    │
│  │  @@ -10,15 +10,22 @@               │    │  ← 被讲解的目标组件
│  │  |10| -  const db = mysql({        │    │     (高亮行带黄色底)
│  │  |10| +  const db = await getDb() │    │
│  │  |15| -  host: 'localhost',        │    │     [行 10-20 高亮]
│  │  |15| +  host: process.env.DB_HO  │    │
│  └──────────────────────────────────────┘    │
│                                              │
└──────────────────────────────────────────────┘
```

#### ExplainCard Schema

```typescript
interface ExplainCardProps {
  /** 标题 */
  title: string;
  /** 被讲解的目标组件 ID（必须是当前 surface 内的组件） */
  targetComponent: string;
  /** 讲解步骤列表 */
  steps: ExplainStep[];
  /** 当前步骤索引（响应式绑定） */
  currentStep?: { $bind: string };
}

interface ExplainStep {
  id: string;
  label: string;           // 步骤标签（短文本）
  highlight: {              // 高亮范围
    lines?: string;         // "10-20" 或 "10" 或 "10,15,20-25"
    tokens?: string[];      // 可选：特定 token 高亮
  };
  content: string;          // 讲解正文（支持 Markdown）
  annotation?: string;      // 额外注解（显示在侧边）
}
```

#### ExplainEngine 实现要点

| 功能 | 实现 |
|------|------|
| 高亮联动 | 点击步骤 → `explainHighlight` 事件 → 目标组件 `scrollIntoView` + CSS class 切换 |
| 步骤导航 | 步骤条 (`<`) step 1/3 (`>`) 按钮 + 键盘 `←` `→` |
| 自动滚动 | 目标组件自动滚到可见区域，缓动 `scroll-behavior: smooth` |
| 多卡片并行 | 每个 ExplainCard 独立状态，不同 surface 互不干扰 |

### 7.7 ⭐ 嵌入图表能力（ChartCard）

#### 设计目标

Agent 的分析结果（性能数据、代码统计、依赖图）需要以图表形式直观呈现。ChartCard 支持**流式数据追加**，Agent 可以边分析边推送数据。

#### 支持的图表类型

| 类型 | `chartType` | 适用场景 | 性能策略 |
|------|-----------|----------|----------|
| 柱状图 | `bar` | 分类对比（如各模块耗时） | Canvas 2D (非 SVG，布局少) |
| 折线图 | `line` | 趋势数据（如请求延迟时序） | Canvas 2D |
| 饼图 | `pie` | 占比分析（如错误类型分布） | Canvas 2D |
| 热力图 | `heatmap` | 二维分布（如请求×响应时间） | Canvas 2D |
| 条形图 | `horizontalBar` | 排名对比 | Canvas 2D |
| 面积图 | `area` | 累积趋势 | Canvas 2D |

> 所有图表基于 Canvas 2D 绘制（非 WebGL/Three.js），降低 GPU 需求和包体积。

#### ChartCard Schema

```typescript
interface ChartCardProps {
  title: string;
  chartType: 'bar' | 'line' | 'pie' | 'heatmap' | 'horizontalBar' | 'area';
  data: ChartData | { $bind: string; mode?: 'replace' | 'append' };
  dimensions: string[];       // 维度名（如 ["修改前", "修改后"]）
  metrics: string[];           // 指标名（如 ["响应时间(ms)"]）
  config?: {
    showLegend?: boolean;
    showGrid?: boolean;
    colorPalette?: string[];   // 使用语义色
    animate?: boolean;         // 入场动画
  };
}

interface ChartData {
  labels: string[];
  datasets: {
    label: string;
    values: number[];
  }[];
}
```

#### 流式数据追加

```json
// Agent 逐步推送数据
{"cmd": "chartAppendData", "target": "chart_1",
 "data": {"labels": ["模块A"], "datasets": [{"label": "耗时(ms)", "values": [230]}]}}

{"cmd": "chartAppendData", "target": "chart_1",
 "data": {"labels": ["模块B"], "datasets": [{"label": "耗时(ms)", "values": [180]}]}}
```

前端接收到 `chartAppendData` 时，将数据追加到 ChartCard 的 data model 中，触发 Canvas 重绘（仅 repaint 脏区域）。

#### Canvas 性能策略

| 策略 | 说明 |
|------|------|
| **脏矩形重绘** | 仅重绘数据发生变化的区域 |
| **requestAnimationFrame** | 与显示器 VSync 同步，避免无用帧 |
| **离屏 Canvas** | 静态部分（坐标轴、图例）离屏渲染缓存 |
| **降级采样** | 数据点 > 1000 时自动降采样 |
| **零透明度** | 避免 `globalAlpha` 操作 |
| **字体缓存** | 预测量文本宽度缓存 |

### 7.8 ⭐ 网页跳转能力（WebLink）

#### 设计目标

Agent 在回复中引用外部文档、API 参考、Bug 追踪链接时，直接渲染为可点击卡片，支持预览和内嵌浏览器打开。

#### 渲染形态

```
┌──────────────────────────────────┐
│  🔗 查看 API 文档          ↗     │  ← 凸起卡片，点击浏览器打开
│  https://docs.example.com/api    │
└──────────────────────────────────┘

┌──────────────────────────────────┐
│  ┌─────┐  📖 了解更多            │
│  │预览 │  ⚡ 性能优化最佳实践     │  ← 带预览按钮
│  └─────┘  https://...            │
└──────────────────────────────────┘
```

#### WebLink Schema

```typescript
interface WebLinkProps {
  label: string;                    // 显示文本
  url: string;                      // 目标 URL
  description?: string;             // 可选的描述文本（URL 下方显示）
  icon?: string;                    // 图标标识
  preview?: boolean;                // 是否支持鼠标悬浮预览
  trustLevel?: 'safe' | 'external' | 'danger';  // 安全等级
}
```

#### 交互行为

| 用户操作 | 行为 |
|----------|------|
| 单击 | 系统默认浏览器打开 URL |
| Ctrl+单击 | 应用内嵌浏览器面板打开（侧栏/独立窗口） |
| 悬停 500ms | 缩略图预览（favicon + meta title/desc） |
| 右键 | 复制链接、在新窗口打开 |

#### 安全策略

| 等级 | 视觉标识 | 行为 |
|------|---------|------|
| `safe` | 无额外标识 | 同域/白名单站 |
| `external` | 🔗 图标 | 外部链接，正常打开 |
| `danger` | ⚠️ 黄色警告边框 | 需二次确认「确认打开外部链接？」 |

### 7.9 ⭐ 文件跳转能力（FileLink）

#### 设计目标

Agent 在提及文件修改、报错位置、参考代码时，渲染为可点击的文件链接卡片，单击精确定位到行/列。

#### 渲染形态

```
┌──────────────────────────────────┐
│  📄 src/config.ts :42            │  ← 凸起卡片，点击跳转
│  连接字符串从硬编码改为环境变量    │
└──────────────────────────────────┘

┌──────────────────────────────────┐
│  文件变更清单                     │
│  ┌────────────────────────────┐  │
│  │ 📄 src/config.ts        +5 │  │  ← 带 diff 统计
│  │ 📄 src/db.ts            -2 │  │
│  │ 📄 src/utils.ts        +12 │  │
│  └────────────────────────────┘  │
└──────────────────────────────────┘
```

#### FileLink Schema

```typescript
interface FileLinkProps {
  label: string;                    // 显示文本
  filePath: string;                 // 项目内相对/绝对路径
  line?: number;                    // 行号（可选）
  column?: number;                  // 列号（可选）
  description?: string;             // 可选的描述文本
  diffStats?: {                     // 可选的 diff 统计
    additions: number;
    deletions: number;
  };
  icon?: string;                    // 根据文件扩展名自动推断
}
```

#### LinkGroup Schema

```typescript
interface LinkGroupProps {
  style: 'horizontal' | 'vertical' | 'compact';
  children: string[];               // WebLink | FileLink 的 ID 列表
}
```

#### 交互行为

| 用户操作 | 行为 |
|----------|------|
| 单击 | 在编辑器中打开文件并跳转到 `:line:column` |
| Ctrl+单击 | 在侧栏预览面板打开（不离开当前对话流） |
| 悬停 | 显示文件路径 + 代码片段预览（前 5 行上下文） |
| 右键 | 复制路径、在文件管理器中打开 |

#### 跳转实现

```typescript
// 文件跳转事件
interface NavigateEvent {
  type: 'navigateRequest';
  source: 'fileLink' | 'webLink';
  target: string;        // filePath 或 URL
  line?: number;
  column?: number;
}

// 前端接收后分发
window.dispatchEvent(new CustomEvent('navigateRequest', {
  detail: {
    type: 'fileLink',
    target: '/src/config.ts',
    line: 42,
  }
}));
```

### 7.10 DSL 卡片渲染 vs 传统渲染对比

| 维度 | 传统前端 | DSL 卡片渲染（本项目） |
|------|----------|----------------------|
| UI 控制权 | 前端固定 | 后端/Agent 驱动 |
| 迭代速度 | 发版 | 运行时 |
| 扩展性 | 新增组件需编码 | 注册新卡片类型即可 |
| 安全性 | 代码执行 | 声明式数据（沙箱安全） |
| LLM 友好 | 低（需生成代码） | 高（JSON 无需代码执行） |
| 流式支持 | 需额外处理 | 天然支持增量更新 |
| **卡片讲解** | 无原生支持 | **内置 ExplainEngine** |
| **嵌入式图表** | 需独立 ECharts 集成 | **内置 ChartEngine + Canvas** |
| **跳转能力** | 手动实现 | **内置 LinkEngine** |

---

## 8. 可访问性与国际化

### 8.1 无障碍标准 (a11y)

| 标准 | 要求 | 实现方式 |
|------|------|----------|
| ARIA 规范 | 所有交互元素有 role/label | aria-label, aria-describedby |
| 键盘导航 | 所有功能可通过键盘完成 | Tab 顺序 + 快捷键 |
| 焦点管理 | 弹窗/模态的焦点捕获 | focus-trap（凸起边框焦点环） |
| 对比度 | WCAG AA (4.5:1 正文, 3:1 大文本) | 低饱和度色板已满足 |
| 屏幕阅读器 | 动态内容更新播报 | aria-live="polite"（DSL 卡片增量播报） |
| 减少动效 | 尊重 prefers-reduced-motion | CSS media query 关闭装饰动画 |

### 8.2 多语言方案

| 层级 | 方案 | 说明 |
|------|------|------|
| 框架 | i18next | 支持 ICU 消息格式 |
| 翻译存储 | JSON namespace 文件 | `locales/en/common.json` |
| 构建 | 按需加载 | 首屏只加载当前语言 |
| 切换 | 运行时热切换 | 无需刷新 |

---

## 9. 性能目标与低端机适配

### 9.1 核心指标

| 指标 | 目标 | 低端机目标 | 测量工具 |
|------|------|-----------|----------|
| 首屏渲染(TTR) | < 500ms | < 1000ms | Tauri/Electron metrics |
| 消息流延迟 | < 100ms | < 200ms | React DevTools Profiler |
| 会话加载 (1000条) | < 200ms | < 500ms | 虚拟化列表性能测试 |
| 帧率 | 稳定 60fps | ≥ 30fps | FPS 监控 |
| 内存 (长期会话) | < 200MB | < 300MB | Chrome DevTools Memory |
| 包体积 | JS < 500KB(gzip) | 同上 | webpack-bundle-analyzer |

### 9.2 低端机适配策略

| 策略 | 说明 | 触发条件 |
|------|------|----------|
| **CSS 动效降级** | 动画帧监测：< 30fps → 关闭所有装饰动画 | `requestAnimationFrame` 间距 > 33ms |
| **虚拟化列表** | 消息历史 > 100 条时启用 react-window | 消息数阈值 |
| **Canvas 降采样** | 数据点 > 500 自动降低渲染精度 | 数据量阈值 |
| **阴影降级** | 关闭双阴影，回退为纯色边框 | 低端机（通过 `navigator.hardwareConcurrency` < 4 检测） |
| **骨架屏优先** | 所有异步内容先显示骨架屏 | 内容加载中 |
| **懒加载** | 面板/卡片滚动到视口内才渲染 | IntersectionObserver |
| **大负载截断** | SSE 消息超过 1MB 时截断，点击展开 | 消息体大小检测 |
| **GC 友好** | 每 100 条消息释放不可见 DOM | 消息数阈值 |

### 9.3 CSS 动效降级系统

```css
/* 默认：全动画 */
.neumorphic-card {
  transition: box-shadow 0.2s ease, transform 0.2s ease;
}

/* 系统减少动效 → 保留必要过渡，去除所有装饰动画 */
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}

/* 低性能检测注入的 class → 关闭阴影、降级动效 */
.low-performance .neumorphic-card {
  box-shadow: none;
  border: 1px solid var(--n-85);
  transition: none;
}
```

### 9.4 渲染优化策略

| 技术 | 场景 |
|------|------|
| **虚拟化列表** (react-window) | 长消息历史 (>100条) |
| **流式增量渲染** | Agent 流式输出 + 图表数据追加 |
| **懒加载** (IntersectionObserver) | 文件内容、大 diff、图表 |
| **React.memo + useMemo** | 高频更新组件（消息列表、工具状态） |
| **代码分割** (React.lazy) | 图表引擎、讲解引擎（非首屏） |
| **骨架屏** | 所有异步内容 |
| **debounce + throttle** | 搜索输入、滚动事件 |
| **Canvas 离屏渲染** | 图表静态部分缓存 |

### 9.5 包体积预算

| 分类 | 目标 (gzip) | 说明 |
|------|------------|------|
| 核心框架 (React + deps) | < 150KB | React、ReactDOM |
| 卡片渲染引擎 (DSL) | < 50KB | 不含具体卡片组件 |
| 内置卡片注册表 | < 60KB | 所有卡片组件 Lazy Load |
| 图表引擎 (Canvas) | < 40KB | 自研轻量渲染，ECharts 太重 |
| 主题/样式 | < 30KB | CSS Variables + Tailwind |
| 工具类/状态管理 | < 40KB | Zustand + TanStack Query |
| **总预算** | **< 500KB** | 首屏加载 |

---

## 10. 技术栈选型

### 10.1 推荐技术栈

| 层 | 技术 | 选型理由 |
|----|------|----------|
| **框架** | React 19 + TypeScript 5.x | 生态最大、社区成熟、Streaming 支持 |
| **构建** | Vite 6.x | 极快 HMR、ESM 原生、Tauri 友好 |
| **桌面容器** | Tauri 2.x | 体积小 (~5MB)，Rust 后端扩展便利 |
| **CSS 方案** | Tailwind CSS 4 + CSS Variables | 零运行时、设计 Token 集成 |
| **组件基座** | Radix UI (headless) | 无障碍、完全自定义样式空间 |
| **动画** | **纯 CSS transitions + @keyframes** | ⭐ 零 JS 开销，GPU 无依赖 |
| **图表** | **自研轻量 Canvas Renderer** | ⭐ 40KB gzip 内，ECharts (300KB+) 太重 |
| **状态管理** | Zustand + TanStack Query | 轻量 + 服务端状态缓存 |
| **Schema 验证** | Zod | TypeScript 类型推导、JSON Schema 导出 |
| **SSE/流式** | EventSource / fetch + ReadableStream | 标准、浏览器支持好 |
| **WebSocket** | ws + reconnecting-websocket | 实时双向通信 |
| **虚拟化** | react-window | 长列表虚拟化渲染 |
| **测试** | Vitest + Playwright + Testing Library | Vite 生态、E2E + 单元 |
| **格式化/规范** | Biome / ESLint + Prettier | 统一规范 |

### 10.2 桌面容器对比

| 维度 | Tauri 2.x ✅ | Electron 32 |
|------|-------------|-------------|
| 包体积 | ~5MB | ~150MB+ |
| 内存占用 | ~30MB (基础) | ~80MB (基础) |
| 性能 | 原生 Rust 后端 | Chromium 多进程 |
| 低端机适配 | 更优（系统 WebView 轻量） | 较重 |
| 生态成熟度 | 发展中 | 极成熟 |
| Rust 集成 | 原生 | 需额外桥接 |
| WebView 兼容 | 系统 WebView（Edge/WebKit） | 内置 Chromium |

### 10.3 ⭐ 动画库选型决策

| 方案 | 包体积 (gzip) | GPU 依赖 | CPU 开销 | 低端机表现 | 是否推荐 |
|------|--------------|----------|----------|-----------|---------|
| **纯 CSS** | 0KB | 无 | 极低 | 优 | **✅ 推荐** |
| Framer Motion | ~15KB | 部分 | 中 | 中 | ❌ (过度) |
| react-spring | ~12KB | 部分(物理引擎) | 中高 | 差 | ❌ |
| GSAP | ~25KB | 无(但 JS 循环) | 高 | 差 | ❌ |
| CSS + 降级检测 | 0KB | 无 | 极低 | 优 | **✅ 本项目方案** |

### 10.4 ⭐ 图表库选型决策

| 方案 | 包体积 (gzip) | GPU 依赖 | Canvas/WebGL | 低端机表现 | 是否推荐 |
|------|--------------|----------|-------------|-----------|---------|
| **自研轻量 Canvas** | ~40KB | 无(Canvas 2D) | Canvas | 优 | **✅ 推荐** |
| ECharts | ~300KB+ | 可选 WebGL | Canvas/WebGL | 中 | ❌ (过重) |
| Chart.js | ~65KB | 无 | Canvas | 良 | 备选 |
| uPlot | ~35KB | 无 | Canvas | 优 | 备选（仅时序) |
| D3.js | ~80KB | 无 | SVG | 中 | ❌ (SVG DOM 重) |

### 10.5 备选框架

| 框架 | 场景 | 说明 |
|------|------|------|
| Vue 3 + Nuxt | Vue 技术栈团队 | A2UI 已有 Vue 实现 |
| Svelte 5 | 极致包体积 + 低端机 | 编译时框架，无 runtime，CSS 原生动画友好 |
| SolidJS | 高帧率流式渲染 | 细粒度响应式，适合大量流式更新 |

---

## 附录 A：竞品设计参考清单

| 产品 | 调研要点 | 参考价值 |
|------|----------|----------|
| [Codex CLI](https://github.com/openai/codex) | Rust TUI, App Server 协议, HistoryCell 体系 | 协议层架构、组件生命周期 |
| [Claude Code](https://github.com/anthropics/claude-code) | React+Ink, 工具卡片, 审批流 | 交互模式、状态设计 |
| [Cursor](https://cursor.com) | 多进程 Electron, MCP 集成 | 前端架构分层 |
| [Windsurf](https://windsurf.com) | Cascade 引擎, 视觉编程 | AI 工作流设计 |
| [Ant Design X](https://x.ant.design) | A2UI 协议, DSL 卡片系统 | 协议层 DSL 定义 |
| [Agent Elements (21st.dev)](https://21st.dev) | 25 个 shadcn 组件, 工具卡片系统 | 组件化实现 |
| [assistant-ui](https://github.com/assistant-ui/tool-ui) | AI Chat UI 组件, 审批卡片 | 实用组件 |
| [StyleSeed](https://github.com/bitjaru/styleseed) | 设计系统, 74 规则, 8 品牌皮肤 | 设计 Token 体系 |
| [TypeUI](https://www.creative-tim.com/blog/ai-agent/typeui) | CLI 管理 SKILL.md, 50+设计文件 | 设计系统分发模式 |
| [Klein Void](https://github.com/robertnowell/klein-void) | APCA 验证终端主题 | 体感对比度设计 |

---

## 附录 B：色彩参考来源

- **A2UI Theme System** — 6 色板 + `light-dark()` 自动反转
- **StyleSeed Design Engine** — 细黑 (`#2A2A2A`)、嵌套圆角法则、低透明度分层阴影
- **Klein Void (APCA)** — 暖米色(`#EDE6D3`)配近黑(`#0B0D14`)、体感对比度 ≥ 90
- **Morandi Color Palette** — 莫兰迪低饱和度色系，长时间观看舒适度
- **Neumorphism Design** — 双阴影系统、凸起/凹陷质感、软光照明

---

## 附录 C：关键设计决策记录

| 决策 | 选项 | 选择 | 理由 |
|------|------|------|------|
| 设计风格 | 扁平 / 毛玻璃 / 新拟物 | **新拟物** | 低饱和低 GPU 需求，质感丰富 |
| 色彩饱和度 | CLI 原始 / 降 60% | **降 60%** | 长时间使用不疲劳，莫兰迪舒适 |
| 高饱和区域 | 全屏 / 仅 Logo | **仅 Logo** | 品牌识别度保留，其余视觉舒适 |
| 阴影方案 | filter / box-shadow | **box-shadow** | 纯 CPU 栅格化，不触发合成层 |
| 动画方案 | Framer Motion / CSS | **纯 CSS** | 零 JS 开销，低端机友好 |
| 图表方案 | ECharts / 自研 Canvas | **自研 Canvas** | 包体积 40KB 内，Canvas 2D 无 GPU 需求 |
| DSL 协议 | 自研 / A2UI | **A2UI v0.9 + 扩展** | 已有成熟协议，扩展 4 命令即可 |
| 桌面容器 | Electron / Tauri | **Tauri** | 体积 5MB，低端机内存优势 |
