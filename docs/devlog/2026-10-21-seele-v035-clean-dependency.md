# Seele v0.3.5：移除本地 replace，回到发布依赖

## 结论

Seele `v0.3.5` 已发布（tag 指向
`478f4d87b57660886a0b71d7f6f8d7b191cb4452`），Seelex 的临时本地依赖到此结束：

```text
github.com/RedHuang-0622/Seele v0.3.5
```

`go.mod` 不再包含 `replace github.com/RedHuang-0622/Seele => ...`；
`go.sum` 记录模块 zip 与 go.mod 校验值；本地 vendor 中的 Seele 版本与
`v0.3.5` 一致，`vendor/modules.txt` 没有 `=>` 行。

## 本版差异

`v0.3.4..v0.3.5` 的生产代码差异只在 `jobs/`：

- `job.go`
- `manager.go`
- `options.go`
- `namer.go`
- `builtin/manage.go`

同步的是“句柄命名由产品注入 Namer”这一组变更。Seelex 的团队作业表装配在新
版本上完成 `go build ./...`，GUI、应用层与工作区测试全绿。

## 验证

```text
go list -m -json github.com/RedHuang-0622/Seele
# Version: v0.3.5，无 Replace

go build ./...
go vet ./gui/ ./application/core/ ./middle/switch/workspace/
go test ./gui/ ./application/core/ ./middle/switch/workspace/ -count=1 -timeout=120s
```

`vendor/` 在仓库中被忽略，但仍按发布依赖同步，避免本机构建继续落到旧的
`v0.3.4` 文件。
