# 2026-09-16 主会话权限档位（Session Permission Tier）落地

> 日期: 2026-09-16 | 范围: `application/contract/dto/permission.go`、
> `seelebridge/tools/{permission_tiers,registry_state}.go`、`session/{ports,runtime_slot}.go`、
> `application/{model/state.go,contract/ports.go,core/*}`、`seelebridge/{runtime_tools}.go`、
> `internal/adapters/runtime_port.go`、`main.go`、`gui/{bridge,headless}.go`、
> `gui/frontend/dist/*`
> | 前置：`docs/2026-09-16-session-permission-tiers/README.md`（方案）、
> `docs/arch/agent-permission-subjects.md`（权责模型长期口径）、
> `docs/2026-09-16-ring-escape-permission-bearing/CHANGES.md` §3（权限拦截承重面）

## 一、口径（用户原话 → 可执行断言）

> **主会话的权限设置要做出不同挡位；挡位做成列表放在相关面板里让用户选；且这是主 agent
> 在这个 session 的粒度。**（承接同日需求 3：主会话全权只管网主会话，员工越权照旧审批提权）
> 补充指令：**chip 的放置位置就放在当今全权的位置上。**

拆成四条：

1. 二元 `full_access` 升格成**有序档位表** `manual / edit / auto / full`（升序 = 自动度递增）；
2. 档位是**对 root 的声明式覆盖**（剪掉哪些 `ask` 规则），不是新判定机制；
3. 档位**会话粒度**（沿用 `SessionUnit` 槽 + 执行门按会话解析），面板列表可选；
4. `full` 档（= 旧全权）**只对 root 短路**，员工/子代理判定链一字不改。

## 二、模型与落点

| 层 | 落点 | 做了什么 |
| --- | --- | --- |
| 词表 | `dto/permission.go` | `PermissionTier{Manual,Edit,Auto,Full}` + `PermissionTiers()` 目录（id/标签/短名/说明）+ `NormalizePermissionTier` 写入侧校验 + `PermissionTierFromFullAccess` |
| 覆盖实现 | `seelebridge/tools/permission_tiers.go`（新） | `ApplyTier(base, tier)`：**只删该档位要放开的 `ask` 规则**（`edit` 剪 write_file/edit_file；`auto` 再剪 bash 的全部 ask），从不新增 allow、从不触碰 deny |
| 判定 | `registry_state.go` | `sessionTier` 取代 `sessionFullAccess`；`tierCheckers` 在 `rebuildLocked` 预构建；`gate()` 按 **class + 会话档位**选表（root 用档位表、sub/员工用 base）；`Enforce` 短路由 `class != sub` **收紧为 `class == root`**；`SetFullAccess*` 变兼容壳 |
| 会话 | `session/{ports,runtime_slot}.go` | `permissionTier` 槽 + `PermissionTier/SetPermissionTier`；`FullAccessMode/SetFullAccessMode` 转派生 |
| 应用 | `application/{model,contract,core}` | `RuntimeState.PermissionTier/PermissionTiers`、`SessionRuntime.PermissionTier`；`RuntimePort.PermissionTier/SetPermissionTierFor`；`Service.SetPermissionTier`（返回生效档）、`syncFullAccessFor` 改走档位、投影/协调器注入 `CurrentPermissionTier`；`SetFullAccess` 兼容壳 |
| bridge/GUI | `seelebridge/runtime_tools.go`、`internal/adapters`、`main.go`、`gui/{bridge,headless}.go` | `Runtime.PermissionTier/SetPermissionTierFor`；`parsePermissionMode` 放宽为档位 id（兼容 `full_access → full`）；`Bridge.SetPermissionTier`；headless 补 `SetPermissionTier` 分派 |
| 前端 | `gui/frontend/dist/{index.html,app.js,styles.css,snapshot-shape.js}` | 运行状态弹窗新增「权限档位（本会话）」有序列表（后端目录下发）；composer chip **就地**改为档位指示器（点击打开运行状态），位置不动 |

**关键不变量**：档位的覆盖只可能发生在 deny 之前——`rm -rf /`、`dd if=* of=*`、`mkfs*`
在任何档位下都硬拦（`DefaultPermissionRules` 的 deny 段逐条保留）。

## 三、主体边界（需求 3 的落点，也是修掉的既有外溢）

旧实现：`Enforce` 的全权短路条件是 `class != SubjectClassSub`——`full_access` 会连带放行
**员工**的越权请求（`emp_*` 主体也命中短路）。本次收紧为 `class == SubjectClassRoot`：

- 员工（`emp_*`）不享 `full` 档：越权照旧走执行选择页面提权（`enforceEmployee`）；
- 子代理（`sub`）不享 `full` 档：断位照旧不可路由、无人类可问即拒绝；
- 且 `gate()` 对非 root 一律用 base checker——`auto/edit` 也不会把员工写文件的"审批提权"
  静默变成"直接放行"。

回归钉：`seelebridge/tools/permission_tiers_test.go`
`TestTierDoesNotBypassEmployeeBoundary` / `TestTierDoesNotBypassSubagentBoundary`。

## 四、验证证据（可复现）

```powershell
gofmt -l application seelebridge session gui internal e2e main.go release_test.go   # 空
go vet ./...                                                    # 通过
go test ./... -count=1                                          # 全绿（无 FAIL）
go test -race ./seelebridge/tools/ ./session/ ./gui/ ./application/core/ -count=1   # 全绿
node --test "gui/frontend/dist/*.test.mjs"                     # 304 用例通过
```

定向用例：

- `seelebridge/tools`：`TestTierDecisionMatrix`（逐档位 × 代表工具的判定表）、
  `TestTierRootAutoRunsWithoutApproval`、`TestTierDoesNotBypassEmployeeBoundary`、
  `TestTierDoesNotBypassSubagentBoundary`、`TestTierSessionIsolation`、
  `TestTierSurvivesConfigReinstall`、`TestSetPermissionTierRejectsUnknown`、
  `TestTierCatalogMirrorsDTO`；
- `application/core`：`TestPermissionTierOwnershipPerSession`、
  `TestPermissionTierProjectionPerSession`、`TestSetPermissionTierRejectsUnknown`、
  `TestSetFullAccessCompatMapsToTier`；
- `gui`：`TestBridgeForwardsPermissionTier`、`TestEmbeddedFrontendExists`（前端契约）；
- 旧用例 `TestFullAccess*` / `TestProbeFullAccess*` 全绿（兼容壳语义不变）。

## 五、与需求 1–3 的关系

| 用户点 | 本轮的承接 |
| --- | --- |
| 员工权限来自全局员工数据（可继承） | 不动：档位不产生员工条目，员工权限仍是 `emp_<角色>` 主体条目。 |
| 前端标明"哪个员工用了工具" | 不动（另一件事）。 |
| 主会话全权只管网主会话；员工越权照旧审批提权 | **本轮核心收口**：`Enforce` 短路收紧为 root；非 root 一律 base 表（§3）。 |
| 主会话权限档位 + 面板列表 + 会话粒度 | §2 各层落点；前端 §2 前端行。 |

## 六、已知风险 / 边界

- 档位**不落盘**（与既有 `effort/full_access` 同口径，会话槽内存态）；跨重启记忆是后续决策点。
- 档位是"问不问人"，不是沙箱；不改工具可见面语义（可见性仍由位决定）。
- 不改框架（Seele）：只用现有 `Gate/Checker/BitEnforcer/PermissionConfig` 能力。
- `rw_desktop`（共享外设）与 `adm`（能力面）刻意只在中档"问人"，仅 `full` 放开。
