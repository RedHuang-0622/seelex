package model

import protocol "github.com/RedHuang-0622/seelex/contract/protocol"

// 编译期钉子：兼容 facade 必须保持同身份。若这里改成同名但独立的 named type，
// 下列无转换赋值将无法编译。
var (
	_ protocol.Snapshot            = Snapshot{}
	_ protocol.RuntimeState        = RuntimeState{}
	_ protocol.SessionRecord       = SessionRecord{}
	_ protocol.ToolCall            = ToolCall{}
	_ protocol.VisibleOutputStream = VisibleOutputStream{}
)
