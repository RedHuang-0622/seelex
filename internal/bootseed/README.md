# bootseed

## 生态位

启动期的**资源自愈**：`main.go`（composition root）在装配前用它决定"该读哪个
配置文件/工具目录"，缺了就用内嵌默认数据初始化。调用方只有两处，都在 `main.go`：
`ensureConfigFile`（`config/seelex.yaml`、`config/seele.yaml`）与 `resolveToolDir`
（本地工具目录，当前是 `local/tools/auto_get_jobs`）。

## 职责与非职责

- **做**：按候选链（责任链）决定读哪份资源；候选全缺时把内嵌的默认数据落盘；
  如实报告"命中 / 已初始化 / 无处落盘"。
- **不做**：不解析任何配置内容、不认识任何业务键、不改写已存在的文件、不决定启动
  是否失败（落盘失败只回错误，调用方回退代码默认值）。

## 结构

| 文件 | 职责 |
|---|---|
| `bootseed.go` | 责任链与落盘：`Spec`/`Pack`/`File`、`Resolve`/`ResolveFS`、`Materialize`、`Decode`/`EncodeBase64` |
| `defaults.go` | 内嵌默认数据（`//go:embed all:assets`）与各包声明：`RuntimeConfigPack`、`PermissionConfigPack`、`AutoGetJobsPack` |
| `assets/config/` | 运行参数与权限规则的默认档：与仓库 `config/*.yaml` **逐字节相同**（`scripts/sync-bootseed-defaults.ps1` 同步） |
| `assets/local/tools/<tool>/` | 本地工具目录骨架（README、`.env.example`）；第三方脚本体不进包 |

## 核心实现

```go
type Spec struct {
    Name       string   // 日志名
    Candidates []string // 存在判据：按优先级的候选文件路径
    SeedRoot   string   // 全缺时的落盘根；"" = 只报告缺失
    Entry      string   // 落盘后的入口（相对 SeedRoot）；"" = SeedRoot 本身
    Pack       Pack     // 默认数据
}
```

`Resolve` 的执行顺序，就是"责任链 + 存在即读 + 缺失即初始化"这三条口径：

1. 逐个 `os.Stat` 候选，第一个**存在**的胜出 → `KindHit`，本次不写一个字节；
2. 全缺且 `SeedRoot == ""` → `KindMissing`（调用方走代码默认值）；
3. 全缺且有 `SeedRoot` → `Materialize`：逐文件写出默认数据，**已存在的目标一律
   跳过**（用户那份优先），入口仍缺时 `Path` 留空（例如工具骨架里没有 `main.py`）；
4. 落盘失败（目录只读等）→ `KindMissing` + error，绝不假装初始化成功。

默认数据有**两种编码**（`File.Encoding`）：`EncodingRaw` 逐字节内嵌（文本，可读
可 diff），`EncodingBase64` 编码后内嵌（非 UTF-8/GBK、二进制、字节敏感的数据；
解码前先去空白，所以载荷可以折行）。`EncodeBase64` 与 `Decode` 对称。

## 依赖方向

只依赖标准库。**不允许**反过来依赖 `application/`、`seelebridge/`、`gui/`：它是
装配期的工具，不参与业务语义。

## 安全与数据约束

- **绝不覆盖**已有文件（`Materialize` 跳过已存在目标）；"存在即读"是硬口径，
  用户的配置永远优先于默认数据。
- 默认数据里**不放密钥/秘密**：`.env.example` 只有键名（值为空），
  `accounts.yaml` 这类凭据文件不在任何包内（由 `-LocalConfigPath` 显式指定）。
- 落盘位置只取**二进制所在目录**（调用方决定，见 `main.go` 的 `runtimeConfigChain`）：
  CWD 可能是用户的项目目录，不能在那里凭空造 `config/`。
- `Rel` 里出现 `..` 直接拒绝（默认数据不许写到落盘根之外）。

## 扩展方式

新增一处"缺失即初始化"的资源：在 `defaults.go` 加一个 `Pack`（`Rel`/`Source`/
`Encoding`），在 `main.go` 里按同一形状声明候选链 + 落盘根。默认数据要么是仓库
规范档的副本（配 `scripts/sync-bootseed-defaults.ps1` + 漂移测试），要么是应用
自己写的骨架（`assets/` 里直接放）。

## Review 指南

- 有没有在**命中**分支里写盘？命中分支必须零副作用。
- 默认数据与仓库规范档会不会分叉？`TestEmbeddedConfigDefaultsMatchRepository`
  钉住 `config/*.yaml`；新增默认数据请照同样的形状补一条。
- `//go:embed all:assets` 的 `all:` 前缀还在吗？少了它会静默丢掉 `.env.example`
  这类点开头的文件（`TestAutoGetJobsSkeletonShipsDotEnvExample` 会红）。
- 落盘根是不是被写成了 CWD 或用户目录？

## 验证

```text
go test ./internal/bootseed -count=1      # 12 条：命中零副作用 / 缺失逐字节落盘 /
                                          # 已存在跳过 / 非 UTF-8 编码往返 / 逃逸拒绝
go test . -run 'EnsureConfigFile|ResolveToolDir|ResolveAutoGetJobsDir' -count=1
go build ./...
```

对应的研发日志：`docs/devlog/2026-09-29-boot-default-seed.md`。
