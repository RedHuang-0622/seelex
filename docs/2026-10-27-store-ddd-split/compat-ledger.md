# Store DDD 兼容台账

日期：2026-10-28
适用范围：M10 完成后的根 `store` 兼容面。

机器 ratchet：

- God 方法集合：`store/testdata/god_type_methods.json`
- 根 `store` 生产 importer：`store/testdata/m10_root_store_import_allowlist.json`
- 门禁：`e2e/store_ddd_m10_facade_gate_test.go`

## 已移除能力发现口

| 旧面 | 新面 | 首调用方 | 删除条件 | 归零验证 |
|---|---|---|---|---|
| `Router.BoardsFor` | `store.Stores.Boards` | `wiring/bootstrap` | M10 已删除 | M10 Facade Gate |
| `Router.BoardsForSession` | `store/board.SessionStore.Bind` + Runtime 会话绑定 | `wiring/adapters` | M10 已删除 | M10 Facade Gate + goal board tests |
| `Router.TeamworkFor` | `store.Stores.Teamwork` → `teamwork.planStoreRepository` | `wiring/bootstrap` | M10 已删除 | M10 Facade Gate + teamwork tests |
| `Router.ToolRecordFor` | `store.Stores.ToolRecords` → bootstrap `toolRecordStore` | `wiring/bootstrap` | M10 已删除 | M10 Facade Gate |
| `MediaStoreOf` | 消费方 `multimodal.MediaReader` / 装配期注入 | `backend/tool/io/multimodal` | M10 已删除 | M10 Facade Gate |
| `Router.WriteMediaWorkspace` / `ReadMediaWorkspace` | `Stores.Media`（`artifact.SessionStore`） | Runtime media Host | Runtime 显式注入；根 media 文件已删除 | `store/media_session_test.go` + media tests |
| `store.NodeSessionStore` + 根 workunit 类型 alias | `Stores.WorkUnits` / `workunit.SessionStore` | Assembly、Subagent、WorkUnit | 根 wrapper 已删除 | WorkUnit compile assertions + role/workunit gate |
| `store.RolePermissionStore` + constructor | `Stores.RolePermissions` / `role.PermissionStore` | Session Host | Host 显式注入，根 permission adapter 已删除 | role permission tests |
| `Router.Read/Write/RemoveTeamRegistryWorkspace` + 根 alias | `store/role.Store` | legacy 诊断 | 根 `team_registry.go` 已删除 | registry owner tests + Teamwork I/O gate |

## 仍保留的根包兼容面

`store.Stores` 是 typed 组合值对象，不提供运行期探测或万能接口。以下根包符号仍被
19 个生产文件直接消费：

- `Router` 的会话/context/项目兼容 API 与生命周期；
- `SessionGranularStore`、`DurableHistory`、`SessionContextStore`；
- `ProjectRecord` 与 project knowledge 兼容出口；
- `Event`/`ToolResult`/`Commit` 等稳定 DTO 或迁移期同身份 alias；
- teamwork 类型 alias 仍供未迁移测试或兼容点使用。

| 旧面 | 当前唯一 owner / 新面 | 删除条件 | 回退方式 |
|---|---|---|---|
| 根 teamwork 类型 alias | `store/teamwork` + `contract/dto` | 所有生产调用点直接 import owner | 恢复同身份 alias |
| 根 workunit 会话根适配器 | `store/workunit.SessionStore` + `Stores.WorkUnits` | Session engine 下沉后直接提供根路径端口 | 恢复 `workunit_roots.go` |
| `SessionGranularStore` | Session owner + 应用层端口 | 应用/适配器全部改用窄端口 | 恢复组合根注入 |
| 根 project knowledge API | `store/project`（Router 上仍有生命周期入口） | M11 后续 Router 方法收口 | 保留只读兼容 |

## 数据与回滚

- 兼容面只改变依赖方向与装配方式；路径、JSON tag、head、watermark、schema 零变化。
- 删面回滚恢复上一版 alias/薄转发，不删除或重写用户会话文件。
- M10 外部批次 checkpoint：`f49f4ec791957d8e581d3f3874580511c84848c1`。
- replay 分支：`codex/store-ddd-M10-replay`；apply 后 build/门禁通过，rollback 后工作树零差异。
