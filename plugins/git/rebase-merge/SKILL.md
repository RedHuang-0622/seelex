---
description: "Rebase vs Merge 场景决策：何时用 rebase pull、何时用 merge，以及各自的优劣和风险"
---
# Rebase vs Merge — 场景决策指南

你是一个 Git 版本控制专家，精通 rebase 和 merge 的适用场景与取舍。

## 核心原则

```
merge = 记录事实（"我在这里合并了分支"）
rebase = 美化历史（"我的变更看起来像是在最新代码上开发的"）
```

## 一、git pull --rebase vs git pull（即 merge）

### 使用 git pull --rebase（推荐日常场景）

**当你** | **原因**
--------|--------
在自己独享的分支上工作 | 只有你一个人看到这个分支的历史，重写无影响
还未 push 的本地 commit | commit 尚未离开本地，重写不会影响他人
想要线性历史 | 避免多余的 merge commit，git log 更清晰
从上游同步代码时 | rebase 把你的 commit 放到上游最新提交之后，避免分叉

**原理**：
```
  — A — B — C  (main)
       \
        D — E  (feature)

git pull --rebase 等价于：
git fetch origin
git rebase origin/main

结果：
  — A — B — C  (main)
               \
                D' — E'  (feature)  ← commit SHA 变了，但内容相同
```

✅ 优点：历史线性、无 merge commit、git bisect 友好
❌ 代价：重写了 commit SHA，只能用于未共享的分支

### 使用 git pull（即 merge）

**当你** | **原因**
--------|--------
在多人协作的公共分支上工作 | rebase 会重写公共历史，导致他人混乱
已经 push 的分支，别人也 pull 了 | 重写已共享的 commit = 破坏团队协作
需要保留分支拓扑 | merge commit 记录了"谁在何时合并了什么"
想保留准确的合并时间线 | merge commit 保留了分支合并的精确时间点

```
  — A — B — C — F (merge commit)
       \         /
        D — E —/

✅ 优点：保留真实历史、安全、不重写他人 commit
❌ 代价：历史有分叉、git log 稍显杂乱
```

## 二、黄金法则

> **绝不对已 push 并共享的分支执行 rebase。**

只要你的 commit 还在本地（没 push），rebase 是安全的。
一旦 push 出去，且可能被其他人 fetch 了，就只 merge。

## 三、决策流程图

```
这条分支有人跟我一起用吗？
  ├── 是 → 使用 git pull (merge)
  └── 否 → 这些 commit 已经 push 过吗？
               ├── 是 → 使用 git pull (merge)
               └── 否 → 使用 git pull --rebase
```

## 四、操作速查

| 场景 | 命令 |
|------|------|
| 同步上游最新代码（推荐） | git pull --rebase |
| 同步上游最新代码（安全路线） | git pull |
| 把当前分支变基到 main | git rebase main |
| 交互式整理最近 3 个 commit | git rebase -i HEAD~3 |
| 合并 feature 到 main | git switch main && git merge feature |
| 合并且不产生 merge commit | git merge --squash feature |
| 中止正在进行的 rebase | git rebase --abort |
| rebase 冲突解决后继续 | git rebase --continue |

## 五、冲突处理

两者都会产生冲突，但处理方式不同：

- **merge**：冲突在 merge commit 中解决，一次解决，历史保留
- **rebase**：冲突分散在每个被重放的 commit 中，需要逐个解决

> 如果冲突范围大、冲突文件多，优先用 merge，否则 rebase 时的逐 commit 解决非常痛苦。
