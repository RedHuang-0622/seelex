# JSON 契约维护规则

本目录中的 JSON Schema 是 HTTP 和跨模块 payload 的必需交付物，不是示意文件。当前使用 JSON Schema Draft 2020-12，每个 Schema 必须有稳定 HTTPS `$id`，共享定义只从 `common.schema.json` 引用。

## Schema 与示例

| Schema | Payload |
|---|---|
| `snapshot.schema.json` | Workbench Snapshot |
| `session-snapshot.schema.json` | Session Snapshot |
| `event.schema.json` | scoped Event envelope |
| `page.schema.json` | Workspace query page |
| `error.schema.json` | API problem response |
| `card.schema.json` | conversation Card |
| `generation-manifest.schema.json` | committed generation manifest |
| `generation-operation.schema.json` | checkpoint/rollback request |
| `turn-request.schema.json` | submit turn request |
| `turn-accepted.schema.json` | accepted turn response |
| `interaction-resolution.schema.json` | approval/interaction resolution |
| `evidence-assessment.schema.json` | requirement claims、evidence bindings、readiness gate 与人工处置 |
| `dev-iteration.schema.json` | requirement→architecture→design→Dev→E2E run 与反馈路由 |
| `module-dotting.schema.json` | module registry |

对应 payload 位于 `../examples/`；`module_dotting.json` 位于上级目录。`docs_contract_test.go` 会拒绝未登记验证的示例。

## 兼容规则

- 可选字段新增通常向后兼容，但必须说明缺省语义。
- required 字段新增、字段删除/改义、enum 收窄、ID/seq/revision 语义变化是不兼容变更。
- 不兼容跨模块变更需要升级 `protocol_version` 或目标 Schema 的 `schema_version`，并写入 `../CHANGELOG.md`。
- `additionalProperties: false` 是外部契约默认值；真正需要扩展点时使用命名的 `metadata` object，不允许任意字段污染主结构。
- Schema 只能做结构验证；路径根限制、hash、授权、revision、cursor 签名和资源上限仍需语义校验。

## 修改流程

1. 修改 Schema 和 `$defs`。
2. 更新有效示例，并增加无效边界测试（实现阶段）。
3. 同步 API、模块详设、Go/JS DTO 和 reducer。
4. 更新 `CHANGELOG.md` 的兼容性说明。
5. 运行 `go test . -run '^TestGUI'`，随后执行完整测试与 race runner。
