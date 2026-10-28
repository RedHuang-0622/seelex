# internal/stablehash

## 生态位

`internal/stablehash` 是仓库内的稳定哈希原语叶子包。它只回答"同一输入是否得到
同一稳定哈希"，不做加密、不做业务映射；调用方是 `backend/tool/node`（节点会话号）、
`backend/engine/account`（账号池定序）、`backend/tool/registry`（员工权限主体名）
与 `backend/tool/task`（goal task key，64 位）。

## 职责与非职责

职责：

- 提供唯一的 FNV-1a 32/64 位实现 `FNV1a32` / `FNV1a64`。
- 用已知向量钉住算法输出，防止无意换算法。

非职责：

- 不决定哈希输入怎么拼、结果怎么用；那属于各调用方的语义契约。
- 不提供抗碰撞保证，不能用于安全场景。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `fnv.go` | `FNV1a32` / `FNV1a64` 唯一实现 |
| `fnv_test.go` | 32/64 位已知向量（空串 / `a` / `foobar`） |

## 核心实现

`FNV1a32` 直接用标准库 `hash/fnv` 的 `New32a`，`FNV1a64` 用 `New64a`；
两者都是逐字节 XOR 后乘 FNV 质数。
输出是**持久兼容面**：节点会话号、账号绑定与权限主体名都依赖它；换算法必须
同时迁移这些持久键。

## 数据流或生命周期

调用方传入字符串 → 本包返回 `uint32`。无状态、无缓存、无 goroutine。

## 依赖方向

只依赖 Go 标准库。任何 Seelex 包都可以依赖它；它不反向依赖任何 Seelex 包。

## 并发、存储、安全或错误语义

纯函数、并发安全；不读写文件、不持有锁。32 位哈希可碰撞，只用于确定性分桶/
命名，不用于完整性或安全校验。

## 扩展方式

需要新的稳定哈希算法时，新增独立函数与已知向量；不要修改 `FNV1a32` 的输出，
除非同时提供持久键迁移方案。

## Review 指南

- 是否又出现了第二份 FNV-1a 实现？
- 调用方是否误把 32 位哈希当安全哈希？
- 改算法时是否评估了 nodeID / 账号绑定 / 权限主体名的迁移？

## 测试与验证

```text
go test ./internal/stablehash/
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### fnv.go

- `func FNV1a32(s string) uint32` — FNV1a32 返回 s 的 FNV-1a 32 位哈希。
- `func FNV1a64(s string) uint64` — FNV1a64 返回 s 的 FNV-1a 64 位哈希。它服务 task key 等更长的稳定标识；

### fnv_test.go

- `func TestFNV1a32KnownVectors(t *testing.T)` — 已知向量钉住算法：这些值变了，node 会话号/账号绑定/权限主体名都会失配。
- `func TestFNV1a64KnownVectors(t *testing.T)`
