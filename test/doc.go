// Package test 承载 Seelex 的**仓库级集成用例**：原先散在仓库根目录的 57 个
// `*_test.go`（full-chain 装配、会话/工具链回归、插件与看板、发布契约、构建布局护栏）。
//
// 为什么它们要独立成包：仓库根是 `package main`（入口），而 Go 不允许别的包 import
// 它——散在根目录意味着这些用例只能贴着入口长，无法被 `go test ./test/` 单独点名，也
// 无法在"入口换成第二个 main（不同 build 变体）"时继续复用。M5 之后入口只剩 48 行，
// 用例统一由本包承载，通过 `wiring/{config,registry,bootstrap}` 的公开 API 摆现场。
//
// 运行：
//
//	go test ./test/ -count=1                     # 默认集合（真 API 用例自查环境变量并 skip）
//	go test -tags manualsmoke ./test/ -count=1   # 需要真实账号的手工冒烟族
//
// 依赖方向：本包只依赖生产包的公开 API（`wiring/*`、`application/*`、`store/*`…），
// 不复制任何业务判定；夹具就地建在临时目录里。
package test

import ()
