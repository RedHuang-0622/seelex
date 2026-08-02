# 项目文件语义注册方案（Project File Semantics）

> 状态：规划
> 日期：2026-08-02
> 目标：项目级（跨会话通用）文件/目录语义知识注册与查询；为后续
> 向量化 code graph 预留数据面。

## 1. 背景与现状

- 已有 `project_refresh` 工具 + `sessionstore.ProjectRecord`：扫描模块文档目录 +
  `module_dotting.json` + 可选 `seelex.project.md`，构建**模块级**语义知识
  （项目级存储、内容 hash 版本化、跨会话共享）。
- 缺口：模块语义是**扫描式**（从文档推断），缺**任务沉淀式**——agent 在真实
  任务中读懂了某个目录/文件（如"application/core 是服务门面层"），这个理解
  没有回写机制，下次会话还要重新读。
- 目标场景：agent 读文件 → 把文件夹级含义写入项目级 JSON → 后续任何会话
  （含子代理）可快速检索，不用重读全文。

## 2. 数据面设计（项目级通用，非会话级）

### 2.1 语义注册表（SemanticsRegistry）

存于项目记录（复用 `ProjectRecord` 通道或新增同构记录），按 projectID 寻址：

```json
{
  "schema_version": 1,
  "project_id": "seelex",
  "updated_at": "2026-08-02T10:00:00Z",
  "entries": [
    {
      "path": "application/core/",
      "kind": "directory",            // directory | file
      "meaning": "应用核心用例编排层：Service 门面 + 组件装配",
      "responsibilities": ["会话生命周期", "终态判定", "上下文预算"],
      "key_terms": ["task_complete", "resumeSession", "react_budget"],
      "relations": [],                 // 预留：depends_on / imports（code graph 种子）
      "recorded_by": "session-abc",
      "recorded_at": "2026-08-02T09:58:00Z"
    }
  ]
}
```

要点：
- **项目级**：按 `(workspace_id, project_id)` 存储，跨会话、跨子代理共享；
  不挂会话 ID。
- **增量 + 去重**：同一 path 重复注册合并（语义更新覆盖，key_terms 并集）。
- **来源可溯**：`recorded_by` 记录沉淀会话（审计/回滚依据）。
- **大小有界**：单条目 meaning ≤ 500 字符、responsibilities ≤ 10 项、
  总条目 ≤ 500（防注册表膨胀；超出按 LRU 淘汰最旧条目）。

### 2.2 与现有 ProjectRecord 的关系

| 现有 | 新增 |
|---|---|
| `project_refresh`（扫描式，文档/元数据推断） | `register_semantics`（沉淀式，任务中回写） |
| 模块级（module_dotting） | 路径级（任意目录/文件） |
| 只读消费（Assembler 项目块） | 读写双通道 |

两者共存：扫描式为基线，沉淀式增量补充；聚合时后者优先。

## 3. 写入路径

### 3.1 `register_semantics` 工具（新）

- 输入：`path`（必填）、`meaning`（必填）、`kind`、`responsibilities[]`、
  `key_terms[]`
- 校验：path 必须位于项目作用域内（ProjectScope 路径校验，防逃逸）；
  meaning 长度限制
- 行为：读现有注册表 → 按 path 合并（语义更新 + key_terms 并集）→
  写回项目记录 → 触发快照事件（前端可选展示）
- 权限：写操作走 permission 门控（默认 ask，与 edit_file 同级）

### 3.2 沉淀时机（提示词引导）

- 系统提示新增规则：**完成一个目录/文件的深入理解后，用
  `register_semantics` 记录路径级含义**（每任务 ≤ 3 条，防刷屏）
- 与 `project_refresh` 区分：refresh 重建模块基线；register 沉淀任务理解

## 4. 读取路径

### 4.1 `read_semantics` 工具（新）

- 输入：`query`（路径前缀或 key_term 关键词）、`limit`
- 行为：项目级注册表检索 → 返回条目（meaning/responsibilities/key_terms）
- 用途：新会话/子代理快速定位"某目录是干什么的"，减少重读全文

### 4.2 会话装配注入

- Assembler 项目块扩展：注册表条目数 < 20 时全量注入；否则按当前
  workspace 路径前缀注入相关条目（预算内）

## 5. 与子代理闭环的关系

- 子代理（plan_run kind:agent）可见 `register_semantics`/`read_semantics`
  （nodeScopeToolVisible 白名单追加）——子代理在片段任务中的理解同样沉淀，
  回传合并（merge-back）后主会话可引用。
- 项目级存储天然跨会话：注册表写入不依赖会话生命周期。

## 6. 未来：向量化 code graph

预留数据面，不在本期实现：

1. **schema 预留**：`relations` 字段（depends_on/imports/calls）作为 graph
   边种子；`kind` 区分 directory/file/module。
2. **向量 role**：新增角色 `graph`（专用模型账号，见 accounts roles）——
   定时/增量把注册表条目 + 文件内容 embedding，构建项目级 code graph；
   `read_semantics` 升级为语义检索（vector 相似度 + 关键词混合）。
3. **graph 存储**：项目级（与注册表同构），可导出为 JSON（下游 IDE 插件/
   MCP 消费）。

## 7. 实施步骤（建议顺序）

1. `SemanticsRegistry` 数据模型 + 项目级存取（sessionstore 扩展）
2. `register_semantics` / `read_semantics` 工具注册（scoped 校验 + 权限门控）
3. 提示词规则 + harness 用例（沉淀时机）
4. Assembler 注入 + 前端展示（可选）
5. 子代理白名单 + 端到端测试
6. （后续）向量 role + code graph

## 8. 风险与边界

- 注册表污染：meaning 质量依赖模型判断 → 长度/数量上限 + 可覆盖可删除
  （`clear_semantics(path)` 预留）
- 路径逃逸：全部走 ProjectScope 校验
- 与现有 module_dotting 重复：注册表优先于扫描基线，冲突以注册表为准
