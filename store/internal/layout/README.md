# store/internal/layout

## 生态位

JSON v8 会话布局机制 owner：物理路径、guide、module head 信封/校验和，以及按会话
与模块分片的锁身份。唯一调用方是根 `store`；领域重建策略仍留在拥有数据的 store
领域代码。

## 职责与非职责

- 职责：稳定路径派生、guide 幂等创建、head 原子发布/严格读/宽容读、锁一对一。
- 非职责：解释 message/event/compact payload、按数据文件重建领域 head、决定模块
  是否启用，或读写用户内容。

## 文件索引

- `layout.go`：`Scope`、`ModuleID`、`Guide`、`Head`、`Layout` 及路径/锁/head 原语。
- `layout_test.go`：路径、guide wire、head checksum/payload 与锁身份。

## 数据流

领域调用方持对应模块锁 → 先把不可变数据写到物理通道 → `Publish` 编码 head 信封、
计算 checksum 并原子替换 `metadata/<module>.json`。读者经 `ReadHead` 做共享读并
校验 schema/session/module/checksum；校验失败后的领域重建仍由调用方编排。

## 依赖方向

只依赖标准库与 `store/root`。禁止 import 根 `store`、任何领域包、application、
middle、backend 或 wiring。

## 兼容约束

`project-<hash>/session-<hash>/metadata/<module>.json`、guide/head JSON 字段、
checksum 算法、锁粒度和原子发布方式必须逐字保持。模块 ID 只是元数据名字，不能
改变物理子目录。

## Review 指南

检查同一模块是否始终得到同一锁、不同模块是否绝不别名、未知根模块是否在根适配层
显式失败、head 是否只在数据完成写入后发布，以及布局包是否开始解释领域 payload。

## 验证

```text
go test ./store/internal/layout ./store/... -run 'Head|Lock|Atomic|Pending|Guide' -count=1
```
