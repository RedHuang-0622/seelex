package adapters

import (
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// session_status_record.go — 会话可见状态在**存储面**上的转换点（全仓唯一一处）。
//
// `sessionstore.Status` 是**契约之下的 wire**（存的是词；总表 docs/arch/state-machine-inventory.md
// §2 已把它登记为边界格）：它承载这一格的**持久子集**
// （draft | idle | running | queued | awaiting_approval | archived）。可见面那一格
// （`dto.SessionStatus`）比它多一个 `restoring`——那个态只在进程活着时成立，不落盘。
//
// 合并前这里是 `model.SessionStatus(item.Status)`：**无类型转换**——store 里冒出一个
// 表外的词，它照样流进可见面，读方按各自的口径猜（"这行到底在不在跑"）。现在这处转换
// 有了名字，口径也就有了落点（与记录状态的 `nodeStateOfRecord`、回合状态的
// `TurnStatusOfRecord` 同形状）：
//
//   - 认得的词（含 store 的全部持久词）→ 同一个枚举值；
//   - 认不得的词、空串（老记录没写这个字段）→ `SessionStatusUnknown`：**说认不得**，
//     不折成 idle/running 这些已知态。折成已知态等于替存储层编一个它没说过的事实，
//     而"这行在不在跑"正是驱逐/归档/待批判定会去读的东西。
func sessionStatusOfRecord(wire sessionstore.Status) dto.SessionStatus {
	if status, ok := dto.ParseSessionStatus(string(wire)); ok {
		return status
	}
	return dto.SessionStatusUnknown
}
