# internal/state

## 生态位

`application/core` 的共享状态内核（叶子包）：唯一共享锁 `Mu`、权威前端
快照 `Snapshot`、外部端口依赖 `Deps`、事件 `Events` 与审批 `Approval`。
core 根门面与各域协调器嵌入 `*state.Core`，避免域间直接持有彼此实现。

## 职责与非职责

- 做：内核构造（`New`）与状态集中。
- 不做：任何域业务逻辑；不 import 任何域包。

## 并发/安全语义

`Mu` 是唯一共享锁；持锁时禁止调用外部端口（I/O、LLM、数据库）；快照
revision 保持「锁内 bump → 锁外 Publish」。

## 测试

无独立测试；由 core 根包测试覆盖。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### state.go

- `func New(deps contract.Dependencies) *Core` — New 构造共享状态内核。deps 必须是装配根已补齐默认值后的依赖集合；

