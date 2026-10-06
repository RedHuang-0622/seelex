package model

import (
	"encoding/json"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ToolCallStatusOfRecord 是"工具调用视图词"这一格在**记录/边界那一侧**的唯一转换点：
// 把记录里的词折成本格的枚举。认不得的词与空词都落到 `dto.ToolEventUnknown`
// ——**不炸**，也**不折成已知状态**：没有词不等于"成功"。
//
// 为什么这一格要一个具名转换点：`model.ToolCall` 同时是存档（`model.SessionArchive`）与
// 事件 payload 的 JSON 形状，字段本身在契约之上（枚举），而磁盘上的老文件里放的是**词**
// （历史写点写过 success / running / error，也有过没填这一栏的记录）。契约枚举的
// `UnmarshalJSON` 认不得的词会报错，直接把枚举塞进字段会把"老存档里一个没见过的词"
// 升级成"整个会话读不回来"。
func ToolCallStatusOfRecord(word string) dto.ToolEventStatus {
	if status, ok := dto.ParseToolEventStatus(word); ok {
		return status
	}
	return dto.ToolEventUnknown
}

// UnmarshalJSON 是这一格的落盘读法：`status` 按**词**读，经 ToolCallStatusOfRecord 折一次；
// 其余字段与默认解码逐字一致（别名类型不带方法，因此不会递归调回本函数）。
//
// 出去那一侧（MarshalJSON）是默认的：枚举的 MarshalJSON 写出的仍是本格的词，
// wire 形状与枚举化之前逐字相同。
func (c *ToolCall) UnmarshalJSON(data []byte) error {
	type plain ToolCall
	var raw struct {
		plain
		Status string `json:"status"`
	}
	raw.plain = plain(*c)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = ToolCall(raw.plain)
	c.Status = ToolCallStatusOfRecord(raw.Status)
	return nil
}
