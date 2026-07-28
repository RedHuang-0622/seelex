# Git Plugin

`git` 聚焦版本控制、变更审查、branch/rebase/worktree 工作流。`plugin.md` 暴露 `git_*` 和必要 shell，子 Skill 提供 PowerShell 辅助脚本。

## 子模块

- `branch/`：分支命名、创建、同步和状态。
- `rebase-merge/`：合并策略与冲突处理。
- `worktree/`：隔离任务目录和 Plan SubAgent 工作流。

## 边界

Git Plugin 只提供能力，不自动授权 commit、push、force update 或删除 branch/worktree；外部状态变更仍需要用户意图。

## 依赖与生命周期

`plugin.md` 控制 runtime 工具可见性，三个 Skill 由 `skill.Loader` 发布；PowerShell 脚本只在 Skill 明确调用时执行，不是 Plugin 激活钩子。

## Review

- 脚本必须验证仓库和目标路径，尤其是 worktree 删除/移动。
- 默认使用非交互命令，保留用户未提交改动。
- 测试脚本参数解析时不要依赖个人 Git 配置。

## 验证

```text
go test ./plugin ./skill . -run 'Plugin|Skill|Layout' -count=1
```
