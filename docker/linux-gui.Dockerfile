# ============================================================================
# Linux GUI 构建镜像（一次性烘好，供 scripts/build-linux-gui.sh 复用）
# ----------------------------------------------------------------------------
# 为什么需要它：`ubuntu:22.04` 裸镜像里既没有 CA、也没有 GTK/WebKit 开发包，
# 每次构建都要重新 apt 下载 ~200MB（国内到 archive.ubuntu.com 经常十几分钟）。
# 把这一步烘成镜像后，后续每次构建只需要 go build（分钟级 → 秒级）。
#
# 构建（通常由 scripts/build-linux-gui.sh 自动调用）：
#   docker build -f docker/linux-gui.Dockerfile \
#     --build-arg APT_MIRROR=http://mirrors.tuna.tsinghua.edu.cn \
#     --build-arg WEBKIT_PKG=webkit2gtk-4.0 \
#     --build-arg GO_URL=https://mirrors.tuna.tsinghua.edu.cn/golang/go1.25.8.linux-amd64.tar.gz \
#     -t seelex-linux-gui-builder:40 docker
#
# ABI：webkit2gtk-4.0 + libsoup-2.4 → Ubuntu 22.04 类；要 24.04 类请 WEBKIT_PKG=webkit2gtk-4.1
# ============================================================================
ARG BASE=ubuntu:22.04
FROM ${BASE}

ARG APT_MIRROR=
ARG WEBKIT_PKG=webkit2gtk-4.0
ARG GO_VERSION=1.25.8
ARG GO_URL=

ENV DEBIAN_FRONTEND=noninteractive

RUN set -eux; \
    if [ -n "$APT_MIRROR" ] && [ -f /etc/apt/sources.list ]; then \
      m="$APT_MIRROR"; \
      case "$m" in https://*) \
        if [ ! -f /etc/ssl/certs/ca-certificates.crt ]; then m="http://${m#https://}"; fi ;; \
      esac; \
      sed -i -E "s#https?://(archive|security)\.ubuntu\.com#${m}#g" /etc/apt/sources.list; \
    fi; \
    apt-get -o Acquire::http::Timeout=20 -o Acquire::Retries=5 -o Acquire::ForceIPv4=true update -qq; \
    apt-get -o Acquire::http::Timeout=20 -o Acquire::Retries=5 -o Acquire::ForceIPv4=true install -y -qq \
      --no-install-recommends \
      libgtk-3-dev "lib${WEBKIT_PKG}-dev" pkg-config build-essential ca-certificates curl; \
    rm -rf /var/lib/apt/lists/*; \
    pkg-config --modversion gtk+-3.0 "${WEBKIT_PKG}"

# Go 单独一层：换 URL 重烘时不会作废上面的 apt 层（apt 是这里最贵的一步）。
RUN set -eux; \
    url="${GO_URL:-https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz}"; \
    echo "[image] Go tarball: $url"; \
    curl -fsSL --retry 3 --retry-delay 2 -m 900 -o /tmp/go.tgz "$url"; \
    rm -rf /usr/local/go; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm -f /tmp/go.tgz; \
    /usr/local/go/bin/go version

ENV PATH=/usr/local/go/bin:${PATH}
WORKDIR /wsrc
