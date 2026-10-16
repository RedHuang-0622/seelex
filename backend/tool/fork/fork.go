// Package fork 承载 fork_subagents 的纯类型与 DAG 构造：输入契约、
// 规范 JSON 与 summary 节点实现。执行编排（账号/任务绑定/结果复用）留在
// 根包 Runtime 门面（fork_tool.go）；本包不反向依赖 seelebridge 根包。
package fork
