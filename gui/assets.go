package gui

import "embed"

// embeddedFrontend 包含启动必需的前端核心。体积较大的离线 vendor
// （mermaid/pdf.js/docx-preview/xterm/highlight）由发行包放到
// `<exe>/frontend/vendor`，运行时经 frontendAssets 覆盖到同一文件系统。
//
// 测试文件 *.test.mjs 也不进入生产二进制。
//
//go:embed frontend/dist/*.js frontend/dist/*.html frontend/dist/*.css frontend/dist/assets frontend/dist/themes frontend/dist/vendor/pico.min.css
var embeddedFrontend embed.FS
