# Assembly Tests + Facade Retirement Handoff

日期：2026-10-28
状态：已完成

## 结果

- `application/assembly` 根目录不再包含 `_test.go`；
- 根测试预算：0 files / 0 lines；
- assembly 生产预算：49 files / 6,467 lines；
- 全部测试预算：0 files / 0 lines，不再统计父包根测试密度；
- `wiring/` 已删除，全仓 Go import 中 `seelex/wiring` 为 0；
- 未 commit/push；
- 未触碰 store 并行会话的未提交改动。

## 测试归属

- 公开 API：`application/assemblytest`；
- AST 与预算门禁：`application/assembly/architecture`；
- 跨 owner 真实装配行为：`application/assembly/integration`；
- 纯 owner 判据：`application/*` 与 `kernel/*` 的 owner 包。

integration 测试通过包内 fake、helpers 和 `ComponentsFor` 访问稳定 owner，
不依赖父包私有测试字段。最终批次中：

- 17 个根测试迁入 integration；
- `session_decoupling_test.go` 改成 architecture AST 门禁；
- 根 `service_fakes_test.go` / `service_helpers_test.go` 与 integration 包内副本
  去重后删除。

## 最终验证

```text
go test ./application/assembly/integration -count=1 -timeout=180s
go test ./application/assembly/architecture -count=1 -timeout=120s
go test ./application -run '^TestApplicationArchitectureBudget$' -count=1 -v
```

结果：

```text
integration: ok
architecture: ok
application budget: ok
assembly root tests: 0
seelex/wiring imports: 0
```

删除与迁移明细以
[`../2026-10-27-assembly-slimming/deletion-list.md`](../2026-10-27-assembly-slimming/deletion-list.md)
为准。

## 后续边界

Qoder handoff 不再需要继续执行。后续变更若重新在 assembly 根目录增加测试，
预算门禁会立即失败；若重新引入 `wiring` facade，assembly architecture 门禁会
失败。
