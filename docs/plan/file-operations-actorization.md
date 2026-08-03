# 文件操作 Actor 化清单

> 状态：规划（第一步：清单）
> 日期：2026-08-03
> 目标：全部文件操作登记在册，明确 Actor 抽象的使用点——文件系统是
> 多 actor（主代理 / 并行子代理 / 后台任务）共享的资源，按 Actor 语义
> 应视为一个（或按路径分片多个）文件系统 actor：操作经消息队列串行化，
> 状态（写锁/变更通知）私有。

## 1. 文件操作全景

### 1.1 工具层（agent 可见，[scoped_tools.go](seelebridge/scoped_tools.go)）

| 工具 | 操作 | 读/写 | 并发风险 |
|---|---|---|---|
| `read_file` | 读文件（行范围） | 读 | 低（只读，可并行） |
| `grep_search` | 目录内容搜索 | 读 | 低 |
| `glob` | 文件名匹配遍历 | 读 | 低 |
| `write_file` | 创建/覆盖文件（`os.WriteFile` + `MkdirAll`） | **写** | **高：并行子代理写同一文件竞争** |
| `edit_file` | 读-替换-写回（`ReadFile` → `ReplaceAll` → `WriteFile`） | **读+写** | **高：读改写非原子，并行 edit 互相覆盖** |
| `bash` | 任意 shell 命令（cwd 约束项目内） | 读/写/执行 | **极高：不可控的外部写** |

写路径全部是**直接 OS 调用**（无锁、无队列、无变更通知）——两个子代理
并行 edit 同一文件时，后写者覆盖先写者（read-modify-write 竞态）。

### 1.2 存储层（sessionstore，各会话独立 key）

| 操作 | 位置 | 现状 |
|---|---|---|
| 会话历史/事件/工具结果读写 | sessionstore.go | **已 actor 化**：repository 内部锁 + generation 原子替换 + 每会话独立 key |
| 项目知识（ProjectRecord） | project_record.go | 项目级共享写（project_refresh / register_semantics）——多会话写需串行化 |
| 压缩归档（TurnArchiver） | compressed_turn.go | 走 SaveCommit（存储 actor）✓ |

### 1.3 应用自身文件操作（启动期/配置）

- 读 `seele.yaml` / `seelex.yaml` / `config/accounts.yaml`：启动期一次，无需 actor
- 写 `session-storage.json` / workspace 索引：存储 actor 覆盖 ✓

## 2. Actor 抽象使用点

### 2.1 FileSystemActor（核心，工具层写路径）

```
所有 write_file / edit_file / bash(写) 操作 → FileSystemActor 队列串行化
  ├─ 按路径分片：shard(path) → 每路径一个串行队列（不同文件并行、同文件串行）
  ├─ edit_file 的 read-modify-write 在队列内原子化（拿锁 → 读 → 改 → 写 → 释放）
  └─ bash 写命令：无法静态判定 → 保守按"整个项目一个写队列"或记录执行
```

**使用点**：
- `scopedWriteFile` / `scopedEditFile` / `scopedBash`（写路径）
- 子代理与主代理共用同一 FileSystemActor（跨 actor 文件互斥）
- 实现形态：`ContextExchanger` 同构——`FileSystem` 接口 + 队列 mailbox
  （可复用 actor.go 的模式：接口定义 + 实现注入 + 队列消费）

### 2.2 项目知识 actor（ProjectKnowledgeActor）

- `project_refresh` / `register_semantics` / `read_semantics` 统一走
  项目知识 actor（projectID 为 actor 身份，读写串行）
- 现状 ProjectRecord 存储已原子；缺的是**多会话并发写串行化**与变更通知

### 2.3 读路径（不需要 actor）

- `read_file` / `grep_search` / `glob`：只读，并发安全，保持直读（可加
  只读缓存 actor 做 read-through 优化，二期）

## 3. 优先级建议

| 阶段 | 内容 | 动机 |
|---|---|---|
| **P0** | FileSystemActor 写路径（write/edit 串行化 + edit 原子化） | 并行子代理写竞争是真实正确性风险 |
| P1 | bash 写命令纳入（保守全项目写队列或审计） | 不可控外部写 |
| P2 | 项目知识 actor（多会话写串行化） | 语义注册表并发 |
| P3 | 读缓存 actor（read-through） | 性能优化 |

## 4. 设计约束（复用 actor.go 模式）

- **接口 + 注入**：`FileSystem` 接口（Read/Write/Edit/Exec），Runtime 装配，
  工具 handler 经接口调用——与 `ContextExchanger` 同构
- **状态私有**：写队列（mailbox）在 FileSystemActor 内部，工具层不共享锁
- **队列消费**：与 `pendingSubagentContexts` 同模式（goroutine + channel）
- **测试**：scripted 确定性测试（同 subagent 模式）+ 并行写竞态测试
