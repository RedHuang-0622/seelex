# store/teamwork

## 生态位

Teamwork plan、append-only audit event 与 Work Item binding 的持久化领域包。

## 职责与非职责

- 职责：稳定 wire、plan/work-item DAG 校验、append-only binding 折叠。
- 非职责：worker 执行、job 调度、整队收口用例、看板投影或跨域事务。

## 依赖方向

只依赖标准库与 `contract/dto`；禁止 import 根 `store`、Application、Middle、
Backend，也禁止 import board/teamwork 投影实现。

## 数据兼容

Plan/Event/Binding 的 JSON tag、文件名与路径由存储适配层保持原样；本包只拥有
形状与纯判据，不创建第二份落盘。

## 测试

```text
go test ./store/teamwork -count=1
```
