package adapters

import (
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	middlehistory "github.com/RedHuang-0622/seelex/middle/switch/history"
)

var _ applicationhistory.WindowAssembler = middlehistory.Assembler{}

// NewHistoryWindowAssembler 返回 Middle 窗口判据的端口实现。
//
// 返回类型是消费方端口，具体实现直接由 middle/switch/history.Assembler 结构
// 满足；本函数不包装、不持有状态，也不复制任何判据。
func NewHistoryWindowAssembler() applicationhistory.WindowAssembler {
	return middlehistory.Assembler{}
}
