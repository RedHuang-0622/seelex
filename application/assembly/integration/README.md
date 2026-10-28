# application/assembly/integration

## 生态位

`application/assembly/integration` 承载必须经过真实 assembly 组合图、但不需要访问
父包私有字段的跨 owner 集成测试。生产代码不依赖本包。

## 职责与非职责

- 做：在测试包内声明 fake ports/依赖变量，构造真实 Service，验证公开
  `ComponentsFor` owner 的协作、事件和错误语义；
- 不做：访问 `service.components`、复制生产状态机或把测试专用接口导出到生产包。

## 目录结构

- `fakes_test.go`、`helpers_test.go`：本测试包私有的 fake ports 与 Service harness；
- `runtime_extra_test.go`：fake Runtime 的会话级项目根能力；
- 其余 `*_test.go`：已迁出的 account、history search、message sequence、notice、
  agent-team fixture、close/shutdown、fault guard、history safety、hot attach、
  context/compaction、plugin source projection、review files、role tool activity、
  role attribution、service/session lifecycle、session history pagination、
  snapshot budget、tool hook、task skill、turn status、session snapshot、
  workspace use cases 等集成用例；
- `legacy_service_test.go` / `context_legacy_helpers_test.go`：仅测试包私有的
  owner 映射与上下文夹具，不作为生产契约。

## 核心实现

测试通过公开的 `assembly.New` 构造 Service，通过 `ComponentsFor(service)` 取得稳定
owner 指针。依赖缺失或类型变化在测试包内修复，不改生产 struct 形状。

## 数据流

本地 fake ports → assembly composition root → `ComponentsFor` owner → 断言/事件。

## 依赖方向

允许依赖 `application/assembly` 的公开面和 contract；禁止导入父包未导出符号，
禁止生产代码反向依赖本测试包。

## 并发与错误

fake Runtime/Engine 按其 public 端口并发语义加锁；测试 harness 在 cleanup 中通过
公开 Lifecycle owner 关闭 catalog/worker。

## 扩展方式

新增依赖优先声明为测试包私有 fake 或通道/recorder 变量，再注入 `Dependencies`。
只有当测试确实不可表达时才扩展 `ComponentsFor` 的真实 owner 指针。

## Review 指南

- 是否只通过公开 API 和 owner 指针访问？
- fake 是否在测试包内，而不是生产包？
- 是否复制了生产判据或重新引入 harness 结构耦合？

## 测试与验证

```text
go test ./application/assembly/integration -count=1
```
