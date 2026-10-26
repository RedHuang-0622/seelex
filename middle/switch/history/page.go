// Package history 是中间层"历史分页"的落点（plan §2.4：`application/core/session_history.go`
// → `middle/switch/history/`）。当前落进来的是**窗口装配判据**：一页读回的历史怎么装进可见窗口
// ——按**行身份**（接缝）拼接、窗口边界与起点推算。
//
// 落点沿革：2026-10-16 随 M5-R6-history-page **接收者转移**从 `application/core/session_history.go`
// 归位；"从哪读"（冷读面 / 分页读回 / 会话存储端口）与写快照仍由宿主负责。
//
// 依赖纪律：本包只依赖 `middle/protocol/model`（消息形状）与 `contract/protocol`
// （窗口边界收敛助手），不读配置、不持锁、不碰快照。
package history

import (
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ReplaceTail 组装「回到最新 / 冷回读」的尾部窗口：以冷读页为底，把内存窗口里发布点之后的那段
// 热尾（本轮尚未落盘的行）按**行身份**接回页尾；页尾接上了热尾时窗口右界就是内存窗口的右界
// （内存窗口贴着有效尾），否则右界是可见总数（窗口右界 = 已发布的最新一条可见行）。
//
// 两个右界都在可见空间里，读回面多出来的内部标记行不会让窗口「永远差几格」。可见总数落后于磁盘
// 已发布的可见行（同一会话由单一视图台账维护，正常不会发生）时这里给出的是下界：窗口里仍是真实的
// 最新一行，只有起点偏保守。
func ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int) {
	rows := append([]model.Message(nil), pageRows...)
	end := memoryTotal
	if at := IndexOfSameRow(memoryRows, lastRow(rows)); at >= 0 {
		rows = append(rows, memoryRows[at+1:]...)
		end = memoryStart + len(memoryRows)
	}
	rows = protocol.BoundConversationTail(rows, window)
	visible := len(DurableRows(rows))
	if visible == 0 {
		return nil, memoryStart
	}
	start := end - visible
	if start < 0 {
		start = 0
	}
	return rows, start
}

// Prepend 组装「加载更早」的窗口：页里接缝（窗口首行）之前的可见行前置到窗口头部，窗口整体后退
// 一页（不改总数——总数是可见空间里所有已可见行）。
//
// 接缝对不上（窗口首行不在页里、或它前面没有可见行）时返回空：宁可窗口原地不动，也不在列表中间留
// 一段谁都没有的下标区间。窗口为空（没有对位锚）时没有「连续」可言：这一页就是窗口之前的一段，
// 按调用方给的可见下标推定位置。
func Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int) {
	if len(pageRows) == 0 {
		return nil, memoryStart
	}
	if len(memoryRows) == 0 {
		rows := protocol.BoundConversationTail(append([]model.Message(nil), pageRows...), window)
		start := memoryStart - len(DurableRows(rows))
		if start < 0 {
			start = 0
		}
		return rows, start
	}
	seam := IndexOfSameRow(pageRows, memoryRows[0])
	if seam <= 0 {
		return nil, memoryStart
	}
	earlier := append([]model.Message(nil), pageRows[:seam]...)
	if len(earlier) > window {
		earlier = earlier[len(earlier)-window:]
	}
	rows := protocol.BoundConversationHead(append(earlier, memoryRows...), window)
	start := memoryStart - len(earlier)
	if start < 0 {
		start = 0
	}
	return rows, start
}

// DurableRows 取出可见窗口里参与历史游标的行（system 引导行不占 durable 下标空间，
// 与 view_state.DurableConversationCount 同一口径）。
func DurableRows(messages []model.Message) []model.Message {
	rows := make([]model.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			continue
		}
		rows = append(rows, message)
	}
	return rows
}

// HasRowIdentity 报告一行是否带得出身份（空行无法对位）。
func HasRowIdentity(message model.Message) bool {
	return message.ID != "" || message.Content != "" || message.ReasoningContent != "" || message.Tool != nil
}

// SameByContent 按「角色 + 正文 + 思考」对位（没有稳定消息 ID 的行只能这样认）；空行（占位 assistant、
// 只有工具调用的 tool 行）不参与内容对位——两条空占位长得一模一样，认错就把接缝挪了位：
// 工具行按调用 ID 对位，其余空行一律不对位。
func SameByContent(left, right model.Message) bool {
	if left.Role != right.Role {
		return false
	}
	if left.Content != "" || left.ReasoningContent != "" {
		return left.Content == right.Content && left.ReasoningContent == right.ReasoningContent
	}
	if left.Tool != nil && right.Tool != nil {
		return left.Tool.ID != "" && left.Tool.ID == right.Tool.ID
	}
	return false
}

// IndexOfSameRow 返回 rows 里第一条与 want 同身份的行下标（-1 = 没找到）。先按消息 ID 认
// （同一条消息在内存窗口与磁盘行里同 ID，最稳）；ID 认不出来再退回内容对位——旧格式 provider 历史行
// 没有稳定 ID（每次读回都由调用方重新派号），只有「角色 + 正文 + 思考」可比。
func IndexOfSameRow(rows []model.Message, want model.Message) int {
	if want.ID != "" {
		for index := range rows {
			if rows[index].ID == want.ID {
				return index
			}
		}
	}
	for index := range rows {
		if SameByContent(rows[index], want) {
			return index
		}
	}
	return -1
}

// ContainsSameRow 报告 rows 里有没有与 want 同身份的行。
func ContainsSameRow(rows []model.Message, want model.Message) bool {
	return IndexOfSameRow(rows, want) >= 0
}

// lastRow 返回最后一行（空序列返回零值行：与任何行都不是同一条）。
func lastRow(rows []model.Message) model.Message {
	if len(rows) == 0 {
		return model.Message{}
	}
	return rows[len(rows)-1]
}
