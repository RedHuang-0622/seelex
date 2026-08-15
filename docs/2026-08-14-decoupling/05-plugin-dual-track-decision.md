# 插件双轨合并：产品决策讨论（root plugin ↔ seelebridge/plugin）

日期：2026-08-15
性质：决策讨论稿（已确认：单选 + 方案 A 轻量版；边界标注已实施）
关联：`docs/2026-08-14-decoupling` §02.4 / §04.7

## 1. 现状（两份"插件状态"）

| 状态面 | 持有者 | 内容 | 文件 |
|---|---|---|---|
| 插件定义（manifest/skills/MCP） | `plugin.Manager`（根） | `plugins map[string]Plugin` + `current` + `attached`（MCP 运行名） | [plugin/manager.go](../../plugin/manager.go) |
| 可见性执行面 | `seelebridge/plugin.Manager` | `defs map[string]Def`（include/exclude 快照）+ `active` | [seelebridge/plugin/plugin.go](../../seelebridge/plugin/plugin.go) |

装配链：`main.go initPluginSystem` → `plugin.NewManager(loader, runtime, runtime, skills)`，
root 经 backend 接口（`ToolBackend`/`MCPBackend`/`SkillBackend`）调用 Runtime，
Runtime 再委托 `seelebridge/plugin` 的 Define/Undefine/Activate/Filter。

问题：同一插件在 root 的 `plugins` 与 `seelebridge/plugin` 的 `defs` 各存一份
（内容不同：root 存完整契约，执行面只存 include/exclude + active），
切换/热更新必须同时改两层；且执行面只支持**单一激活插件**（`active string`）。

## 2. 收敛方向（解耦方案 §02.4）

root manager 保持唯一"插件定义"事实源；`seelebridge/plugin` 只做执行面
（backend 实现，不持有独立定义状态）；热更新 diff 在 root、apply 走 backend。

## 3. 需要产品决策的问题

### 3.1 是否支持多插件叠加（multi-plugin stacking）？

- **单选（现状）**：任一时刻只有一个激活插件，其 include/exclude 过滤全部工具。
  简单、可预测；`active string` 与 root `current` 一一对应。
- **多选叠加**：多个插件同时激活，可见性 = 交集/并集/优先级合并。
  需要定义合并规则（工具冲突时谁优先、skill 命名空间如何隔离、MCP 运行名
  是否仍 plugin-qualified），`seelebridge/plugin` 的 `active` 从 string 变集合。
- **分层叠加**：default 全局技能常驻，专业插件按需叠加在 default 之上
  （当前"激活后只展示该插件 Skill"的语义与之冲突，见
  [plugins/default/README.md](../../plugins/default/README.md)）。

### 3.2 热更新粒度

- 仅配置热更新（Add/Update/Remove，`plugin.Manager.Reload` 已具备）；
- 还是需要激活态热切换（切换时旧插件保持可用直到新插件就绪，`Activate`
  已具备 prepare/restore 语义）。

## 4. 方案选项

### 方案 A：root 唯一事实源 + 执行面无定义（收敛方向原案）

- `seelebridge/plugin` 删除 `defs`，改为只保存 `active`（或激活集合）；
- root 在 Define/Reload 时把 include/exclude 快照**推送到执行面**（backend
  调用），Filter 读执行面缓存；
- 优点：单一事实源，热更新只改 root，apply 走 backend；缺点：仍有一份
  可见性缓存（root 定义 → 执行面投影），需保证投影原子性（可复用
  `plugin/apply.go` 的 `Transaction`）。

### 方案 B：执行面完全无状态

- `seelebridge/plugin.Filter` 改为由 root 注入"当前激活插件定义"读取器
  （`func() (Def, bool)`），执行面不存 defs/active；
- 优点：无任何重复状态；缺点：Filter 每次读取跨层闭包，可见性策略与
  root 生命周期强耦合。

### 方案 C：保持现状 + 文档化边界

- 明确 `seelebridge/plugin` 是"可见性投影缓存"而非"定义事实源"，在
  README 标注投影方向；不做结构性改动。

## 5. 决策（2026-08-15 已确认）

- 产品确认：**保持单选**（任一时刻只有一个激活插件），不支持多插件叠加；
- 选定**方案 A 轻量版**：`seelebridge/plugin` 保留 `defs` 作为可见性投影
  缓存，事实源在 root `plugin.Manager`；写路径为 root → `ToolBackend` →
  Runtime → 执行面单入口（现状已是），配合 `plugin/apply.go` 的
  `Transaction` 保证"root 定义更新 + 执行面投影"原子。

## 6. 已落地

- `seelebridge/plugin` 包注释与 README 标注"投影缓存 / 事实源在 root /
  单选"边界（解耦方案 §02.4）。
- 写路径单入口经核查成立（全仓仅 root `plugin.Manager` 调用 backend）。
- 多插件叠加：仍作为未来产品议题，不排期。
