# 前端组件化最终审查

## 变更概览

| 提交 | 范围 | 设计方式 |
|---|---|---|
| `bdb34d1` | Plan JSON DSL、增量渲染、样式与测试 | JSON 单一状态源 + 纯 DSL 转换 + keyed DOM reconcile |

## 结论

当前组件化对 **v0.1 alpha 演示和继续验证产品形态基本够用**，但对后续持续增加项目、会话、权限、知识库、CodeGraph、设置页和更多运行时卡片 **不够用**。

Plan 模块本身已经形成相对清晰的边界；整体前端仍是“若干独立模块围绕一个 600 行 `app.js` 运转”，尚未形成统一的组件生命周期、状态选择器和事件分发机制。

## 五轴审查

| 维度 | 状态 | 评分 | 说明 |
|---|:---:|:---:|---|
| 正确性 | ⚠️ | B | Snapshot/Event 主链正确，但 Full Access 使用本地布尔值，可能与后端真实权限脱节 |
| 可读性 | ⚠️ | B- | 小模块清晰；`app.js` 同时承担启动、Bridge、状态、渲染、事件和多个业务域 |
| 架构 | ⚠️ | C+ | 有 ES module，但缺统一组件接口、selector 和共享 reconcile 原语 |
| 安全性 | ⚠️ | B- | 文本普遍转义；Git remote 直接进入 `href`，未限制 URL scheme |
| 性能 | ✅ | B | Plan keyed 更新良好；`runtime.changed` 仍会重建无关 Plugin/Account 列表并重复绑定监听器 |
| 测试性 | ⚠️ | C+ | 纯函数模块测试较好；`app.js`、DOM 合同、设置/会话/工作区控制器缺集成测试 |

## 做得好的部分

1. `client-state.js` 与 `protocol.js` 将快照同步、revision 和事件顺序从 UI 中分离。
2. `conversation-view.js` 已经用稳定 key 保留消息 DOM 和滚动状态。
3. `effort-control.js` 通过依赖注入接收提交函数和错误回调，是目前最接近标准组件的模块。
4. `plan-dsl.js` 不复制业务生命周期，Plan JSON 每次重新派生 DSL；状态点、边和进度共用同一数据源。
5. Markdown、Plan DSL、协议、客户端状态均有独立单元测试。

## 发现的问题

### 高优先级警告

#### 1. `app.js` 是事实上的 God Module

`app.js` 约 600 行，同时负责：

- Wails Bridge 调用；
- 全量与增量渲染编排；
- Session 列表；
- Project/Workspace；
- Runtime、Plugin、Account、Skill；
- Storage Settings；
- Command Palette 与 inline suggestions；
- Interaction Modal；
- Composer、权限和全局快捷键；
- 应用初始化及事件订阅。

结果是任一 DOM、Bridge 或状态字段改动都可能影响整个入口文件，也使功能无法独立 mount、测试或销毁。

#### 2. DOM 合同没有被验证

入口在模块加载时一次性 `getElementById`，随后直接调用 `addEventListener`。HTML 少一个必需 ID 就会让整个 GUI 在启动阶段崩溃，这正是此前 `Cannot read properties of undefined/null` 类型错误容易出现的根因。

当前注册表还包含 HTML 中不存在的可选 `effort-value`，说明 JS/HTML 合同已经出现漂移，只是该组件恰好允许 output 为空。

#### 3. Full Access 权限状态不属于权威快照

`fullAccessOn` 是 `app.js` 内的本地变量，初始化固定为 false。会话恢复、后端拒绝、运行时重建或其他入口修改权限后，按钮可能显示与实际权限不一致。

权限属于安全状态，应进入 Snapshot/Runtime 合同，并由后端状态驱动 UI；前端点击只发送意图，不自行拥有最终状态。

#### 4. Git remote 链接缺少 URL scheme 校验

Workspace 的 `git_remote` 虽然做了 HTML 转义，但直接写入 `href`。HTML 转义不能阻止 `javascript:` 等危险 scheme。项目来自不受信任仓库时，恶意 remote 配置可能进入有 Bridge 权限的 WebView。

应只允许 `https:`/`http:`，对 `git@host:path`、`ssh:` 等不可浏览 remote 显示为文本或转换为受控 HTTPS 地址，并添加 `rel="noopener noreferrer"`。

### 中优先级警告

#### 5. 增量事件没有组件级 selector

Plan 节点状态变化发布 `runtime.changed` 后，入口同时重绘 Runtime、Plugin、Account、Plan、Skill 和 Project。Plugin/Account 会整体替换 HTML 并重新绑定监听器，即使只有 Plan 的一个状态点变化。

需要按 feature selector 或字段版本决定更新目标，例如 `runtime.plan` 只通知 PlanView。

#### 6. 存在两套 keyed reconcile

`conversation-view.js` 和 `plan-dsl.js` 各自实现了 keyed DOM 对账。两者目前需求略有差异，但节点复用、排序和删除逻辑可以下沉为共享 DOM primitive，否则后续卡片组件会继续复制第三套。

#### 7. `components.js` 职责过宽

该文件同时包含 Icon registry、HTML escape、Markdown 入口、Conversation model、Message、Tool IO 和 Project source rendering。名字泛化且业务职责混合，后续很容易成为第二个入口巨石。

#### 8. CSS 和分发目录缺乏组件边界

`styles.css` 接近 40 KB，包含所有页面、弹窗和卡片样式；通用类与组件类混排。`patch_app.py` 也位于 `frontend/dist`，而 Go 使用通配符 embed，会把这个开发期补丁脚本一并打进 GUI 二进制。

## 推荐目标结构

保持无框架、原生 ES module 即可，不需要为了组件化引入 React/Vue。

```text
frontend/dist/
  app.js                         # 仅 bootstrap、组合和订阅
  platform/
    bridge.js                    # Wails Bridge adapter
  state/
    client.js
    protocol.js
    selectors.js
  shared/
    dom.js                       # requireElements、keyed reconcile
    html.js                      # escape/safeURL
    icons.js
    modal.js
  features/
    sessions/controller.js
    workspaces/controller.js
    runtime/controller.js
    plan/model.js                # JSON -> DSL
    plan/view.js                 # DSL -> DOM
    settings/controller.js
    command-palette/controller.js
    interaction/controller.js
    composer/controller.js
  styles/
    base.css
    layout.css
    plan.css
    conversation.css
    settings.css
```

每个 UI feature 建议统一暴露：

```js
createFeature({ root, bridge, notify }) -> {
  render(snapshotSlice),
  destroy()
}
```

入口只负责把 snapshot selector 的结果传给组件；组件只操作自己的 root，不读取全局 `elements`。

## 实施顺序

### P0：先修正确性与安全边界

1. 增加 `requireElements()` 和 JS/HTML DOM 合同测试。
2. Full Access 纳入后端 Snapshot，由后端状态驱动按钮。
3. Git remote 使用统一 `safeExternalURL()`。

### P1：拆入口业务域

1. 先拆 Workspace/Session controller，两者是项目—会话机制的核心。
2. 再拆 Settings 与 Command Palette，这两块拥有独立本地状态和事件。
3. 把 Interaction、Runtime sidebar、Composer 分别装配。
4. `app.js` 压缩到约 100 行以内。

### P2：统一基础设施

1. 抽取共享 keyed reconcile。
2. 拆分 `components.js` 与 `styles.css`。
3. 将 `patch_app.py` 移出 `dist` 或删除。
4. 增加基于轻量 DOM 环境的组件集成测试，覆盖 mount、render、event、destroy。

## 最终判断

- [x] 当前 Plan DSL 变更可以保留并继续使用。
- [x] 当前前端足以支持 alpha 演示。
- [ ] 当前组件化足以支撑后续大规模功能扩展。

**最终结论：有条件通过。** 下一阶段在继续加入知识库、CodeGraph 或更多会话能力之前，至少应完成 P0，并优先拆分 Workspace/Session 与 Settings/Command Palette。

