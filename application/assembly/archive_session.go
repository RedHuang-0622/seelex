package assembly

// archive_session.go — **C2 ArchiveSession** 的宿主适配 + 门面转发。
//
// 业务事实：把会话置为 archived（record 级粘性状态）。归档只改 record 状态通道，
// history/transcript/tool-results/context 四片原样保留，会话从常规目录（分格枚举）
// 隐藏，仍可按 ID 冷读/重开（存储层枚举不丢行）。门控复用会话忙态语义：
// running/queued/awaiting_approval 一律拒绝，与驱逐前置 flush 同一顺序（先 flush +
// 释放引擎 bundle，再写状态），保证与驱逐/冷读并发时 record 是完整快照。
//
// 判据（门控次序 → 读 record → flush/unload/摘驻留序 → 写归档状态 → 释放后台命令 →
// 按项目刷新目录）已于 M5-R6-archive 随接收者转移归位 `middle/residency/archive.go`
// （plan §2.4）；本文件只剩"把宿主读数与存储/引擎落点装成端口"这条装配缝与一行转发。

import applicationsession "github.com/RedHuang-0622/seelex/application/session"

// ErrSessionNotFoundArchive 表示目标会话不存在或 record 缺失/版本不兼容。
// 判据经 `application/session.ResidencyPort` 注入；本名字是宿主侧兼容面，与
// `application/session.ErrArchiveNotFound`
// 是同一个错误值（`errors.Is` 两个名字都成立）。
var ErrSessionNotFoundArchive = applicationsession.ErrArchiveNotFound

// ArchiveSession 归档指定会话（判据与文案在 `middle/residency`）。
