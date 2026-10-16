// 基座再导出：`store/root` 抽出以后，本包对**已发布 API** 保留同身份的别名，
// 使调用点与 `main.go` 零改动（类型身份与错误值身份都不变）。
//
// 纪律（AGENTS.md §8.1）：
//   - 别名只用于"形状与已发布符号"；本包内部一律直接调 `storeroot.X`，
//     不写同构包装函数（同一个判据不许有第二份实现）。
//   - 带状态与判据的实现（数据根锁、原子发布、共享读、残尾、Settings 解析）
//     的唯一落点在 `store/root`，本文件只转发同一个值/同一个类型。
package store

import storeroot "github.com/RedHuang-0622/seelex/store/root"

// Settings 是装配根注入的存储运行参数（唯一实现 = store/root.Settings）。
type Settings = storeroot.Settings

// storageSettings 是包内沿用的旧名，指向**同一类型身份**（搬迁保形，不是新类型）：
// 包内与测试的复合字面量因此零改动。
type storageSettings = storeroot.Settings

// retentionModeManual 的唯一定义在 `store/root`（RetentionModeManual）；这里只是
// 同一常量的本包别名，供 retention.go 沿用。
const retentionModeManual = storeroot.RetentionModeManual

// NewSettings 返回零值覆盖项（字段零值 = 用默认）。
var NewSettings = storeroot.NewSettings

// ErrDataRootLocked / ErrDataRootStaleLock 是数据根独占锁的两个哨兵错误：
// 转发的是**同一个错误值**，`errors.Is` 口径不变。
var (
	ErrDataRootLocked    = storeroot.ErrDataRootLocked
	ErrDataRootStaleLock = storeroot.ErrDataRootStaleLock
)

// DataRootLockedBy / DataRootLockedByWith 读该数据根当前的存活持有者。
var (
	DataRootLockedBy     = storeroot.DataRootLockedBy
	DataRootLockedByWith = storeroot.DataRootLockedByWith
)
