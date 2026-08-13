package model

// NodeResultRefPrefix 是节点工具结果引用的前缀（ref = node:<nodeID>:result:<callID>），
// 由 node/ 域与 session 域共享：生成引用（node_tool_result）与读回（subagentSessions）使用同一常量。
const NodeResultRefPrefix = "node:"
