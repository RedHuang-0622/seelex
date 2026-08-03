# Seelex Memory 详细设计：生态位与全向量化读取

> 状态：设计（v2，全向量化）
> 日期：2026-08-03
> 关联：docs/plan/cli-memory.md（机制：MEMORY.md 索引 + 详情文件）
> 核心变更（v2）：**memory 的读取路径唯一走向量检索**——不经过
> grep / glob / Get-ChildItem 等通用文件读取；向量检索器是 memory 模块的
> 私有组件，不暴露为公开方法。memory 是 seelex 第一个支持向量化检索的
> 文件读操作。

## 1. 生态位（与 v1 一致）：Agent 的长期记忆层（LTM）

见 v1 结论：Memory = LTM，与 STM（会话）/ PK（项目知识）/ Skill（能力）
构成 agent 记忆四层。接口为 `read_memory` / `remember_memory` /
`forget_memory`，本体属于 agent 能力层。本文聚焦**读取实现的唯一性**。

## 2. 读取架构：全向量检索（无通用文件读取回退）

### 2.1 铁律

```
read_memory(query) 的检索实现 = 向量相似度匹配
  禁止：grep / glob / Get-ChildItem / os.ReadFile 逐文件硬读作为检索路径
```

- memory 的读取**从第一天就是向量检索**——Phase 1 即向量化，不做
  "先关键词后向量"的两段式。
- 通用文件工具（read_file / grep_search / glob）**不提供向量检索能力**——
  它们保持普通文件语义；向量检索只存在于 memory 模块内部。

### 2.2 读取链路

```
read_memory(query, limit)
  └─ memory.Retriever（私有，unexported）
       ├─ query → embedding（graph role 向量模型，见 §3.2）
       ├─ 向量索引 top-k 相似度（余弦）
       ├─ name 精确匹配始终优先（精确名 = 100% 相似，绝对召回）
       └─ 返回条目 + 命中片段（chunk 级别）

无回退：向量索引缺失/损坏 → 返回错误并提示 reindex（不降级到 grep）
```

### 2.3 为什么"不降级到 grep"

- **一致性**：检索语义唯一（向量相似度），避免"有时语义有时关键词"
  的双轨行为导致 agent 对 memory 的预期不一致。
- **示范价值**：memory 是向量化读操作的第一个落地形态——为 code graph
  （项目语义）的向量检索提供同构实现样板。
- **封装测试**：私有 Retriever 使测试聚焦单一路径（确定性 embedding
  桩 → 断言 top-k），不产生第二实现的分叉。

## 3. 组件设计

### 3.1 模块边界（私有性）

```
memory 包（internal/memory）
  ├─ memory.go        MemoryStore：条目 CRUD（remember/forget/list）
  ├─ index.go         Index：MEMORY.md 索引解析（frontmatter + 行索引）
  ├─ retriever.go     Retriever（unexported 类型 + 方法）：
  │                    ● BuildIndex(entries)   — embedding + 落盘
  │                    ● Retrieve(query, limit) — 向量 top-k + 精确名优先
  │                    ● Reindex()             — 全量重建
  └─ embed.go         Embedder 接口（注入 graph role 客户端，可桩替换）

公开面（exported）：
  - New(scope, embedder) *Memory    — 唯一构造入口
  - (*Memory).Remember / Forget / List / Retrieve
  - Retrieve 是 read_memory 工具 handler 的唯一调用面

私有面（unexported）：
  - retriever 及其全部内部（向量索引构建/检索/存储格式）
  - 不对外导出任何"向量检索任意文件"的能力
```

**封装约束**：`Retriever` 不是独立导出类型，而是 `Memory` 的内部组件；
`Read`（向量检索）只接受 memory 条目集合，不接受任意路径——从类型上
杜绝"用向量检索读任意文件"的扩散。

### 3.2 向量基础设施（graph role）

- embedding 来源：**graph 向量 role**（accounts roles 新增 `graph` 角色，
  专用向量模型账号）——memory 是其第一个消费者，后续 code graph 复用。
- 无 graph role 可用时：`Remember` 仍可写（条目入存储），`Retrieve`
  返回明确的"向量索引不可用，请配置 graph role"错误——**不降级硬读**。

### 3.3 索引存储与失效

| 项 | 设计 |
|---|---|
| 存储 | 项目级 `.seelex/memory/vectors.json`（条目 id → embedding + chunk 映射），用户级同构 |
| 构建 | Remember 时增量（单条 embedding）+ 启动时校验（条目数 vs 索引数） |
| 失效 | 条目删除 → 索引同步删；`Reindex` 全量重建（版本号 + 内容 hash，与 project_refresh 同构） |
| 规模 | 千级条目暴力相似度（O(n·d)）足够；万级再引入 ANN（hnsw 等，第二阶段） |

## 4. 检索细节

```
相似度 = 余弦（query_embedding, entry_embedding）
排序   = 1) name 精确匹配（query == name → 恒召回，排最前）
         2) 其余按相似度降序
limit  = 默认 5，上限 20（limits 配置）
返回   = [{name, description, type, snippet, score}]
```

- chunk：条目正文按标题/段落切分（≤ 512 tokens/块），检索命中返回
  块级 snippet。
- query 为空：返回最近更新的条目（按 updated_at）——不触发向量检索。

## 5. 与通用文件工具的隔离（明确不做什么）

| 能力 | read_file/grep/glob（通用） | read_memory（向量） |
|---|---|---|
| 读任意路径 | ✅ | ❌（只读 memory 条目） |
| 关键词搜索 | ✅ grep | ❌（向量语义检索） |
| 向量检索 | ❌ 不提供 | ✅ 唯一路径 |
| 封装 | 公开工具 | 私有 Retriever，仅 memory 内部 |

通用文件工具**永远不获得**向量检索能力——向量检索是 memory（及未来
code graph）的专有读取形态，避免把文件系统抽象成"可向量化的通用资源"
（那会把 embedding 成本扩散到所有读路径）。

## 6. 实施步骤

1. `internal/memory` 包：MemoryStore（CRUD）+ Index（MEMORY.md 解析）
2. `retriever.go`：Embedder 接口 + 索引构建/检索/落盘（unexported）
3. `remember_memory` / `read_memory` / `forget_memory` 工具注册
   （read_memory handler 只调 `Memory.Retrieve`）
4. graph role：accounts roles 新增 graph 角色（向量模型账号）
5. 装配：会话开始加载 memory-index PromptBlock（索引条目，非向量）
6. 测试：确定性 embedding 桩（脚本化向量）→ top-k/精确名/无索引错误
7. 冒烟：read_memory 真实检索阶段（graph role 可用时）

## 7. 一句话

**Memory 是 seelex 第一个全向量化文件读操作**：读取唯一走私有向量
Retriever（无 grep/glob 回退、不对外公开），embedding 由 graph role 提供，
为 code graph 的向量检索立样板。
