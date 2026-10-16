# test（仓库级集成用例包）

## 生态位

`test/` 承载 Seelex 的**仓库级集成用例**：原先散在仓库根目录的 57 个 `*_test.go`
（full-chain 装配、会话/工具链回归、插件与精选目录、项目看板、发布契约、构建布局护栏、
定时任务与 teamwork 冒烟）。

它存在的理由有两条：

1. 仓库根是 `package main`（入口），Go 不允许别的包 import 它——用例散在根目录只能贴着
   入口长，既不能被单独点名，也无法在"入口换成第二个 main（不同 build 变体）"时复用；
2. M5 之后入口只剩 48 行装配调用，用例统一由本包承载，通过
   `wiring/{config,registry,bootstrap}` 的公开 API 摆现场——这正是"启动项分包"的验收面：
   用例不需要 `main` 就能装出完整产品。

主要调用方：开发者与 CI（`go test ./test/`）。本包不被任何生产代码依赖。

## 职责与非职责

**做**：端到端装配与回归复现（真 API 用例自查环境变量后 skip）、跨模块契约钉
（发布版本号、默认档位、构建布局、图标资源、精选目录）、源码文本门禁（"只有一处实现"
这类判据的机械检查）。

**不做**：不复制业务判定（夹具只摆现场、断言只读公开 API）；不放模块内部单元用例
（那些随包走：`backend/...`、`store/...` 各自的 `_test.go`）；不连外部服务（真 API 族由
build tag 或环境变量开关）。

## 文件结构

| 文件 | 职责 |
|---|---|
| `doc.go` | 包说明（本包的定位与运行方式）。 |
| `main_test.go` | `launchFlags`（与入口同一份 `wiring/config.BindFlags`）+ `TestMain`（把工作目录切到仓库根）。 |
| `*_test.go` | 57 个集成用例文件（M5-S4 从仓库根整体搬入，断言逐条未改）。 |
| `README.md` | 本文件。 |

## 核心实现

- **摆现场的唯一句柄**：`launchFlags`（`wiring/config.BindFlags(flag.CommandLine)`）。用例改
  `launchFlags.StorePath` / `PluginsPaths` 等字段把产品指到临时目录，与搬迁前改包级
  `*storePath` 的语义逐字相同。
- **full-chain 基座**：`tool_full_chain_test.go` 的 harness 按**与 `wiring/bootstrap` 同序**
  装配（M5-S3 之后用 `bootstrap.Init*` 逐段复用），并在注释里点名顺序约束（例如
  `SetTeamworkBackend` 必须先于 `InitApplication`）。
- **源码文本门禁**：少数用例读生产源码文本断言"只有一处实现"（插件根解析、根读数生成、
  工具注册分封、状态词表）。它们读的是仓库相对路径，因此依赖下面那条 CWD 口径。

## 数据流

用例 → `launchFlags` / 临时目录夹具 → `wiring/bootstrap` 或各模块公开 API → 断言。
真 API 族（`-tags manualsmoke` 等）由环境变量与 build tag 双重开关控制，默认集合里全部
skip，不会因为缺账号而红。

## 依赖方向

允许：任何生产包的**公开 API**（`wiring/*`、`application/*`、`backend/*`、`store/*`、
`internal/testutil`…）与标准库。

禁止：本包被生产代码 import（`test` 只被 `go test` 使用）；在用例里复制装配顺序——
应当调用 `wiring/bootstrap` 的 `Init*`。

## 并发、存储与错误语义

- **CWD 口径（重要，且是刻意保留的偏差）**：`TestMain` 在运行前把工作目录切到**仓库根**，
  因为搬进来的用例按仓库相对路径读夹具（`gui/icons/`、`scripts/`、`.claude/`、`plugins/`、
  `wiring/**` 的源码文本）。不切目录就得逐条改路径，那属于重写用例而不是搬迁；切目录让
  搬迁前的语义逐字保持。用例内部仍可用 `t.Chdir` 切到临时目录。
- 用例之间共享**进程级**状态（`launchFlags`、`runtimeLimits` 类的包级值、`promptassets` 目录）：
  因此涉及这些状态的用例不要 `t.Parallel()`；已有用例按原样保留并行标记。
- 真 API 用例一律先查环境变量（如 `SEELEX_RUN_LLM_SMOKE=1`）再 skip，缺凭证不会让 CI 红。

## 扩展方式

新增仓库级集成用例：放进本包，摆现场用 `launchFlags` + `t.TempDir()`；需要"与生产同序装配"
就用 `wiring/bootstrap` 的 `Init*`（不要自己拼一遍），并在注释里写清顺序约束。
需要真 API 的用例加 build tag（`manualsmoke` / `asynclive` / `compactlive` / `computerlive` /
`manualsmoke2`）并自查环境变量。

## Review 指南

- 用例是否在复制生产装配（应当复用 `bootstrap.Init*`）。
- 新用例若改包级状态（`launchFlags`、`promptassets.SetDir`、进程环境），是否 `t.Cleanup`
  还原、是否与 `t.Parallel()` 冲突。
- 源码文本门禁的**目标路径**是否仍指向真实落点（M5 搬迁后 `main.go` 已不再持有装配段）。
- 断言是否被"顺手放宽"（搬迁类改动必须逐条不动）。
- 真 API 用例是否仍能在缺凭证时 skip 而不是 failed。

## 测试与验证

```text
go test ./test/ -count=1                      # 默认集合（真 API 族 skip）
go test -tags manualsmoke ./test/ -count=1    # 需要真实账号的手工冒烟族
gofmt -l test
```

关键用例：`tool_full_chain_test.go`（同序装配基座）、`manual_smoke_test.go`（手工冒烟总入口）、
`repro_*_test.go`（会话/工具链回归复现）、`release_test.go`（发布契约：版本号、默认档位、
安全默认值）、`build_layout_test.go` 与 `icon_resource_test.go`（构建/交付布局护栏）、
`plugin_*_test.go` 与 `project_board_*_test.go`（插件与看板契约）。

## 分卷 README

本包文件数超过 15，按 readme-spec §4 分卷承载「文件与函数索引」（每卷一个 `README-<卷名>.md`）：

| 卷 | 覆盖 | 内容 |
|---|---|---|
| [README-harness.md](README-harness.md) | `doc.go`、`main_test.go` | 用例基座：包说明、TestMain 的 CWD 口径与 launchFlags 摆现场句柄。 |
| [README-live.md](README-live.md) | `async_exec_ab_live_test.go`、`async_tool_wire_live_probe_test.go`、`compact_live_smoke_test.go`、`computer_use_live_smoke_test.go`、`context_long_live_test.go`、`job_backfill_live_smoke_test.go`、`job_live_smoke_test.go`、`manual_smoke_test.go`、`prefix_chain_live_test.go`、`real_api_permission_smoke_test.go`、`real_api_prefix_live_test.go`、`real_api_smoke_test.go`、`scheduled_task_live_smoke_test.go`、`smoke_test.go` | 真 API / 手工冒烟族：由 build tag 与环境变量双开关，缺凭证即 skip（默认集合不红）。 |
| [README-repro.md](README-repro.md) | `prefix_invariant_fullchain_test.go`、`repro_chain_ab_test.go`、`repro_hot_attach_background_tool_order_test.go`、`repro_multi_round_llm_preserved_test.go`、`repro_queued_input_switch_promotion_test.go`、`repro_second_round_engine_released_test.go`、`repro_session_background_test.go`、`repro_session_disappear_test.go`、`repro_session_race_test.go`、`repro_session_switch_real_test.go`、`repro_session_workspace_test.go`、`repro_three_sessions_running_switch_test.go`、`repro_two_running_view_third_then_switched_finishes_test.go` | 会话/工具链回归复现：用户现象 → 会红的断言（含多会话并发与热切场景）。 |
| [README-session.md](README-session.md) | `session_chain_bench_test.go`、`session_switch_profile_test.go` | 会话生命周期与切换成本：链路基准与切换剖面。 |
| [README-plugin.md](README-plugin.md) | `curated_catalog_test.go`、`dev_plugin_payload_test.go`、`plugin_catalog_registration_test.go`、`plugin_curated_runtime_test.go`、`plugin_doc_sigil_test.go`、`plugin_roots_test.go`、`plugin_self_tools_test.go`、`skill_activate_tool_test.go`、`tool_face_prompt_consistency_test.go`、`tool_meta_declaration_test.go` | 插件与精选目录契约：目录登记、根解析、自省工具、技能激活、工具声明分封。 |
| [README-project-board.md](README-project-board.md) | `project_board_e2e_test.go`、`project_board_i7_test.go`、`project_board_wiring_test.go`、`project_board_write_test.go` | 项目看板：读/写/刷新工具与装配接线。 |
| [README-teamwork.md](README-teamwork.md) | `teamwork_headless_smoke_test.go`、`teamwork_plugin_assembly_headless_test.go`、`teamwork_worktree_lifecycle_test.go` | teamwork / 子代理装配：headless 冒烟、插件装配与 worktree 生命周期。 |
| [README-tool.md](README-tool.md) | `tool_full_chain_live_test.go`、`tool_full_chain_test.go` | 工具面全链：装配基座与上下文读工具、工具记录写路径。 |
| [README-permission.md](README-permission.md) | `permission_config_test.go`、`permission_employee_panel_test.go` | 权限门与审批桥：配置叠加、员工面板、真 API 权限冒烟。 |
| [README-release.md](README-release.md) | `boot_prompt_test.go`、`boot_seed_test.go`、`build_layout_test.go`、`icon_resource_test.go`、`release_test.go` | 发布与交付契约：版本号、默认档位、构建布局、图标资源、快照校验与启动种子。 |
| [README-probe.md](README-probe.md) | `headless_restore_prefix_probe_test.go`、`job_migration_residue_test.go` | 一次性探针：headless 恢复前缀顺序与作业迁移残留（现场取证用，保持可复跑）。 |

