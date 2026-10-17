package dto

// UnitStatus 是恢复模板里「一个未完成单元」在持久化事实中的状态。
//
// 它是**粗分**（三桶），不是记录状态那一格的同义词：记录状态回答"这一轮跑到哪"
// （queued|running|done|failed|interrupted），这一格回答"重启后这一单元要不要续跑"——
// 由记录状态折一次得到（写方唯一：seelebridge 的恢复单元折叠；读方唯一：`Unit.Active()`）。
//
// 三桶：active（未终结，要续跑）/ done（已成功终结，只补历史）/ failed（已失败终结，
// 只补历史）。
type UnitStatus uint8

const (
	// UnitStatusUnknown 是零值：没折叠过，或读回来的词认不得。
	UnitStatusUnknown UnitStatus = iota
	// UnitActive = 单元未终结（派发已发生、结果未记录），需要重启续跑。
	UnitActive
	// UnitDone = 单元已成功终结，只补历史不重启。
	UnitDone
	// UnitFailed = 单元已失败终结，只补历史不重启。
	UnitFailed
)

// unitStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var unitStatusWords = [...]string{
	UnitStatusUnknown: "unknown",
	UnitActive:        "active",
	UnitDone:          "done",
	UnitFailed:        "failed",
}

var unitStatusCodec = stateCodec{name: "恢复单元状态", words: unitStatusWords[:]}

// String 给出对外词。
func (s UnitStatus) String() string { return unitStatusCodec.word(uint8(s)) }

// ParseUnitStatus 把对外词读回枚举；第二个返回值报告认不认得。
func ParseUnitStatus(text string) (UnitStatus, bool) {
	ordinal, ok := unitStatusCodec.ordinal(text)
	return UnitStatus(ordinal), ok
}

// Active 报告这一单元是否要续跑（唯一判据：`Unit.Active()` 转调它）。
func (s UnitStatus) Active() bool { return s == UnitActive }

// MarshalJSON 保住 wire 形状：JSON 里仍是 "active" 这样的词。
func (s UnitStatus) MarshalJSON() ([]byte, error) { return unitStatusCodec.marshal(uint8(s)) }

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *UnitStatus) UnmarshalJSON(data []byte) error {
	return unitStatusCodec.unmarshal(data, (*uint8)(s))
}
