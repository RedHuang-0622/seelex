# Seelex Makefile
# 跨平台构建与打包

VERSION ?= dev
DIST   := dist

# 目标平台: OS/ARCH
PLATFORMS := windows/amd64 linux/amd64 darwin/amd64 darwin/arm64

.PHONY: all clean build package help

## all: 构建所有平台 + 打包
all: clean build package
	@echo "=== 完成 ==="

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
			go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o "$$out" . || exit 1; \
	done
	@echo "[build] 完成"

## package: 复制运行时文件 + 打包
package:
	@for p in $(PLATFORMS); do \
		os=$$(echo $$p | cut -d/ -f1); \
		arch=$$(echo $$p | cut -d/ -f2); \
		outdir="$(DIST)/$$os-$$arch"; \
		echo "[copy] $$outdir"; \
		cp -r config "$$outdir/"; \
		cp -r plugins "$$outdir/"; \
		cp seele.yaml "$$outdir/"; \
		dirname="seelex-v$(VERSION)-$$os-$$arch"; \
		cp -r "$$outdir" "$(DIST)/$$dirname"; \
		tar -czf "$(DIST)/$$dirname.tar.gz" -C "$(DIST)" "$$dirname"; \
		rm -rf "$(DIST)/$$dirname"; \
	done
	@echo "[package] 完成"

## clean: 清理构建产物
clean:
	rm -rf $(DIST)

## help: 显示帮助
help:
	@echo "可用目标:"
	@sed -n 's/^## //p' Makefile
