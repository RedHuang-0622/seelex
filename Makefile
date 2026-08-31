# Seelex Makefile
# 跨平台构建与打包

DEFAULT_VERSION := $(shell sed -n 's/^var Version = "\([^"]*\)"/\1/p' internal/buildinfo/version.go)
VERSION ?= $(if $(DEFAULT_VERSION),$(DEFAULT_VERSION),dev)
ARCHIVE_VERSION := $(patsubst v%,%,$(VERSION))
DIST ?= dist
POWERSHELL ?= powershell.exe
LOCAL_CONFIG ?= config/accounts.yaml
SMOKE_TARGET ?=
CONFIRMED ?=
GUI_PACKAGE := seelex-v$(ARCHIVE_VERSION)-windows-amd64-gui
GUI_PACKAGE_ROOT := $(DIST)/$(GUI_PACKAGE)
GUI_ARCHIVE := $(GUI_PACKAGE_ROOT).zip
GUI_CHECKSUM := $(GUI_ARCHIVE).sha256

# 目标平台: OS/ARCH
PLATFORMS := windows/amd64 linux/amd64 darwin/amd64 darwin/arm64

.PHONY: all release rebuild clean build package clean-gui build-gui dev-build-gui publish-build-gui rebuild-gui publish-rebuild-gui stage-gui smoke-gui deploy-gui rollback-gui release-dev dev-flow guard-dist guard-version guard-local-config help

## all: 安全清理、构建所有平台并打包
all: release

## release: 按 clean -> build -> package 顺序生成跨平台发布包
release: rebuild
	@$(MAKE) package VERSION="$(VERSION)" DIST="$(DIST)"
	@echo "=== 完成 ==="

## rebuild: 清理全部 dist 后重新构建所有平台
rebuild: clean
	@$(MAKE) build VERSION="$(VERSION)" DIST="$(DIST)"

## build: 仅构建二进制
build:
	@echo "[build] 版本: $(VERSION)"
	@for p in $(PLATFORMS); do \
		os=$$(echo $$p | cut -d/ -f1); \
		arch=$$(echo $$p | cut -d/ -f2); \
		ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		outdir="$(DIST)/$$os-$$arch"; \
		out="$$outdir/seelex$$ext"; \
		mkdir -p "$$outdir"; \
		echo "  GOOS=$$os GOARCH=$$arch -> $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags="-s -w -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=$(VERSION)" -o "$$out" . || exit 1; \
	done
	@echo "[build] 完成"

## package: 复制运行时文件 + 打包
package:
	@for p in $(PLATFORMS); do \
		os=$$(echo $$p | cut -d/ -f1); \
		arch=$$(echo $$p | cut -d/ -f2); \
		outdir="$(DIST)/$$os-$$arch"; \
		echo "[copy] $$outdir"; \
		mkdir -p "$$outdir/config"; \
		cp config/accounts.example.yaml "$$outdir/config/"; \
		cp config/seele.yaml config/seelex.yaml "$$outdir/config/"; \
		cp -r plugins "$$outdir/"; \
		cp LICENSE CHANGELOG.md README.md "$$outdir/"; \
		[ ! -f README_EN.md ] || cp README_EN.md "$$outdir/"; \
		dirname="seelex-v$(ARCHIVE_VERSION)-$$os-$$arch"; \
		cp -r "$$outdir" "$(DIST)/$$dirname"; \
		tar -czf "$(DIST)/$$dirname.tar.gz" -C "$(DIST)" "$$dirname"; \
		rm -rf "$(DIST)/$$dirname"; \
	done
	@echo "[package] 完成"

## guard-dist: 拒绝对仓库 dist 之外的路径执行 clean
guard-dist:
	@test "$(abspath $(DIST))" = "$(abspath dist)" || { \
		echo "refusing to clean unexpected DIST=$(DIST)"; \
		exit 1; \
	}

## guard-version: 拒绝可能形成路径逃逸的版本字符串
guard-version:
	@case "$(VERSION)" in \
		""|*[!A-Za-z0-9._-]*) echo "refusing unsafe VERSION=$(VERSION)"; exit 1 ;; \
		*) ;; \
	esac

## guard-local-config: 本地 GUI 构建必须提供真实账号配置
guard-local-config:
	@test -f "$(LOCAL_CONFIG)" || { \
		echo "local GUI account configuration is missing; set LOCAL_CONFIG to an existing file"; \
		exit 1; \
	}

## clean: 安全清理全部构建产物
clean: guard-dist
	@echo "[clean] $(abspath $(DIST))"
	rm -rf -- "$(DIST)"

## clean-gui: 只清理当前版本 Windows GUI 产物
clean-gui: guard-dist guard-version
	@echo "[clean-gui] $(GUI_PACKAGE)"
	rm -rf -- "$(GUI_PACKAGE_ROOT)" "$(GUI_ARCHIVE)" "$(GUI_CHECKSUM)"

## build-gui: dev-build-gui 的兼容别名
build-gui: dev-build-gui

## dev-build-gui: 构建开发用 Windows GUI，并不透明复制本地账号配置
dev-build-gui: guard-version guard-local-config
	$(POWERSHELL) -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
		-File scripts/build-gui.ps1 -Version "$(VERSION)" -BuildKind Dev -LocalConfigPath "$(LOCAL_CONFIG)"

## publish-build-gui: 构建可发布 Windows GUI，只包含公开配置 example
publish-build-gui: guard-version
	$(POWERSHELL) -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
		-File scripts/build-gui.ps1 -Version "$(VERSION)" -BuildKind Publish

## rebuild-gui: 按 clean-gui -> build-gui 顺序重建 Windows GUI
rebuild-gui: clean-gui
	@$(MAKE) dev-build-gui VERSION="$(VERSION)" DIST="$(DIST)" POWERSHELL="$(POWERSHELL)" LOCAL_CONFIG="$(LOCAL_CONFIG)"

## publish-rebuild-gui: 清理并重建只含 example 的可发布 Windows GUI
publish-rebuild-gui: clean-gui
	@$(MAKE) publish-build-gui VERSION="$(VERSION)" DIST="$(DIST)" POWERSHELL="$(POWERSHELL)"

## stage-gui: 阶段1 构建新 GUI 二进制到暂存区 tmp/staging-gui（不触碰基线工作区）
stage-gui: guard-version
	$(POWERSHELL) -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
		-File scripts/seelex-flow.ps1 -Stage Stage -Version "$(VERSION)"

## smoke-gui: 对暂存区/指定二进制做无头冒烟测试（SMOKE_TARGET 可指定路径）
smoke-gui: guard-version
	$(POWERSHELL) -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
		-File scripts/seelex-flow.ps1 -Stage Smoke -Version "$(VERSION)" -SmokeTarget "$(SMOKE_TARGET)"

## deploy-gui: 阶段2 部署暂存区二进制到基线工作区（进程检测+确认+stash 备份）
deploy-gui: guard-version
	$(POWERSHELL) -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
		-File scripts/seelex-flow.ps1 -Stage Deploy -Version "$(VERSION)" $(if $(CONFIRMED),-Yes)

## rollback-gui: 从 stash 回滚基线工作区到上一个可用版本
rollback-gui: guard-version
	$(POWERSHELL) -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
		-File scripts/seelex-flow.ps1 -Stage Rollback -Version "$(VERSION)" $(if $(CONFIRMED),-Yes)

## release-dev: 阶段3 构建各平台发布包（需 VERSION=tag，仅 example 配置，不清空 dist）
release-dev: guard-version
	$(POWERSHELL) -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
		-File scripts/seelex-flow.ps1 -Stage Release -Version "$(VERSION)" $(if $(CONFIRMED),-Yes)

## dev-flow: 一键分阶段流程 Stage -> Smoke -> Deploy -> Smoke -> Release（交互确认）
dev-flow: guard-version
	$(POWERSHELL) -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
		-File scripts/seelex-flow.ps1 -Stage All -Version "$(VERSION)" $(if $(CONFIRMED),-Yes)

## help: 显示帮助
help:
	@echo "可用目标:"
	@sed -n 's/^## //p' Makefile
