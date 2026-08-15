# Default Plugin

`default` 是未选择专业形态时的开放能力入口，允许所有已注册工具并展示全局 Skill。它适合通用对话、跨域任务和插件选择前的探索。

## 实现

- `plugin.md` 的 include/exclude 均为空，交由 Seele Tool holder 暴露全部工具。
- Plan 是启动即注册的基础工具；`plan/` 是默认 Plugin 提供的 `#plan` Skill，不再存在独立 Plan Plugin。
- 子目录包含代码、代码审美、规划、测试、review、goal 和 CLI 设计等通用 Skill。
- 激活专用 Plugin 后，Registry 只展示该 Plugin 发布的 Skill；退出后恢复 default/global 集合。

## 非职责

Default 不等于 full-access permission。工具可见性与执行授权是两个独立层次。

## 依赖与生命周期

manifest 由 `plugin.Loader` 读取，Tool filter 由 `seelebridge.Runtime` 注册，Skill 可见性由 `skill.Registry` 计算。Default 通常是启动基线；专用 Plugin 停用后回到全局 Skill/工具视图。

## Review

- 新全局 Skill 是否真的适用于所有形态；领域专用 Skill 应放到对应 Plugin。
- 不要用 default 绕过 project scope、PathGate 或 approval。

## 验证

```text
go test ./skill ./plugin . -run 'Plugin|Skill|Layout' -count=1
```
