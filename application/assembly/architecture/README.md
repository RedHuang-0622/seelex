# application/assembly/architecture

## 生态位

`application/assembly/architecture` 是 assembly 的结构门禁测试包。它读取父目录的
Go 源码并验证组件边界，不参与生产装配。

## 职责与非职责

- 做：禁止自由 `newService*` 构造器、组件转发 getter、单调用 facade 和退场宽接口；
- 做：验证 Conversation turn 运行时与 ChatState/request 规则没有复制回 assembly；
- 不做：启动 Service、访问运行态或复制业务实现。

## 目录结构

- `component_getter_boundary_test.go`：组件 getter、inbound contract、单调用转发门禁。
- `service_constructor_boundary_test.go`：组合根构造函数门禁。
- `conversation_turn_boundary_test.go`：Conversation turn 规则归属门禁。

## 核心实现

测试通过 `go/parser` 扫描父目录生产文件，路径固定为 `..`，不依赖测试进程的工作目录名称。

## 数据流与依赖

测试 → 父目录 `.go` AST → 结构断言。只依赖标准库和 `application/assembly` 源码。

## 并发与错误

只读扫描；解析或目录读取失败立即以测试错误返回。

## 扩展方式与 Review

新增结构门禁放本包；涉及运行态、锁序或真实端口交互的测试仍留父包集成测试。

## 测试与验证

```text
go test ./application/assembly/architecture -count=1
```
