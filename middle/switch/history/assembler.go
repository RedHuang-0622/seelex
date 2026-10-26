package history

import "github.com/RedHuang-0622/seelex/middle/protocol/model"

// Assembler 是窗口装配判据的只读实现面。
//
// 每个方法逐项转发本包已有纯函数；这里不持有状态，也不复制行身份或窗口
// 边界判据。Application 只依赖自己的 WindowAssembler 端口，由 Wiring 注入本类型。
type Assembler struct{}

// ReplaceTail 转发现有 ReplaceTail 实现。
func (Assembler) ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int) {
	return ReplaceTail(pageRows, memoryRows, memoryStart, memoryTotal, window)
}

// Prepend 转发现有 Prepend 实现。
func (Assembler) Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int) {
	return Prepend(pageRows, memoryRows, memoryStart, window)
}

// DurableRows 转发现有 DurableRows 实现。
func (Assembler) DurableRows(messages []model.Message) []model.Message {
	return DurableRows(messages)
}

// IndexOfSameRow 转发现有行身份对位实现。
func (Assembler) IndexOfSameRow(rows []model.Message, want model.Message) int {
	return IndexOfSameRow(rows, want)
}

// ContainsSameRow 转发现有同身份行判据。
func (Assembler) ContainsSameRow(rows []model.Message, want model.Message) bool {
	return ContainsSameRow(rows, want)
}

// HasRowIdentity 转发现有行身份有效性判据。
func (Assembler) HasRowIdentity(message model.Message) bool {
	return HasRowIdentity(message)
}
