# 前置审查报告

## 需求摘要

将会话历史的读、写、列表和删除收敛为原子仓储接口；提供 JSON、SQLite 与外部数据库三种可装配 backend，并在 GUI 右上角设置中配置及切换。

## 影响文件清单

| 文件路径 | 修改类型 | 修改原因 |
|---|---|---|
| `seelebridge/storage.go` | 重构 | 当前包装 Seele `FileStore`，需升级为统一的原子 session repository。 |
| `seelebridge/session_repository_*.go` | 新增 | JSON 分片、SQLite、PostgreSQL backend 和 factory。 |
| `session/manager.go` | 修改 | 所有保存/读取经 repository 接口，支持运行时原子切换。 |
| `main.go` | 修改 | 加载持久化配置，创建 repository，并以 manager 注入方式装配。 |
| `application/ports.go`、`application/app.go` | 修改 | 暴露会话存储配置读取、测试与切换端口。 |
| `application_adapters.go` | 修改 | 连接 session manager 与应用端口。 |
| `gui/bridge.go` | 修改 | 向 Wails bridge 暴露 session storage 设置 API。 |
| `gui/frontend/dist/index.html`、`app.js`、`styles.css` | 修改 | 右上角设置入口及 JSON/SQLite/PostgreSQL 配置表单。 |
| `*_test.go` | 新增/修改 | 验证读写原子性、分片一致性、SQLite 与配置切换。 |

## 依赖分析

- 上游：Seele 的 `types.Message` / `SessionMeta`；现有 nested workspace 路由。
- 下游：`session.Manager`、application session port、GUI bridge 和主程序装配。
- 外部依赖：采用纯 Go SQLite driver（避免 CGO）和 PostgreSQL driver；数据库 DSN 只保存到本机配置，不返回给 GUI snapshot 或日志。

## 风险评估

- 运行中切换 backend 若旧历史未迁移会导致会话列表改变；默认策略应为“先测试连接，切换后仅新写入使用新 backend”，迁移必须显式执行。
- 数据库连接字符串含密码，前端读取设置时必须掩码；持久化配置文件权限须尽量限制。
- JSON 分片必须以 manifest/版本指针提交，读取先固定 manifest，避免看到跨版本 shard 混合。

## 建议方案

定义 `AtomicSessionRepository`，以 `(projectID, sessionID)` 为 key，提供 `Read`、`ReadRange`、`WriteAtomic`、`List`、`Delete`、`Ping`、`Close`。JSON backend 采用 generation + 原子 manifest rename；SQLite 和 PostgreSQL 以事务 upsert 单份序列化 history，统一复用 metadata 算法。`session.Manager` 是唯一调用点；GUI 仅调用 application 的配置/测试接口。

默认外部数据库类型按 PostgreSQL 实现；SQLite 使用本地 `.seelex/sessions.db`。
