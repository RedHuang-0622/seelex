package dto

import (
	"encoding/json"
	"fmt"
)

// state_codec.go — 状态枚举的**唯一一份编码口径**（"状态机枚举统一"的落点）。
//
// 每一格状态机都长成同一个形状：
//
//	type XState uint8                   // 取值只能从下面那组常量来（iota，0 约定是"未知"）
//	var xStateWords = [...]string{...}  // 枚举 ↔ 对外词，一格一张表
//
//	func (s XState) String() string                   { return xStateWords.word(uint8(s)) }
//	func ParseX(text string) (XState, bool)           { o, ok := xStateWords.ordinal(text); return XState(o), ok }
//	func (s XState) MarshalJSON() ([]byte, error)     { return xStateWords.marshal(uint8(s)) }
//	func (s *XState) UnmarshalJSON(data []byte) error { return xStateWords.unmarshal(data, (*uint8)(s)) }
//
// 为什么四个方法的实现收在一处：它们是**同一份判据**——词怎么拼、认不认得、读不回来怎么办。
// 每格各写一遍，就是每格各写一遍错误消息、各写一遍越界处理、各写一遍未知词策略。
// 每格**不同**的只有那张表与类型名（格子名进错误消息，便于一眼看出是哪一格读不回）。
//
// 这一层不负责策略分歧：**落盘格**的未知词取舍在 seelebridge 的 `nodeStateOfRecord`
// （读旧文件不许炸），进程内的 wire 一律走这里的严格口径（读不懂就报错，不折成零值——
// 那会把"读不懂"变成某个已知状态，判定面上这是最贵的一类错误）。
type stateCodec struct {
	// name 是格子名（错误消息里说清是哪一格）。
	name string
	// words 下标 = 枚举序数；下标 0 约定是"未知"。
	words []string
}

// word 给出对外词。越界（将来加了常量却忘了加词）折回"未知"，不 panic：
// 展示层不该因为一个越界值炸掉。
func (c stateCodec) word(ordinal uint8) string {
	if int(ordinal) < len(c.words) {
		return c.words[ordinal]
	}
	return c.words[0]
}

// ordinal 把对外词读回枚举序数；第二个返回值报告认不认得。
func (c stateCodec) ordinal(text string) (uint8, bool) {
	for index, word := range c.words {
		if word == text {
			return uint8(index), true
		}
	}
	return 0, false
}

// marshal 让枚举的**整数值不出本进程**：JSON 里永远是词。
func (c stateCodec) marshal(ordinal uint8) ([]byte, error) {
	return json.Marshal(c.word(ordinal))
}

// unmarshal 读回对外词；认不得的词报错且**不动原值**。
func (c stateCodec) unmarshal(data []byte, into *uint8) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	ordinal, ok := c.ordinal(text)
	if !ok {
		return fmt.Errorf("dto: %q 不是%s的词（认得：%v）", text, c.name, c.words)
	}
	*into = ordinal
	return nil
}
