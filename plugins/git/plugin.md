---
schema_version: 1
name: git
description: Git 版本控制和变更审查
include: [switch_plugin, switch_mode, "git_*", bash, get_time]
exclude: []
---

# Git

面向版本控制和变更审查。

## 技能（Skills）

| 技能 | 说明 | 脚本 |
|------|------|------|
| **branch** | 分支策略与生命周期管理 | `git-branch.ps1` |
| **rebase-merge** | Rebase vs Merge 场景决策 | `git-rebase-merge.ps1` |
| **worktree** | Worktree 隔离开发与 Plan SubAgent 集成 | `git-worktree.ps1` |

### branch
提供分支创建（含命名规范校验）、同步、清理、commit 规范校验功能。
支持 Trunk-Based、GitHub Flow、Git Flow 三种分支模型。
- 交互菜单：`Show-BranchMenu`
- 创建分支：`New-GitBranch -BranchType feature -Description "xxx"`
- 同步分支：`Sync-GitBranch`
- 状态查看：`Get-GitBranchStatus`

### rebase-merge
根据分支状态自动选择 git pull --rebase 或 merge 策略。
提供交互式 rebase、冲突检测与解决、缩写快捷操作。
- 交互菜单：`Show-RebaseMergeMenu`
- 智能 Pull：`Invoke-SmartPull`
- 安全 Rebase：`Invoke-SafeRebase`
- 冲突查看：`Resolve-RebaseConflict`

### worktree
Git Worktree 管理，支持 Plan 模式 SubAgent 隔离开发。
提供 worktree 创建、列表、删除、移动、清理操作。
- 交互菜单：`Show-WorktreeMenu`
- 创建 SubAgent worktree：`Add-PlanSubAgentWorktree -NodeId "xxx"`
- 紧急任务切换：`Switch-GitTask -Description "xxx"`
- 合并与清理：`Merge-PlanSubAgentWorktree -WorktreePath "xxx" -BranchName "xxx"`
