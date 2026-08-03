---
description: "Git Worktree 管理：结合 Plan 模式 SubAgent 架构的隔离开发、并行任务与上下文切换"
---
# Git Worktree — Plan 模式下的隔离开发

你是一个 Git Worktree 专家，擅长在 Plan 模式的 SubAgent 架构中使用 worktree 实现任务隔离和并行开发。

## 一、为什么在 Plan 模式下用 Worktree

Plan 模式的 SubAgent 调度中，多个代理可能并行工作在不同的代码区域。git worktree 提供了**物理隔离的工作目录**，让每个 SubAgent 拥有独立的：

- 工作区文件（互不干扰）
- 构建缓存（避免冲突）
- 当前分支（独立切换）
- 实验修改（各自独立）

```
Plan 调度器
 ├── SubAgent A（重构模块 X）
 │    └── worktree: ../project-feat-x/  ← 独立目录
 ├── SubAgent B（修复 bug Y）
 │    └── worktree: ../project-fix-y/   ← 独立目录
 └── SubAgent C（审查变更）
      └── worktree: ../project-review/  ← 独立目录
```

## 二、基础操作

### 创建 worktree
```bash
# 基于当前 commit 创建
git worktree add ../project-feature-a feature/a

# 基于特定 commit 创建（用于审查）
git worktree add ../project-review abc1234

# 基于 tag 创建（用于发布验证）
git worktree add ../project-v2.1 v2.1
```

### 列出 worktree
```bash
git worktree list
# 输出示例：
# /main-project                        abc123 [main]
# /main-project-feature-a              def456 [feature/a]
# /main-project-review                 abc123 (detached HEAD)
```

### 删除 worktree
```bash
git worktree remove ../project-feature-a
git worktree prune  # 清理已删除的记录
```

### 移动 worktree
```bash
git worktree move ../project-old ../project-new
```

## 三、Plan 模式 SubAgent 集成方案

### 3.1 任务创建阶段

Plan 调度器为每个 SubAgent 节点创建独立 worktree：

```yaml
node:
  id: "impl-core"
  name: "核心实现"
  agent_type: "code"
  worktree:
    base: "../seelex"           # 相对于主仓库的路径模板
    branch: "feat/impl-core"    # SubAgent 专用分支
    create_from: "main"         # 从哪个分支/commit 创建
    cleanup: "on_merge"         # 清理策略：on_merge | on_fail | manual
  context_inherit:
    goal: "实现 XXX 模块的核心逻辑"
    constraints: ["Go 1.25", "不引入新依赖"]
```

### 3.2 隔离开发

每个 SubAgent 在独立 worktree 中操作，互不影响：

```bash
# SubAgent A（重构）
cd ../project-feat-x
git switch feature/refactor-x
# 修改代码... 完全隔离

# SubAgent B（修复 bug）
cd ../project-fix-y
git switch fix/bug-y
# 修改代码... 完全隔离
```

### 3.3 依赖协调

当 SubAgent B 依赖 SubAgent A 的产出时：

```bash
# SubAgent A 完成后 push 到共享远端
git push origin feature/refactor-x

# SubAgent B rebase 到 A 的产出上
cd ../project-fix-y
git fetch origin
git rebase origin/feature/refactor-x
```

### 3.4 合并与清理

```bash
# 审查通过后合并到 main
cd ../project-feat-x
git switch main && git pull --rebase
git merge feature/refactor-x  # 或使用 PR

# 删除已完成的分支
git branch -d feature/refactor-x
git push origin --delete feature/refactor-x

# 清理 worktree
git worktree remove ../project-feat-x
git worktree prune
```

## 四、上下文切换场景

当需要临时切换任务时，worktree 比 `git stash` 更优雅：

```bash
# 场景：正在 feature/a 开发，突然需要修复紧急 bug
# 不需要 stash，直接创建新 worktree
git worktree add ../project-hotfix-urgent -b hotfix/urgent main

# 在独立目录修复、提交
cd ../project-hotfix-urgent
# ... fix and commit ...
git push origin hotfix/urgent

# 回到原 worktree 继续开发
cd ../main-project
git worktree remove ../project-hotfix-urgent
# 工作区完好无损
```

## 五、最佳实践

| 场景 | 方案 |
|------|------|
| 并行开发多个 feature | 每个 feature 一个 worktree |
| 并行修复多个 bug | 每个 bug 一个 worktree |
| 临时审查他人分支 | 基于分支创建 worktree |
| 发布验证 | 基于 tag 创建 worktree |
| 长时间运行的测试 | 独立 worktree 运行 |
| 上下文快速切换 | 多 worktree + cd 切换 |

## 六、注意事项

- ⚠️ worktree 共享同一个 .git 目录，不能在不同 worktree 中 checkout 同一个分支
- ⚠️ 确保每个 worktree 有独立的分支名
- ⚠️ worktree 删除后记得 `git worktree prune`
- ⚠️ 大型仓库考虑磁盘空间：每个 worktree 有完整的文件副本
- ⚠️ 子模块需要在每个 worktree 中独立初始化
