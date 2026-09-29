# 启动期默认数据落盘：责任链补上"缺失即初始化"这一支

- 日期：2026-09-29
- 起因：用户口径——"对这个内容做出 if not exist 的初始化操作，同理对其他地方的也是；
  同时需要支持默认数据的编码，在初始化注入到配置里面去；如果发现 exist 的话那么就读配置
  就完事了，这就是个责任链的设计模式使用的问题"
- 范围：`main.go` 的启动期资源解析（`config/seelex.yaml`、`config/seele.yaml`、白名单命令
  的脚本目录）+ 新增 `internal/bootseed` + 两个默认数据包
- 结论：落点已接通并实证（先红后绿）；两件事按安全/许可口径**没有**擅自做（见 §7）

---

## 1. 现场：责任链只有"读"的那一支，走到头就静默降级

启动期有三处"按候选链找文件/目录"的地方，全都只有读分支，链走空就悄悄换一套语义：

| 位置 | 候选 | 链走空的后果 |
|---|---|---|
| `main.go:initRuntime` | `firstExisting("config/seelex.yaml", "seelex.yaml")` | window/limits 全走代码默认值 |
| `main.go:setupPermissionGate` | `firstExisting("config/seele.yaml", "seele.yaml")` | 权限规则只剩 `DefaultPermissionConfig()` |
| `main.go:resolveAutoGetJobsDir` | CWD 相对 → 二进制旁边 | 白名单命令**不登记**：新建定时任务的命令下拉是空的，命令类任务无法发布 |

两条现场事实（本次复核）：

- `dist/seelex-gui-dev/` 里有 `config/`、`plugins/`、`CHANGELOG.md`，**没有 `local/`** ——
  所以那条 `auto_get_jobs` 白名单命令在 dev 包里必然落空（仓库根 `local/` 被 `.gitignore:146`
  排除，不随包走）；
- `dist/dev/`（CLI 包）**连 `config/` 都没有** —— 从包目录直接跑 CLI，limits 与权限规则
  一份都不读（这条早先记在 `docs/devlog/2026-09-29-dev-package-config-drift.md` §6）。

两处"看起来不一样"的怪相，根子是同一件事：**链上没有文件时，没有人负责把它初始化出来**。

## 2. 口径：责任链 + 存在即读 + 缺失即初始化

```text
1. config/<name>         CWD 相对（仓库里那份 / 开发场景；用户改过的就是它）
2. <name>                根目录回退（历史兼容）
3. <exe>/config/<name>   包内配置（正式部署：二进制旁边自带一份）
```

- **存在即读**：链上第一份存在的文件就是答案，进程一个字节都不写（`KindHit` 分支零副作用）。
- **缺失即初始化**：链全走空 → 用内嵌默认档落盘，再按同一入口读回来（`KindSeeded`）。
- **落盘根只取 `<exe>/config`**：CWD 可能是用户的项目目录，不能在那里凭空造出 `config/` 来；
  二进制所在目录才是这个应用自己的地盘。落盘失败（只读安装目录）只回错误，调用方回退
  代码默认值，**不阻断启动**。

## 3. 机制：`internal/bootseed`

```go
type Spec struct {
    Name       string   // 日志名
    Candidates []string // 存在判据：按优先级的候选文件路径
    SeedRoot   string   // 全缺时的落盘根；"" = 只报告缺失
    Entry      string   // 落盘后的入口（相对 SeedRoot）；"" = SeedRoot 本身
    Pack       Pack     // 默认数据
}
```

`Resolve` 就是上面三条口径的实现；`Materialize` 逐文件写默认数据，**已存在的目标一律跳过**
（用户那份优先），入口仍缺时 `Path` 留空（例如工具骨架里没有 `main.py`）。

**默认数据的两种编码**（这是"支持默认数据的编码"那一句的落点）：

| `File.Encoding` | 用途 | 解码 |
|---|---|---|
| `EncodingRaw`（零值） | 文本类，可直接 diff、可读 | 逐字节 |
| `EncodingBase64` | 非 UTF-8（GBK）、二进制、字节敏感 | 先去掉空白再 base64 解码（载荷可折行） |

`EncodeBase64` 与 `Decode` 对称；单测用 `{0xB1,0xB1,0xBE,0xA9,0x00,0xFF,0x0A}`（GBK + NUL +
非法 UTF-8 字节）钉住"编码只是承载方式，解回来必须逐字节相同"。

## 4. 第一批落点

| 落点 | 默认数据 | 从哪来 |
|---|---|---|
| `config/seelex.yaml` | 仓库规范档的逐字节副本 | `scripts/sync-bootseed-defaults.ps1` 同步 |
| `config/seele.yaml` | 同上 | 同上 |
| `local/tools/auto_get_jobs/` | 骨架 `README.md` + `.env.example`（只有键名，值为空） | 应用自己写（`assets/`） |

为什么工具目录只落骨架、不落脚本本体：那份脚本是**第三方项目**
（`local/tools/auto_get_jobs/README.md` 指向 <https://github.com/SanThousand/auto_get_jobs>），
把它复制进仓库再随二进制分发是产品/许可决定，不在本次擅自动手的范围内。骨架解决的是
"下拉是空的、没人知道该放什么"这一半：目录与 `.env.example` 一出现，缺口就是现场可见的事实。

命令登记的既有安全口径**没有**放宽：没有 `main.py` 就不登记（`登记即信任、argv 固定直传`）。
骨架不会让一个跑不起来的命令看起来可发布。

## 5. 先红后绿

先红：把 `ensureConfigFile` / `resolveToolDir` 临时改回改动前的形状（只读链、不落盘），
同一条命令跑出 4 条红：

```text
--- FAIL: TestEnsureConfigFileSeedsDefaultThenReadsIt
    候选链全缺时应初始化到二进制旁边的 config/：期望 <tmp>/config/seelex.yaml，得到 config\seelex.yaml
--- FAIL: TestEnsureConfigFilePrefersCWDConfig / TestEnsureConfigFileKeepsLegacyRootFallback
    （这两条顺带钉住"候选优先级"：红的实现返回的是相对路径，绿的实现返回绝对路径）
--- FAIL: TestResolveToolDirSeedsSkeletonWithoutRegistering
    缺失即初始化：骨架应落盘 README.md（The system cannot find the path specified.）
```

再绿（`main.go` 逐字节还原后，备份哈希一致 `0C38E102…`）：

```text
go test ./internal/bootseed -count=1   → 12 条全 PASS
go test . -run 'TestEnsureConfigFile|TestResolveAutoGetJobsDir|TestResolveToolDir' -count=1 → 6 条全 PASS
```

绿侧覆盖的性质（每条都是"不许违反"的口径）：

| 用例 | 钉住的性质 |
|---|---|
| `TestResolveHitReadsExistingAndWritesNothing` | 命中即读：不改写用户那份、`SeedRoot` 一个目录都不建 |
| `TestResolveCandidatePriorityFirstWins` | 候选链是责任链：第一个存在的胜出 |
| `TestResolveMissingSeedsDefaultBytes` | 缺失即初始化：写出的字节与默认数据逐字节相同 |
| `TestResolveKeepsSeededThenNeverOverwrites` | 再次启动：已存在目标全跳过，用户改过的不被盖回 |
| `TestResolveWithoutSeedRootReportsMissing` / `TestResolveSeedFailureIsReported` | 无处落盘 / 落盘失败：如实报告，不假装成功 |
| `TestResolveSeedWithoutEntryReportsEmptyPath` | 骨架里没有入口 → `Path` 留空，不会返回不存在的路径 |
| `TestMaterializeRejectsEscapingRel` | 默认数据不许写到落盘根之外 |
| `TestDecodeBase64RoundTripKeepsNonUTF8Bytes` | 编码往返逐字节保真（GBK/NUL/非法 UTF-8） |
| `TestEmbeddedConfigDefaultsMatchRepository` | 内嵌默认档与仓库规范档**逐字节相同**（防漂移钉子） |
| `TestAutoGetJobsSkeletonShipsDotEnvExample` | `//go:embed all:assets` 的 `all:` 前缀（少了会静默丢掉点开头的文件） |
| `boot_seed_test.go` 四条 | 真实装配路径：CWD config 优先 → 根目录回退 → 包内缺失时落到二进制旁边；工具目录命中/骨架/优先级 |

## 6. 门禁

```text
gofmt -l main.go internal/bootseed boot_seed_test.go     # 干净
go vet ./internal/bootseed .                             # 干净
go build ./...                                           # 通过
go test ./internal/bootseed -count=1                     # 12 PASS
go test . -run 'EnsureConfigFile|ResolveToolDir|ResolveAutoGetJobsDir' -count=1  # 6 PASS
```

## 7. 遗留（如实记账，不擅自决定）

1. **第三方脚本本体是否随包分发**：要让 dev 包的命令下拉真的有值，必须让包里有
   `local/tools/auto_get_jobs/main.py`（以及它的依赖）。可选口径：把该项目的源码当默认数据
   收进仓库并编码进二进制（需要你确认许可与"要不要把第三方代码放进这个仓库"）；
   或维持"骨架 + 本机自备"。
2. **白名单是否搬进配置**：把 `main.go` 里登记的那条命令改成配置驱动（`config/` 里
   一份命令清单，缺失时用内嵌默认条目初始化）能顺带解决"加命令要重新编译"，但**会削弱
   现有安全口径**——`登记即信任、argv 固定直传、编译期白名单`会变成"可编辑的本地文件说了算"，
   而 agent 本身具备写文件能力（写好配置 → 定时任务执行任意 argv）。要做的话得配一条
   代码侧键名允许清单，或明确接受这个边界。
3. `accounts.yaml` **不参与**自愈：它是本地凭据（`-LocalConfigPath` 显式给出），
   仓库根与包内都不会凭空生成一份假的。
4. CWD 不落盘是刻意的：只有二进制所在目录会被初始化。若将来要支持"数据根"（如
   `.seelex/`）作为落盘位置，在 `runtimeConfigChain` 里加候选即可，机制不用动。
