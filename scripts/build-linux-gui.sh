#!/usr/bin/env bash
# ============================================================================
# Linux GUI 构建入口（P6 交付树：dist/linux-amd64-gui/）
# ----------------------------------------------------------------------------
# 为什么需要专门的入口：Linux GUI 是 cgo + GTK3 + WebKit2GTK 产物，Windows 上
# 无法交叉编译（`CGO_ENABLED=0 GOOS=linux -tags gui,...` 会报 undefined: Frontend），
# 因此它一直游离在 make/flow/CI 之外，只能靠手工脚本。本脚本把这条路径固化：
#
#   scripts/build-linux-gui.sh --version v0.1.1              # Docker(ubuntu:22.04) 构建 + 打包
#   scripts/build-linux-gui.sh --version v0.1.1 --native     # 在 Linux 主机上就地构建
#   scripts/build-linux-gui.sh --version v0.1.1 --pack-only  # 用已有二进制补齐交付树
#   scripts/build-linux-gui.sh --pack-only --binary /path/seelex-gui   # 用外部（VM 内）产物
#
# ABI 由构建 tag 决定，装错发行版直接起不来：
#   webkit2_40 -> webkit2gtk-4.0 + libsoup-2.4（Ubuntu 22.04 类）  ← 默认
#   webkit2_41 -> webkit2gtk-4.1 + libsoup-3.0（Ubuntu 24.04 类）
#
# 落点（分区规范见 .claude/build-convention.md）：
#   dist/linux-amd64-gui/seelex-gui + config/ + plugins/ + README/LICENSE/CHANGELOG
#   + seelex-logo.png（品牌图随每个交付文件夹各存一份）
#   dist/archive/seelex-v<v>-linux-amd64-gui.tar.gz(+.sha256)
# ============================================================================
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

VERSION="dev"
WEBKIT=40
IMAGE="${LINUX_GUI_DOCKER_IMAGE:-ubuntu:22.04}"
GO_VER="${LINUX_GUI_GO_VERSION:-1.25.8}"
# 可选加速（默认关闭，保持公开可复现）：
#   --apt-mirror 换 apt 源（国内网络下 archive.ubuntu.com 会慢到十几分钟）
#   --go-tarball 用本机已有的 go<ver>.linux-amd64.tar.gz，省掉 60MB 下载
APT_MIRROR="${LINUX_GUI_APT_MIRROR:-}"
GO_TARBALL="${LINUX_GUI_GO_TARBALL:-}"
# Go 官方 tarball 地址；国内可用 --go-url 换成镜像（如
# https://mirrors.tuna.tsinghua.edu.cn/golang/go1.25.8.linux-amd64.tar.gz）
GO_URL="${LINUX_GUI_GO_URL:-}"
MODE="docker"
BINARY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --webkit)  WEBKIT="$2"; shift 2 ;;
    --image)   IMAGE="$2"; shift 2 ;;
    --apt-mirror) APT_MIRROR="$2"; shift 2 ;;
    --go-tarball) GO_TARBALL="$2"; shift 2 ;;
    --go-url)  GO_URL="$2"; shift 2 ;;
    --native)  MODE="native"; shift ;;
    --pack-only) MODE="pack"; shift ;;
    --binary)  BINARY="$2"; shift 2 ;;
    -h|--help) sed -n '2,30p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "[build-linux-gui] unknown argument: $1" >&2; exit 2 ;;
  esac
done

case "$WEBKIT" in
  40) PC_VER="webkit2gtk-4.0"; TAG="webkit2_40" ;;
  41) PC_VER="webkit2gtk-4.1"; TAG="webkit2_41" ;;
  *) echo "[build-linux-gui] --webkit 只接受 40 或 41（收到 $WEBKIT）" >&2; exit 2 ;;
esac

# 构建镜像名：同一 ABI 一个镜像，烘一次长期复用（GTK/WebKit 开发包 + Go 都在里面）。
DEFAULT_IMAGE="seelex-linux-gui-builder:$WEBKIT"
if [[ -z "${LINUX_GUI_DOCKER_IMAGE:-}" && "$IMAGE" == "ubuntu:22.04" ]]; then
  IMAGE="$DEFAULT_IMAGE"
fi

OUTDIR="$ROOT/dist/linux-amd64-gui"
OUTBIN="$OUTDIR/seelex-gui"
ARCHIVE_DIR="$ROOT/dist/archive"
PACKAGE="seelex-v${VERSION#v}-linux-amd64-gui"
BRAND_LOGO="$ROOT/gui/icons/seelex-logo.png"

log() { echo "[build-linux-gui] $*"; }

# ── 打包：补齐运行时文件 + 品牌图 + 归档 ────────────────────────────────────
pack() {
  [[ -f "$OUTBIN" ]] || { log "缺少 $OUTBIN（先构建，或用 --binary 指定产物）"; exit 1; }
  chmod 755 "$OUTBIN"
  mkdir -p "$OUTDIR/config"
  cp config/accounts.example.yaml config/README.md "$OUTDIR/config/"
  cp config/seele.yaml config/seelex.yaml "$OUTDIR/config/"
  rm -rf -- "$OUTDIR/plugins"
  cp -r plugins "$OUTDIR/"
  cp LICENSE CHANGELOG.md README.md "$OUTDIR/"
  [[ ! -f README_EN.md ]] || cp README_EN.md "$OUTDIR/"
  # 品牌图：每个交付文件夹各备份一份，交付树自足（唯一事实源是 gui/icons/seelex-logo.png）
  cp "$BRAND_LOGO" "$OUTDIR/seelex-logo.png"
  log "交付树: $OUTDIR"

  mkdir -p "$ARCHIVE_DIR"
  rm -rf -- "$ARCHIVE_DIR/$PACKAGE"
  cp -r "$OUTDIR" "$ARCHIVE_DIR/$PACKAGE"
  tar -czf "$ARCHIVE_DIR/$PACKAGE.tar.gz" -C "$ARCHIVE_DIR" "$PACKAGE"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$ARCHIVE_DIR" && sha256sum "$PACKAGE.tar.gz" > "$PACKAGE.tar.gz.sha256")
  fi
  rm -rf -- "$ARCHIVE_DIR/$PACKAGE"
  log "归档: $ARCHIVE_DIR/$PACKAGE.tar.gz"
}

if [[ "$MODE" == "pack" ]]; then
  if [[ -n "$BINARY" ]]; then
    [[ -f "$BINARY" ]] || { log "找不到 --binary $BINARY"; exit 1; }
    mkdir -p "$OUTDIR"
    cp "$BINARY" "$OUTBIN"
    log "采用外部产物: $BINARY"
  fi
  pack
  exit 0
fi

mkdir -p "$OUTDIR"

if [[ "$MODE" == "native" ]]; then
  if ! pkg-config --exists gtk+-3.0 "$PC_VER"; then
    log "本机缺 cgo 依赖：需要 gtk+-3.0 与 $PC_VER 的开发包（本机 = $(uname -s)）"
    exit 3
  fi
  log "native 构建 ($PC_VER, tags=$TAG) -> $OUTBIN"
  export GOFLAGS=-mod=vendor GOPROXY=off
  CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -tags "gui,desktop,production,$TAG" -trimpath \
    -ldflags "-s -w -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=$VERSION -X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui" \
    -o "$OUTBIN" . || { log "构建失败"; exit 1; }
  file "$OUTBIN" || true
  "$OUTBIN" -version || { log "-version 探针失败"; exit 1; }
  pack
  exit 0
fi

command -v docker >/dev/null 2>&1 || {
  log "没有 docker，也没有 --native：Linux GUI 只能在 Linux 侧构建"
  log "  选项 A: 安装 Docker Desktop 后重跑本脚本"
  log "  选项 B: 在 Linux 主机/虚拟机里用 --native"
  log "  选项 C: 在别处构建好二进制后 --pack-only --binary <path>"
  exit 4
}

log "docker 构建: $IMAGE ($PC_VER, tags=$TAG) -> $OUTBIN"

# 自动发现本机 Go 工具链包（_tmp/ 或家目录），省掉容器里的 60MB 下载。
if [[ -z "$GO_TARBALL" ]]; then
  for cand in "$ROOT/_tmp/go${GO_VER}.linux-amd64.tar.gz" "$HOME/go${GO_VER}.linux-amd64.tar.gz" "$ROOT/_tmp/go${GO_VER}.tar.gz"; do
    if [[ -f "$cand" ]]; then GO_TARBALL="$cand"; break; fi
  done
fi

# 构建镜像不存在就先烘一次（GTK/WebKit 开发包 + Go 都装进去，之后每次构建只跑 go build）。
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  if [[ -f "$ROOT/docker/linux-gui.Dockerfile" ]]; then
    log "构建镜像不存在，先烘一次: $IMAGE（含 GTK3/$PC_VER 开发包 + Go $GO_VER，之后复用）"
    bake_args=(--build-arg "WEBKIT_PKG=$PC_VER" --build-arg "GO_VERSION=$GO_VER")
    [[ -z "$APT_MIRROR" ]] || bake_args+=(--build-arg "APT_MIRROR=$APT_MIRROR")
    [[ -z "$GO_URL" ]] || bake_args+=(--build-arg "GO_URL=$GO_URL")
    docker build -f "$ROOT/docker/linux-gui.Dockerfile" "${bake_args[@]}" -t "$IMAGE" "$ROOT/docker" \
      || { log "构建镜像失败（可先用 --image ubuntu:22.04 走现场安装那条路）"; exit 1; }
  else
    log "镜像 $IMAGE 不存在且没有 docker/linux-gui.Dockerfile"
    exit 1
  fi
fi
log "使用镜像: $IMAGE"

DOCKER_ARGS=(--rm -v "$ROOT:/wsrc:ro" -v "$OUTDIR:/out"
  -e VERSION="$VERSION" -e GO_VER="$GO_VER" -e PC_VER="$PC_VER" -e TAG="$TAG"
  -e APT_MIRROR="$APT_MIRROR")
if [[ -n "$GO_TARBALL" ]]; then
  [[ -f "$GO_TARBALL" ]] || { log "--go-tarball 不存在: $GO_TARBALL"; exit 1; }
  log "Go 工具链用本机包: $GO_TARBALL"
  DOCKER_ARGS+=(-v "$GO_TARBALL:/go-toolchain.tar.gz:ro")
fi
[[ -z "$APT_MIRROR" ]] || log "apt 源换成: $APT_MIRROR"

docker run "${DOCKER_ARGS[@]}" \
  -w /wsrc "$IMAGE" bash -c '
    set -euo pipefail
    export PATH=/usr/local/go/bin:$PATH
    # 构建镜像里已经装好依赖；只有用裸镜像（--image ubuntu:22.04）时才现场安装。
    if ! pkg-config --exists gtk+-3.0 "${PC_VER}"; then
      export DEBIAN_FRONTEND=noninteractive
      if [ -n "${APT_MIRROR:-}" ] && [ -f /etc/apt/sources.list ]; then
        eff_mirror="$APT_MIRROR"
        case "$eff_mirror" in
          https://*)
            # base 镜像自带没有 CA bundle，此时 https 源一律握手失败（certificate NOT trusted）。
            if [ ! -f /etc/ssl/certs/ca-certificates.crt ]; then
              echo "[container] base image has no CA bundle; using http for the mirror"
              eff_mirror="http://${eff_mirror#https://}"
            fi ;;
        esac
        sed -i -E "s#https?://(archive|security)\.ubuntu\.com#${eff_mirror}#g" /etc/apt/sources.list
        echo "[container] apt mirror: $eff_mirror"
      fi
      APT_OPTS="-o Acquire::http::Timeout=20 -o Acquire::Retries=5 -o Acquire::ForceIPv4=true"
      apt-get $APT_OPTS update -qq
      apt-get $APT_OPTS install -y -qq --no-install-recommends \
        libgtk-3-dev "${PC_VER}-dev" pkg-config build-essential ca-certificates curl
    fi
    if ! go version 2>/dev/null | grep -q "go${GO_VER}"; then
      if [ -f /go-toolchain.tar.gz ]; then
        echo "[container] Go from local toolchain tarball"
        rm -rf /usr/local/go && tar -C /usr/local -xzf /go-toolchain.tar.gz
      else
        echo "[container] downloading Go ${GO_VER}"
        curl -fsSL -m 900 -o /tmp/go.tgz "https://go.dev/dl/go${GO_VER}.linux-amd64.tar.gz"
        rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz
      fi
    fi
    echo "[container] gtk+-3.0=$(pkg-config --modversion gtk+-3.0) ${PC_VER}=$(pkg-config --modversion "$PC_VER") | $(go version)"
    export GOFLAGS=-mod=vendor GOPROXY=off GOCACHE=/tmp/gocache
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
      -tags "gui,desktop,production,${TAG}" -trimpath \
      -ldflags "-s -w -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=${VERSION} -X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui" \
      -o /out/seelex-gui .
    echo "--- ABI ---"
    readelf -d /out/seelex-gui | grep NEEDED
    /out/seelex-gui -version
  ' || { log "docker 构建失败"; exit 1; }

pack
