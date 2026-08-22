# internal/limits

## 生态位

进程级运行时上限（seele.yaml limits 段）叶子包：`Apply`/`Get`。根包
`ApplyLimits`/`Limits` 门面与域包统一经此读取，避免根包导入环。

## 测试

无独立测试；由 core 根包测试覆盖。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### limits.go

- `func Apply(l seelexctx.Limits)` — Apply 应用 seele.yaml limits 段（零值字段自动补默认）。
- `func Get() seelexctx.Limits` — Get 返回当前生效的运行时上限（只读拷贝语义）。

