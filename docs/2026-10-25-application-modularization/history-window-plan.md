# History Window 端口化方案

## 设计目标

在不改变分页、接缝、窗口坐标和锁顺序的前提下，删除 core 对
`middle/switch/history` 的生产 import。

## 方案对比

| 维度 | A：`application/history.WindowAssembler` | B：把窗口安装移入 `application/history` |
|---|---|---|
| 耦合度 | 低：端口只暴露六个纯函数 | 中：Application 需要扩展窗口读写 host |
| 内聚性 | 高：Middle 保留唯一窗口判据 | 中：分页决策与锁内安装跨层重新切分 |
| 可测试性 | 高：断言与 adapter parity 独立 | 中：每次测试需重演视图锁和宿主写面 |
| 锁风险 | 低：原有锁结构逐字保留 | 高：需要重设读取、计算、写入的锁边界 |
| 改动面 | 小：参数与构造注入 | 大：涉及 Snapshot 安装、revision 与发布 |
| 可回滚性 | 高：恢复 core import 与旧构造即可 | 低：多处编排变化需整体回退 |

## 推荐：方案 A

### 端口

```go
type WindowAssembler interface {
	ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int)
	Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int)
	DurableRows(messages []model.Message) []model.Message
	IndexOfSameRow(rows []model.Message, want model.Message) int
	ContainsSameRow(rows []model.Message, want model.Message) bool
	HasRowIdentity(message model.Message) bool
}
```

后两个方法由真实 core 调用点推导，不能省略。

### 所有权

- `application/history`：分页顺序和 `Deps.WindowAssembler`；
- `middle/switch/history.Assembler`：窗口判据的唯一实现；
- `application/core`：锁内读/写、Snapshot、revision、事件发布；
- `wiring/adapters`：把 Middle Assembler 作为端口注入 core 构造；
- `wiring/constructors`：唯一组合根注入点。

### 实施

1. 新增 core import 红测和 adapter parity 红测；
2. 在 `application/history` 定义六方法端口；
3. Middle 新增 `Assembler`，逐项转发原函数；
4. core 构造器接收 assembler，回调使用端口；
5. Wiring 注入 `middle/switch/history.Assembler{}`；
6. 删除 core 的 Middle import，收紧 Application 依赖预算；
7. 更新 README 与兼容台账。

## 测试

- adapter 与原函数同输入逐字段一致；
- 接缝对不上时 window/start 不动；
- 可见总数不倒退；
- 空占位不参与内容对位；
- 冷读分页测试证明 `ViewMu` 未跨磁盘读持有；
- `-race` 与完整 Application/Core 回归。

## 回退

恢复 `session_history.go` 的 Middle import 和 core 内部装配；保留 Middle
`Assembler` 不会改变行为。端口/adapter 无持久化或状态，可独立回退。
