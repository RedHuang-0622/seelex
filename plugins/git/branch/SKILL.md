---
description: "分支策略与生命周期管理：Git Flow、GitHub Flow、Trunk-Based 的选择与操作规范"
---
# 分支控制 — 策略与管理

你是一个 Git 分支管理专家，精通各种分支模型的优劣和适用场景。

## 一、分支模型选型

### 1. Trunk-Based Development（推荐 CI/CD 团队）

```
main ← 所有变更直接/短分支合入
  ├── feature/xxx  →  → main（24h 内合并）
  ├── fix/xxx      →  → main
  └── experiment/  →  → main（或丢弃）

适用：成熟 CI/CD、小团队、高频发布
核心：分支存活时间 < 1 天，频繁合并到 main
```

✅ 优点：最简单、最少合并冲突、持续集成
❌ 代价：需要完善的 feature toggle、不适合大版本隔离

### 2. GitHub Flow

```
main ← 稳定可部署
  └── feature/* → main（通过 PR 合并）
  └── fix/* → main（通过 PR 合并）

适用：持续部署、PR 审查流程
流程：创建分支 → 开发 → PR → 审查 → 合并到 main → 部署
```

✅ 优点：轻量、PR 驱动、适合开源
❌ 代价：没有发布分支，不方便管理多版本

### 3. Git Flow（适合发布周期明确的项目）

```
main          ← 只包含已发布版本
  └── develop   ← 日常开发主干
       ├── feature/* → develop
       ├── release/* → develop → main
       └── hotfix/*  → main → develop

适用：有版本发布周期、需要维护多个历史版本
```

✅ 优点：版本隔离好、发布流程清晰
❌ 代价：分支多、操作复杂、merge 频繁

## 二、分支命名规范

| 类型 | 命名模式 | 示例 |
|------|---------|------|
| 功能分支 | feature/<短描述> | feature/user-auth |
| 修复分支 | fix/<问题编号或描述> | fix/issue-142 |
| 发布分支 | release/<版本号> | release/v2.1.0 |
| 热修复分支 | hotfix/<版本号> | hotfix/v2.0.1 |
| 实验分支 | experiment/<描述> | experiment/ai-router |
| 杂务分支 | chore/<描述> | chore/upgrade-deps |

## 三、分支生命周期管理

### 创建
```bash
# 从最新 main 开始
git switch main && git pull --rebase
git switch -c feature/xxx
```

### 保持同步
```bash
# 定期 rebase 到 main（仅在本地分支）
git fetch origin
git rebase origin/main

# 或 merge（如已共享）
git fetch origin
git merge origin/main
```

### 提交 PR / 合并
```bash
# 合并前整理 commit
git rebase -i HEAD~3   # 交互式整理
git push origin feature/xxx

# 创建 PR，审查后合并
# 合并后删除远程分支
git push origin --delete feature/xxx
# 删除本地分支
git branch -d feature/xxx
```

### 清理策略
- **合并后立即删除**：本地 + 远程分支
- **本地分支巡检**：`git branch --merged main | grep -v main | xargs git branch -d`
- **远程残留**：`git remote prune origin`

## 四、保护规则（推荐 GitHub/GitLab 配置）

| 规则 | 说明 |
|------|------|
| 要求 PR 审查 | 至少 1 人 approve 后才能合并 |
| 要求 CI 通过 | 合并前必须所有检查通过 |
| 禁止直接 push main | 只能通过 PR 合并 |
| 要求分支最新 | 合并前分支必须 rebase/merge 到最新 main |
| 线性历史 | 只允许 squash merge 或 rebase merge |

## 五、commit 规范

推荐 Conventional Commits 格式：

```
<type>(<scope>): <description>

[optional body]
[optional footer]
```

| type | 含义 |
|------|------|
| feat | 新功能 |
| fix | 修复 |
| refactor | 重构 |
| docs | 文档 |
| chore | 杂项 |
| test | 测试 |
| revert | 回滚 |

示例：
```
feat(auth): 添加 OAuth2.0 登录支持
fix(parser): 修复空指针导致的崩溃
refactor(api): 统一错误响应格式
```
