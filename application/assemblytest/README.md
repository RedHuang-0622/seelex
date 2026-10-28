# application/assemblytest

## 生态位

`application/assemblytest` 是从 `application/assembly` 外移的公开边界测试包，
主要调用方是仓库测试入口。它只通过 assembly 的公开构造函数、错误和组件图验证
稳定 API，不访问包内状态。

## 职责

- 验证 assembly 公开构造缺少依赖时显式失败；
- 验证 `ComponentsFor(nil)` 的零值图语义；
- 为后续可公开测试的 assembly 边界提供不污染生产目录的落点。

## 非职责

- 不测试 assembly 内部锁、字段或未导出状态；
- 不持有生产 Service、fake 或 fixture；
- 不复制组合根实现。

## 目录结构

- `public_api_test.go`：公开构造函数与组件图边界。

## 核心实现

测试以 `package assemblytest` 导入 `application/assembly`，生产包只暴露既有公开面。

## 数据流

测试 → assembly 公开 API → 返回结果/错误。

## 依赖方向

只依赖 `application/assembly`、`contract` 与标准库。禁止反向导入测试包。

## 并发与错误

本包不保存状态；错误断言只覆盖公开构造和零值图。

## 扩展方式

新增测试必须只使用公开 API。需要包内状态时，应迁到对应 owner 包或留在 assembly
的 `_test.go` 集成测试中，不得通过导出生产内部来迁测试。

## Review 指南

确认测试没有通过 `unsafe`、反射写包内字段或复制 Service 内部图。

## 测试与验证

```text
go test ./application/assemblytest -count=1
go test ./application/... -skip TestApplicationCompatibilitySurfaceIsRetired -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### public_api_test.go

- `func TestPublicConstructorRejectsMissingDependencies(t *testing.T)`
- `func TestComponentsForNilReturnsEmptyGraph(t *testing.T)`
