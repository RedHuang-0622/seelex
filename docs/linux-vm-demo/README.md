# Seelex 在 VMware Ubuntu 22.04 虚拟机上的 Linux 部署与演示

> 目标：把 Seelex 的 Linux 版本构建出来、部署进 VMware 里的 Ubuntu 22.04 虚拟机，并在 VM 内
> 完成「构建 → 运行 → 工具调用 → 人机审批」的端到端验证，产出可交付的作品集演示素材。

> **后续补充（2026-09-25）**：本包还包含 **Linux computer use（X11 后端）的实测与演示**、
> 以及 **Ubuntu 桌面上的 Seelex GUI** 素材——见 [`computer-use.md`](computer-use.md) 与 §6.1。

> **后续补充（2026-09-30）**：Linux GUI 已有正式构建入口与交付落点——`make build-linux-gui`
> （Docker `ubuntu:22.04` + `webkit2_40`）产出 P6 `dist/linux-amd64-gui/`；构建、ABI 判据、
> 送入虚拟机与覆盖步骤见 [`build-linux-gui-delivery.md`](build-linux-gui-delivery.md)。

## 1. 结论（一句话）

Seelex 已作为原生 `linux/amd64` 静态程序运行在 VMware 的 Ubuntu 22.04.5 虚拟机内：既能以
`-frontend backend` 无界面跑通完整 Agent 流程（真实调用 `bash` 并返回内核信息），也能以 TUI
在 VM 桌面里完成「提问 → 权限审批 → 工具执行 → 回答」的完整闭环（见第 6 节素材）。

## 2. VM 环境（实测）

| 项 | 值 |
| --- | --- |
| 宿主 | Windows + VMware Workstation 17.6.3 (build-24583834) |
| VM | `G:\Linux\msi\Ubuntu.vmx`，NAT（vmnet8）|
| 客户机 | Ubuntu 22.04.5 LTS，kernel `6.8.0-79-generic`，x86_64 |
| 主机名 / 账号 | `red-virtual-machine` / `red`（sudo 可用）|
| VMware Tools | 12.3.5 build-22544099（`vmtoolsd` active，即 open/VMware Tools 通道可用）|
| 资源 | 4 vCPU / 8 GB RAM / 磁盘 `/dev/sda3` 39G（14G 可用）|
| 网络 | `ens33` 192.168.227.128/24，网关 192.168.227.2，可访问外网 |

## 3. 无界面管控通道（关键突破）

宿主的**键鼠注入到 VM 控制台不可用**（VMware 控制台走 raw input，系统级合成事件被忽略；
`vmcli MKS sendKeyEvent` 实测为空操作），因此走 **vmcli Guest ops** 这条带认证的通道：

```powershell
# 只读探针（无需认证）
vmcli.exe "G:\Linux\msi\Ubuntu.vmx" Guest query
vmcli.exe "G:\Linux\msi\Ubuntu.vmx" Guest toolsproperties

# 执行 / 双向传文件（需要 guest 凭据）
vmcli.exe <vmx> Guest run     -u red -p <pass> /bin/bash /tmp/script.sh
vmcli.exe <vmx> Guest copyTo  -u red -p <pass> -o <hostPath>  <guestPath>
vmcli.exe <vmx> Guest copyFrom -u red -p <pass> -o <guestPath> <hostPath>
```

- 认证要点：密码是 `Hzr040622.`（**结尾有一个点**）；`/etc/pam.d/vmtoolsd` 本来就在，
  之前失败纯粹是密码串不对。
- `Guest run` 实际**不等待**程序结束（只返回 PID），所以宿主脚本用「输出重定向 + 完成标记文件 +
  轮询 `Guest ls`」的方式回收 stdout（见宿主侧的 vmguest.ps1（仓库外，不入库））。
- 另建了更顺手的通道：把 ed25519 公钥写进 `~/.ssh/authorized_keys`，宿主机用
  `ssh -i keys/seelex_ed25519 red@192.168.227.128` 直接跑远程 bash（宿主侧 vmrun2.ps1，stdin 传脚本，
  规避多层引号问题）。

**顺带修好的两处 VM 配置：**

1. **网卡**：`ens33` 原本是 down 且被 NetworkManager 标为 unmanaged（无 DHCP、无默认路由）。
   宿主先 `vmrun connectNamedDevice <vmx> ethernet0`，客户机内 `ip link set ens33 up` +
   `dhclient ens33` → 拿到 192.168.227.128/24 与外网（注意：当前是手工 DHCP，重启后需要重做或
   为 NM 建持久连接）。
2. **桌面自动登录**：`/etc/gdm3/custom.conf` 打开 `AutomaticLoginEnable=true` /
   `AutomaticLogin=red`，避免演示时卡在锁屏。

## 4. 构建

### 4.1 宿主交叉编译（部署用的正式产物）

```powershell
cd G:\Program\go\seelex
$env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='amd64'
go build -trimpath -ldflags "-s -w -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=v0.1.0-linuxvm" `
  -o dist/linux-amd64/seelex .
```

产物 `dist/linux-amd64/seelex` = 23,720,120 B，`file` 报告
`ELF 64-bit LSB executable, x86-64, statically linked, stripped`（静态链接，天然可移植）。

运行期文件随包部署：`config/`（`accounts.yaml`/`seelex.yaml`/`seele.yaml`）、`plugins/`、
`README*.md`、`CHANGELOG.md`、`LICENSE`。整体 scp 到客户机 `~/seelex-linux/`。

### 4.2 客户机内原生构建（源码 + Go 工具链进 VM）

```bash
# 工具链：Go 1.25.8（go.mod 要求）由宿主侧提供 tar 包，避免内网下载 78MB
sudo tar -C /usr/local -xzf ~/go1.25.8-linux-amd64.tgz   # -> /usr/local/go/bin/go version = go1.25.8 linux/amd64
# 源码 + vendor（宿主侧 go mod vendor，30.3MB，离线可构建）
tar -xzf ~/seelex-src-vendor.tgz -C ~/seelex-build
cd ~/seelex-build
export PATH=/usr/local/go/bin:$PATH GOFLAGS=-mod=vendor GOPROXY=off CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=v0.1.0-invm" -o seelex .
```

实测：**31.7s** 构建完成，产物 23,711,928 B，`./seelex -version` → `v0.1.0-invm`。

## 5. 运行验证

### 5.1 headless（`-frontend backend`）

```bash
cd ~/seelex-linux
./seelex -frontend backend -permission auto \
  -backend-prompt "请执行 uname -a，然后用一句话回答你运行在什么操作系统上。" \
  -backend-project "$HOME/seelex-linux" -backend-timeout 150s -backend-log /tmp/seelex-backend.log
```

关键日志（`/tmp/seelex-backend.log`）：

```
[backend] +4ms  stage=startup.store.ready / startup.permissions.ready / startup.application.ready
[backend] +1.042s stage=toolhook.start.enter tool=bash
[backend] +1.047s stage=toolhook.complete.project.done tool=bash
[backend] +2.735s stage=tool.completed tool=task_complete status=success
[backend] +3.416s stage=chat.idle
2026/09/24 23:50:37 INFO shutdown complete
```

### 5.2 TUI（VM 桌面内，含权限审批）

TUI 需要**已绑定项目的会话**（ProjectScope 生效前提）：VM 内的
`~/seelex-linux/.seelex/workspace_index.json` 里已有 `ws-1790265020586310348` →
`/home/red/seelex-linux`，并用 `/resume draft_1790265034577360684_1` 恢复该绑定会话。
实测对话（原样摘录）：

```
You    请执行 uname -a，然后用一句话回答你运行在什么操作系统上。
Seele  I'll run `uname -a` to check the kernel and OS details.
✓ bash({"command": "uname -a"})   ↳ [bash] {"stdout":"Linux red-virtual-machine 6.8.0-79-generic
        #79~22.04.1-Ubuntu SMP PREEMPT_DYNAMIC Fri Aug 15 16:54:53 UTC 2 x86_64 ...","stderr":"","exit_code":0}
✓ task_complete("执行 uname -a 得到内核信息 ...")
Seele  我运行在 Ubuntu 22.04.5 LTS(Jammy)的 64 位 x86_64 机器上,当前内核为 Linux 6.8.0-79-generic。
```

未绑定项目的会话里，`bash`/`grep_search` 会被**在 project scope 阶段硬拦**
（`project scope: no project is bound to this session`）——这是设计内的第二道边界，也一并留证。

随后同一会话内用只读工具（`glob` / `grep_search` / `read_file`）读项目 README 回答
「两层安全边界」的问题，全部 `✓` 成功。

## 6. 演示素材（`artifacts/`）

| 文件 | 说明 |
| --- | --- |
| `01_tui_ubuntu_vm.png` | 1714×937 客户机整屏截图：Ubuntu 22.04 桌面 + 最大化终端里的 Seelex TUI，停在 `bash(pwd; ls -a)` 的**权限审批面板** |
| `02_tui_tool_result.png` | 同屏截图：`bash(uname -a / head -2 /etc/os-release)` 执行成功 + 最终自然语言回答 |
| `03_tui_demo.mp4` | 46s、12fps、H.264（218 kb/s，1.2MB）的 TUI 演示录屏（提问 → 工具调用 → 回答）|

采集方式：客户机内 `scrot -o <png>`（X11 会话）/ `ffmpeg -f x11grab -video_size 1714x936 -i :0.0 …`
（注意 libx264 要求宽高为偶数），再由宿主 scp 取回。

## 6.1 Linux GUI 与 computer use 素材（2026-09-25 补）

> 这一组是后续补采的：Linux computer use（X11 后端）的实测与演示，以及 Ubuntu 桌面上的
> Seelex GUI。完整报告、判据与复现命令见 [`computer-use.md`](computer-use.md)。

| 文件 | 说明 |
| --- | --- |
| `04_linux_computer_use_demo.mp4` | 31.7s、1714×936、15fps、H.264 的 **Linux computer use 能力演示**录屏（看→找→点→敲→滚→拖） |
| `05_gui_linux.png` | Ubuntu 桌面上的 **Seelex Linux GUI（Wails/WebKitGTK）** 全屏截图（会话列表 / 对话·轨迹 / 输入框 / 右栏三页） |
| `06_cu_full_screen.png` | computer use 演示起手：`screenshot` 自己截的整屏 |
| `07_cu_after_ascii.png` | 敲完 ASCII 命令后终端区域的截图 |
| `08_cu_after_scroll.png` | 滚轮上翻后同一区域的截图（与 07 内容不同，证明视口真的动了） |

三条最要紧的结论（细节见 `computer-use.md`）：

1. **能用**：观察面（截屏/窗口/焦点/光标）与鼠标面（移动/点击/拖拽/滚轮）实机全通，
   ASCII 键盘在 `xev` 层逐项精确；
2. **有一个真实缺陷**：非 ASCII（中文/emoji）走"临时重映射键码"那条路会丢字串字
   （同机 `xdotool` 正确，已留复现与判据）；
3. **有一个环境前提**：桌面输入法在中文态时会吃掉注入的 ASCII，需先 `ibus engine xkb:us::eng`。

## 7. 复现清单

```
宿主（Windows）
  G:\Linux\seelex-demo\
    runwith.ps1      通用带超时的进程执行器（抓 stdout/stderr/exit）
    vmguest.ps1      push 脚本 → Guest run → copyFrom 回收输出（含完成标记轮询）
    vmrun2.ps1       走 SSH 的远程 bash（stdin 传脚本）
    vmssh.ps1 / vmscp.ps1 / vmscpget.ps1   SSH / scp 上下行
    keys\seelex_ed25519(.pub)              专用密钥
    artifacts\                             三件演示素材
    各步骤脚本：probe1/2、nicfix、sshkey、headless_run、tui_*、invm_build3、record2 …

客户机（Ubuntu 22.04）
  ~/seelex-linux/      部署树（宿主交叉编译产物 + config + plugins）
  ~/seelex-build/      VM 内原生构建产物（v0.1.0-invm）
  ~/seelex-src.tgz / seelex-src-vendor.tgz / go1.25.8-linux-amd64.tgz
  ~/seelex-artifacts/  三件演示素材
  /usr/local/go        Go 1.25.8
  tmux 会话 seelexdemo 里跑着 TUI（200×45）
```

## 8. 已知限制 / 后续可做

- 客户机 `ens33` 的 IPv4 目前来自手工 `dhclient`，**重启后需要重做**；如需长期可用，建议为
  NetworkManager 建一个 ethernet 连接或在 netplan 里固定。
- GDM 自动登录已打开（演示便利），如不需要可把 `/etc/gdm3/custom.conf` 改回。
- 只装了演示必需的小工具（tmux / scrot / ffmpeg）。`make`、`socat`、`node`、`pip3` 在 VM 内缺失。
- VM 内 `go1.18.1`（apt 版）依旧存在；本次原生构建显式使用 `/usr/local/go`（1.25.8）。
- TUI 的演示会话依赖 `workspace_index.json` 里的 session→workspace 绑定（`/resume` 恢复）；
  全新会话默认没有项目绑定，工具会被 ProjectScope 拦下。