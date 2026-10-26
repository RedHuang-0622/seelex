# application/reference

## 生态位

`application/reference` 承载 `read_tool_result`、`read_plan` 与
`read_compressed_turn` 的只读应用用例。
主要调用方是 `application/core` 的兼容 `Service` 入口、模型工具注册和 GUI 工具结果
详情。归档、Engine 节点结果、Task Plan 栈和 Session 持久化仍由各自既有模块拥有。

## 职责

- 校验引用工具参数并应用分页默认值/上限。
- 解析子代理节点引用与 `result:call_*` 别名。
- 按 `contains` 过滤并返回 `model.ToolResultPage`。
- 读取权威 Plan frame，并按请求筛选 canonical 节点。
- 读取压缩轮次原文，渲染角色标记并做字符分页。

## 非职责

- 不保存工具结果或 Plan。
- 不拥有 Engine、Task、Session 状态。
- 不导入 `application/core`，不持有完整 `Service`。

## 文件结构

- `application.go`：`Deps`、`Application`、构造器和引用/Plan 用例。
- `compressed_turn.go`：`read_compressed_turn` 参数、归档读取、渲染与分页。
- `application_test.go`：分页上限、别名解析和 Plan 节点筛选。
- `compressed_turn_test.go`：压缩段作用域、过滤、上限、截断与角色渲染。

## 核心实现

`Deps` 只接收实际使用的窄能力：分页配置、节点结果、别名解析、结果读取、
当前 Plan ID、Plan frame，以及压缩轮的当前视图作用域、归档读取与 ref 前缀。
`Application` 不缓存任何结果；同一请求始终读取底层唯一事实源。

## 数据流

`Service` 兼容入口 → `Application` → 注入的归档/Plan 端口 → Engine、Task 或
SessionStore。压缩轮写路径仍由 `CompressedTurnArchiver` 负责，读路径经同一
ToolResults 通道按会话/项目显式作用域回读。组件只做请求塑形和分页，不执行
I/O 写入。

## 依赖方向

允许依赖 `backend/tool/reference` 的纯分页判据和 `middle/protocol/model`。
禁止依赖 `application/core`、UI、装配根或具体存储实现。

## 并发与错误

- 组件自身无状态锁；调用方负责让注入回调保持原锁边界。
- 参数错误、未知节点/引用、归档存储错误和损坏 Plan 均显式返回。
- 查询路径不获取 `application/core` 的共享写锁。

## 扩展方式

新增引用读取能力时先扩展 `Deps` 中真实调用方的窄能力，再实现用例；不得把
`Application` 扩成完整会话门面。

## Review 指南

- 分页默认值/上限是否仍复用 `Limits().ReferencePageSize` 等唯一来源。
- 别名是否先解析再读 archives。
- 子代理节点引用是否优先于普通归档。
- Plan 缺省 ref 是否仍取当前权威 Plan。
- 压缩轮是否同时使用当前会话与显式 workspace 作用域。

## 测试与验证

```text
go test ./application/reference ./application/core -run 'Reference|CompressedTurn|Assistant|Plan' -count=1
go test ./application/... -count=1
```
