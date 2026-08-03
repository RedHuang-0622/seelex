# Seelex CLI 级 Memory 规划

> 状态：规划（第一阶段：memory.md 机制）
> 日期：2026-08-02
> 目标：seelex 拥有跨会话的 CLI 级记忆——代码风格要求、经典错误与解法、
> 用户偏好，装配进会话上下文，与项目级记忆（ProjectRecord）互补。

## 1. 背景

- 项目级：已有 `ProjectRecord`（模块语义、文件语义注册表，见
  project-file-semantics.md）——**项目知识**。
- 缺口：**用户级/CLI 级记忆**——用户对代码风格的要求、踩过的经典坑及其
  解法、协作偏好（如"配置参数和权限字段分开放"、"用 Actor 术语沟通"），
  跨项目、跨会话有效。Claude Code 用 MEMORY.md 机制解决，seelex 第一阶段
  采用同构方案。

## 2. 数据布局（第一阶段：memorymd）

```
~/.seelex/memory/                  # 用户级（跨项目）
  MEMORY.md                        # 索引：每行一条（标题 — hook）
  <name>.md                        # 详情文件（frontmatter: name/description/type）
  <name>.md                        # 可多个

<project>/.seelex/memory/          # 项目级（覆盖用户级同 key）
  MEMORY.md
  <name>.md
```

与 Claude Code 同构：索引常驻，详情按需加载。

## 3. 装配机制（第一阶段）

### 3.1 读取时机

- **会话开始时**（resumeSession / startChat）：读用户级 + 项目级 MEMORY.md
  索引，解析出条目清单（name + description + type）。
- **上下文块注入**：索引条目作为 system-level PromptBlock 装配（与
  parent-evidence / plan 块同机制）：
  - `memory-index` 块：全部条目一行式描述（预算内，≤ 20 条）
  - **详情按需**：agent 需要某条目细节时用 `read_memory(name)` 工具读取
    详情文件（避免全文常驻）。

### 3.2 写入路径

- 新工具 `remember_memory(name, description, content, type)`：
  - 用户级（默认）或项目级（显式 `scope: project`）
  - 写入 `<scope>/memory/<name>.md` + 更新 MEMORY.md 索引
- 提示词规则：任务中学到的经典错误/解法、用户明确的风格要求 → 主动
  `remember_memory`（类似 register_semantics 的沉淀机制）。

### 3.3 权限

- `remember_memory` 写操作走 permission 门控（默认 ask）；
- `read_memory` 只读 allow。

## 4. 与现有机制的关系

| 机制 | 粒度 | 内容 | 读写 |
|---|---|---|---|
| ProjectRecord（项目语义） | 项目级 | 模块/文件语义（code graph 种子） | register_semantics / read_semantics |
| **CLI Memory（本方案）** | 用户级 + 项目级 | 风格要求、经验教训、偏好 | remember_memory / read_memory |
| 会话记录（SessionRecord） | 会话级 | 对话 + 计划 + 检查点 | 自动 |

三者互补：会话级 = 事实，项目级 = 代码知识，CLI 级 = 用户偏好与经验。

## 5. 实施步骤（第一阶段）

1. `memory` 包：索引解析（MEMORY.md）、条目 CRUD、frontmatter 解析
   （复用 internal/frontmatter）
2. `remember_memory` / `read_memory` 工具注册（scoped 校验 + 权限门控）
3. 装配：会话开始时加载索引 → memory-index PromptBlock
4. 提示词规则 + harness 用例（沉淀时机）
5. 用户级目录初始化（首次运行时创建 `~/.seelex/memory/`）
6. 测试：索引解析 / 装配注入 / 工具端到端

## 6. 未来（第二阶段）

- 向量化检索：条目 embedding，`read_memory` 升级语义检索（与 code graph
  向量 role 共用基础设施）
- 自动沉淀：任务结束反思（Learning）时自动提取经验 → 待用户确认后写入
