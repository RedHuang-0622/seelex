# 跨平台 shell 沙箱开源方案调研

调研日期：2026-07-28。目标是让 Seelex 的 `bash` 在 Windows、Linux、macOS 上都能做到“仅项目目录可读写，其余默认拒绝”，并能限制网络、进程资源和继承的环境变量。

## 结论

推荐采用可替换的 `CommandSandbox` 端口，并优先对接 **isobox** 进行原型验证；在它稳定前，保留 Seelex 当前的项目路径 gate 作为文件工具的确定性保护，且将 shell 明确标记为“非 OS 级隔离”。

isobox 是目前最贴近目标的跨平台能力模型：同一策略按平台落地为 Windows AppContainer、Linux gVisor、macOS Seatbelt，提供 `strict` 模式并报告无法实施的能力。它的主要风险是项目很新（2026-06 创建）、社区很小，且 Linux 的强隔离需要安装 `runsc` / Docker backend。因此它应经由适配层接入，不能让业务代码直接依赖其 CLI 或配置格式。

## 候选对比

| 方案 | 平台后端 | 项目目录策略 | 网络/资源 | 成熟度与风险 | 结论 |
|---|---|---|---|---|---|
| [can1357/isobox](https://github.com/can1357/isobox) | macOS Seatbelt、Linux gVisor、Windows AppContainer；可选 Docker backend | `--readable` / `--writable`；支持临时写入 | 网络能力、CPU/内存/PID/超时、环境变量清理；`--strict` | MIT；创建于 2026-06，约 15 stars；要求 Go 1.26，Linux 强后端有外部运行时 | **首选原型**；只通过 adapter 集成，并将 strict 作为生产默认。 |
| [zhangyunhao116/agentbox](https://github.com/zhangyunhao116/agentbox) | macOS Seatbelt、Linux namespace + Landlock、Windows Restricted Token + Job Object + ACL | 默认拒绝写入，允许显式路径 | 网络规则、命令分类、审批回调 | MIT；约 5 stars；README 明确标注 beta / API 可能破坏性变化 | **备选原型**；Go API 更贴近 Seelex，但需要安全审计和三平台 POC。 |
| [itchio/smaug](https://github.com/itchio/smaug) | Linux bubblewrap/firejail、macOS sandbox-exec、Windows 低权限独立用户 | Linux/macOS 可按目录配置；Windows 通过用户 ACL 共享目录 | 无统一最小权限能力矩阵 | MIT；长期维护但最初面向游戏启动；Windows 需要创建本地用户 | 可借鉴进程树/兼容性处理，不适合作为 AI 命令隔离主依赖。 |
| OCI 容器：[Moby](https://github.com/moby/moby) / [Podman](https://github.com/containers/podman) / [runc](https://github.com/opencontainers/runc) | 三平台通过 Docker Desktop/Podman machine/Windows containers 等运行环境 | bind mount 只挂载项目目录 | 资源与网络隔离成熟 | 成熟但部署较重，Windows/macOS 通常隐含 VM 或 daemon | 适合“高风险/可选强隔离”模式，不适合作为零依赖默认。 |
| [Firecracker](https://github.com/firecracker-microvm/firecracker) | Linux KVM | guest 内挂载项目 | 极强 VM 隔离 | Linux-only，运维和启动成本高 | 不满足本地三平台统一主路径；可作为远程执行后端。 |
| [nsjail](https://github.com/google/nsjail) / [gVisor](https://github.com/google/gvisor) | Linux | namespace / user-space kernel | 强 | Linux-only | 不单独采用；isobox 可在 Linux 端选用 gVisor。 |

## 建议的产品架构

```text
application / shell tool
        │
        ▼
CommandSandbox (Seelex-owned interface)
  - project root read/write allowlist
  - network policy
  - env allowlist/scrubbing
  - cpu / memory / pids / timeout
  - strict required capabilities
        │
        ├── NativeProjectCWD (当前：仅固定 cwd；非安全 sandbox)
        ├── IsoboxAdapter (推荐 POC)
        ├── AgentboxAdapter (备选 POC)
        └── OCIAdapter (高风险 / 用户显式开启)
```

接口应返回实际实施的能力与 caveat；当用户要求的能力在当前 OS/backend 不可实施时，生产模式必须 fail-fast，而不是悄悄降级成普通 `exec.Command`。

### 环境透传契约（2026-08-06 正式化）

沙盒必须继承本地环境，而不是"空环境"：

- `SandboxCapabilities.EnvPassthrough` 报告是否透传本地环境；
- PATH、HOME、SystemRoot 等基础变量与本地工具链（go/git/node/python/gcc 等）
  原样继承，`go test -race`、`node`、`git` 等本地命令必须可用；
- 只清洗凭据类变量（API key/secret/token/password，`scrubEnvironment`）；
- 剥夺本地工具链的沙盒视为能力降级，必须显式报告（`EnvPassthrough=false`），
  不得静默提供空环境。

## POC 验收标准

1. Windows、Linux、macOS 各执行一组同样的测试：读取项目文件成功，读取用户 home/Seelex 目录失败；项目内写入成功，项目外写入失败。
2. 禁网策略下 DNS、HTTP、原始 socket 均失败；允许网络时仅按明确策略放行。
3. shell 可用 `cd`、绝对路径、子进程、解释器（Python/Node/PowerShell）尝试绕过，仍不能访问项目外文件。
4. kill/timeout 能回收整棵进程树；CPU、内存、PID 上限可观察、可测试。
5. 将当前项目路径、临时目录、所需 runtime 只读依赖列入最小 allowlist；继承环境仅允许必要变量，默认清理凭据变量。
6. backend 缺失或能力矩阵不足时返回可操作错误；不回退到非隔离 shell。

## 当前实现的边界

本轮修复已经让 `read_file`、`grep_search`、`glob`、`write_file`、`edit_file` 在未绑定项目时 fail-closed，并将其路径解析到项目根且拒绝 `..`、绝对路径越界与符号链接逃逸。`bash` 也会强制其默认/显式 `workdir` 在项目内。

但是 shell 命令字符串本身仍可以执行 `cd`、引用绝对路径或启动子进程，因此 `workdir` 不是安全边界。只有接入并严格验证上述 OS 级 sandbox 后，才可宣称 shell 的文件访问受项目范围强制约束。
