# Linux GUI：构建落点（P6）与送进 Ubuntu 虚拟机

> 本文是操作手册。分区规范以 [`.claude/build-convention.md`](../../.claude/build-convention.md)
> 与 `scripts/build-layout.ps1` 为准；Linux GUI 的交付落点是 **P6 `dist/linux-amd64-gui/`**。

## 0. 两个事实（决定了整条路径）

1. **Linux GUI 是 cgo + GTK3 + WebKit2GTK 产物，Windows 无法交叉编译**
   （`CGO_ENABLED=0 GOOS=linux -tags gui,desktop,production` 会报 `undefined: Frontend`），
   必须由 Linux 侧构建。
2. **ABI 由 tag 决定，装错发行版直接起不来**：

   | tag | 链接 | 适用发行版 |
   |---|---|---|
   | `webkit2_40`（默认） | `webkit2gtk-4.0` + `libsoup-2.4` | Ubuntu 22.04 类 ← **本机那台 VM 就是这个** |
   | `webkit2_41` | `webkit2gtk-4.1` + `libsoup-3.0` | Ubuntu 24.04 类 |
   | 都不给 | `webkit2gtk-4.0`（legacy 分支） | 22.04 也能跑 |

   交付前用 `readelf -d <bin> | grep NEEDED` 核对，别只看能不能编译过。

## 1.（宿主）构建 Linux GUI —— 一条命令

```bash
make build-linux-gui VERSION=v0.1.1
```

等价于 `bash scripts/build-linux-gui.sh --version v0.1.1`。脚本会先用
`docker/linux-gui.Dockerfile` 烘一个**构建镜像** `seelex-linux-gui-builder:40`
（`ubuntu:22.04` + `libgtk-3-dev` + `libwebkit2gtk-4.0-dev` + Go 1.25.8），
之后每次构建都在这个镜像里跑 `go build -tags "gui,desktop,production,webkit2_40" -mod=vendor`
（离线，不需要 GOPROXY），并就地核对 `readelf -d` 与 `-version`。

> 镜像只是**编译机**，不是交付物，不进 `dist/`；不想要可以随时
> `docker rmi seelex-linux-gui-builder:40`（代价是下次构建要重装一遍依赖）。
> 国内网络下建议加镜像源，否则光 apt 就可能十几分钟（实测见 §6）：
>
> ```bash
> make build-linux-gui VERSION=v0.1.1 \
>   LINUX_GUI_BUILD_ARGS="--apt-mirror http://mirrors.tuna.tsinghua.edu.cn"
> ```

产物：

```text
dist/linux-amd64-gui/
  seelex-gui              Linux cgo 产物（22.04 ABI）
  seelex-logo.png         品牌图（每个交付文件夹各一份）
  config/                 accounts.example.yaml / seele.yaml / seelex.yaml / README.md
  plugins/                default + freecad
  README.md  README_EN.md  CHANGELOG.md  LICENSE
dist/archive/seelex-v0.1.1-linux-amd64-gui.tar.gz(+.sha256)
```

其它入口：

```bash
# 已有二进制（例如在 VM 内构建好的），只补齐交付树、不重建
make pack-linux-gui STAGED_LINUX_GUI=/path/to/seelex-gui

# 在 Linux 主机/虚拟机内就地构建（不需要 Docker）
bash scripts/build-linux-gui.sh --version v0.1.1 --native

# 换 ABI（要 24.04 版时）
bash scripts/build-linux-gui.sh --version v0.1.1 --webkit 41 --image ubuntu:24.04
```

没有 Docker 时的退路：在那台 Ubuntu 22.04 虚拟机里用 `--native`（见 §4）。

## 2.（宿主 → 虚拟机）把交付树送进去

不需要 SSH、不需要端口转发。二选一：

### 2a. 拖拽

把 `dist/linux-amd64-gui/seelex-gui` 拖进 VMware 的 Ubuntu 窗口（VMware Tools 拖放），
或整目录打包后再拖。

### 2b. vmcli（走 VMware Tools 的客户机文件通道）

```powershell
$vmcli = 'G:\Tools\VMware\vmcli.exe'; $vmx = 'G:\Linux\msi\Ubuntu.vmx'
& $vmcli $vmx Guest copyTo -u red -p '<密码>' -o `
  'G:\Program\go\seelex\dist\linux-amd64-gui\seelex-gui' '/home/red/seelex-gui-v0.1.1'
```

## 3.（虚拟机内）核对 ABI 后覆盖

```bash
ldd ~/seelex-gui-v0.1.1 | grep -E 'webkit|gtk-3|soup|not found'   # 要 4.0 + libsoup-2.4，无 not found
~/seelex-gui-v0.1.1 -version                                       # v0.1.1

cd ~/seelex-linux
cp -a seelex-gui "seelex-gui.bak-$(date +%Y%m%d-%H%M%S)"           # 先备份
install -m 755 ~/seelex-gui-v0.1.1 seelex-gui                      # 覆盖
cd ~/seelex-linux && ./seelex-gui                                  # 启动
```

回滚：`cp -a ~/seelex-linux/seelex-gui.bak-<时间戳> ~/seelex-linux/seelex-gui`。

## 4.（虚拟机内自行构建，无 Docker 时的退路）

虚拟机已核实具备：Ubuntu 22.04.5 / Go 1.25.8（`/usr/local/go/bin/go`）/ `gcc` `make` `pkg-config` /
`gtk+-3.0` 与 `webkit2gtk-4.0` 开发包；**但没有外网**（DNS 解析失败），所以只能用 vendor 离线构建。

```bash
# 源码包（含 vendor 与 gui/frontend/dist）拖进客户机后：
cd ~ && sha256sum seelex-src-v0.1.1.tgz    # 应为 b2bc44e0…1961
rm -rf ~/seelex-src && mkdir -p ~/seelex-src && tar -xzf seelex-src-v0.1.1.tgz -C ~/seelex-src
cd ~/seelex-src

export PATH=/usr/local/go/bin:$PATH
export GOFLAGS=-mod=vendor GOPROXY=off GOCACHE=$HOME/.cache/go-build
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
  -tags "gui,desktop,production,webkit2_40" -trimpath \
  -ldflags "-s -w \
    -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=v0.1.1 \
    -X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui" \
  -o /tmp/seelex-gui-v0.1.1 .

file /tmp/seelex-gui-v0.1.1
ldd  /tmp/seelex-gui-v0.1.1 | grep -E 'webkit|gtk-3|soup'
/tmp/seelex-gui-v0.1.1 -version
```

> 客户机内的源码包必须含 `gui/frontend/dist/`（132 个文件）——前端由
> `gui/assets.go` 的 `//go:embed all:frontend/dist` 编进二进制，缺了会构建失败或界面空白。

## 5. 品牌图

唯一事实源是**入库的** `gui/icons/seelex-logo.png`（曾经只存在于下载目录的
`Seelex_logo.png` 已入库为它）。打包步骤把它复制为每个交付树里的
`seelex-logo.png`，所以从任一交付文件夹出发都能就地取到品牌图，不依赖任何本机下载路径。
这条由 `go test . -run 'BuildLayout|BrandLogo'` 钉住。

## 6. 本次实测（v0.1.1，2026-09-30）

构建环境：`seelex-linux-gui-builder:40`（首次烘镜像，之后复用）
= `ubuntu:22.04` + `gtk+-3.0 3.24.33` + `webkit2gtk-4.0 2.50.4` + `go1.25.8 linux/amd64`。

```text
$ readelf -d dist/linux-amd64-gui/seelex-gui | grep NEEDED
  libwebkit2gtk-4.0.so.37      ← 22.04 ABI（不是 4.1）
  libjavascriptcoregtk-4.0.so.18
  libsoup-2.4.so.1             ← libsoup2
  libgtk-3.so.0  libgdk-3.so.0  libgdk_pixbuf-2.0.so.0
  libgio-2.0.so.0  libgobject-2.0.so.0  libglib-2.0.so.0  libc.so.6
$ dist/linux-amd64-gui/seelex-gui -version
v0.1.1
```

| 交付物 | 大小 | sha256 |
|---|---|---|
| `dist/linux-amd64-gui/seelex-gui` | 31,039,648 B | `51e7d47e643f3399963f6fa9171d28cdec9757605bc94d4890fdce37760cbd55` |
| `dist/archive/seelex-v0.1.1-linux-amd64-gui.tar.gz` | 11,764,245 B | `27bfa387b6e3d65ddc31275dacc15ae5f1126b863339771b3f41798095001ec1` |

**国内网络：为什么一定要加 `--apt-mirror`**（同一台机、同一文件实测）：

| 源 | 实测速度 | 备注 |
|---|---|---|
| `mirrors.tuna.tsinghua.edu.cn` | **4.4 MB/s** | 本次使用 |
| `mirrors.ustc.edu.cn` | 2.8 MB/s | |
| `mirrors.163.com` | 1.8 MB/s | |
| `mirrors.aliyun.com` | 0.15 MB/s | 曾用它跑 7 分钟只下到 1MB，apt 卡死 |
| `go.dev`（Go 官方） | **16.1 MB/s** | Go 用官方源最快；aliyun 的 golang 镜像只有 0.12 MB/s |

另外两个坑（脚本已处理）：`ubuntu:22.04` 裸镜像**没有 CA bundle**，直接换 https 镜像源会
`certificate NOT trusted`（脚本自动降级为 http）；`go version` 必须在
`export PATH=/usr/local/go/bin:$PATH` 之后调用。