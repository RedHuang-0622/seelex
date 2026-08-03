# GUI / Agent Workbench 设计变更记录

本文件记录会改变模块边界、跨模块契约、兼容性、持久化或运行流程的重要设计。纯文字修正不记录。

## 2026-07-24

### Added

- 建立 `architecture.md` 作为 GUI/Agent Workbench 权威总体架构入口。
- 建立机器可读 `module_dotting.json`，登记模块状态、职责、接口、实现路径、输入输出和依赖。
- 冻结 protocol v2 的 Snapshot、Event、Page、Error、Card 和 Generation Manifest JSON Schema。
- 增加与 Schema 对应的可执行示例和 Go 契约测试。
- 定义规划中的 HTTP API、安全、分页、错误、幂等、条件请求和 Snapshot 语义。
- 定义 generation 提交、回滚、重建和故障恢复 recipes。
- 增加 Generation Repository 与 HTTP API Adapter 模块详细设计。
- 增加证据门禁驱动的需求到 Dev 自迭代模块、运行 recipe、Evidence Assessment 与 Dev Iteration Schema/示例。

### Changed

- `docs/arch/agent-workbench-architecture.md` 降为方案推演材料；字段、依赖和发布语义以 `docs/gui/` 为准。
- 规划模块的总体架构链接统一指向 `docs/gui/architecture.md`。
- 明确当前实现仍为 protocol v1/单 Engine；v2/多 SessionActor/HTTP/generation repository 均为规划状态。

### Design decisions

- Event sequence 改为 per-scope，不采用全局高频序列。
- 大型 Workspace/历史数据使用 cursor Query Page，不进入 Workbench Snapshot。
- generation 采用不可变资源目录、manifest hash 和原子 current 指针，不允许原地覆盖。
- HTTP 与 Wails 是并列 adapter，共享 Application ports，但不共享 transport DTO。
- 模块依赖必须为 DAG，并纳入自动化验证。
- RAG 从辅助上下文升级为工程证据获取机制；在线使用 evidence readiness，低证据条目不删除，E2E 反馈按需求/架构/详设/Dev/Test 层精确重开。
