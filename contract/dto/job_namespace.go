package dto

import (
	"strconv"
	"strings"
)

// 作业句柄的**命名空间**（唯一一份定义）：句柄必须自证来源。
//
// 为什么需要它：本进程里**并存两张作业表**——seelebridge/tools 的后台作业表（`bash_bg` /
// `read_batch` / `fork_subagents`）与 Seele `jobs.Manager` 的团队作业表（teammate，取回走
// `jobs_manage`）。两张表各自维护自己的派发序号；句柄形状如果相同，同一个会话里就会同时
// 存在两条同名的句柄，于是**把 A 表的句柄交给 B 表的管理工具会静默命中一条无关作业**
// （2026-10-15 审计 D1 的实测：leader 想停 teammate 的 `a5`，`bg_manage(op=done, handle=a5)`
// 销掉的是一条无关的后台作业，返回还是"成功"）。
//
// 因此句柄文法固定为 `<token>-<seq>`，token 与表的出处一一对应；跨表调用由此**语法上
// 不可能命中**。判据、铸造、识别都只在本文件一处（AGENTS.md §8「一份判据 = 一处函数」）。
type JobNamespace string

const (
	// JobNamespaceToolJob = 后台**工具**作业：`bash_bg`（process）/ `read_batch`（inline）。
	JobNamespaceToolJob JobNamespace = "bt"
	// JobNamespaceSubagent = 后台**子代理**作业：`fork_subagents`（subagent）。
	JobNamespaceSubagent JobNamespace = "bs"
	// JobNamespaceTeamwork = **团队**作业：teammate（Seele `jobs.Manager`；命名由 Seelex 侧
	// 注入的 Namer 给出，见 seelebridge 的团队作业命名器）。
	JobNamespaceTeamwork JobNamespace = "bteam"
)

// jobNamespaceSeparator 是 token 与序号之间的分隔符。它是**文法的一部分**，不是装饰：
// `bt` 是 `bteam` 的前缀，只有把分隔符一起匹配，`bteam-3` 才不会被判成 `bt-` 那一族。
const jobNamespaceSeparator = "-"

// JobNamespaces 列出全部命名空间：识别、铸造与门禁用例都从这一处取，不再各写一份字面量。
var JobNamespaces = [...]JobNamespace{
	JobNamespaceToolJob,
	JobNamespaceSubagent,
	JobNamespaceTeamwork,
}

// jobNamespacePrefix 是某个命名空间的**前缀**（token + 分隔符）。识别与解析都走它，
// 于是"分隔符是文法的一部分"这条规则只有一处实现。
func jobNamespacePrefix(namespace JobNamespace) string {
	return string(namespace) + jobNamespaceSeparator
}

// JobHandle 铸造一个句柄。它是**唯一一处**把 token 与序号拼起来的地方：两张作业表的铸造点
// （seelebridge/tools 的后台作业表、Seele `jobs.Manager` 经注入 Namer）都调它。
func JobHandle(namespace JobNamespace, seq int) string {
	return jobNamespacePrefix(namespace) + strconv.Itoa(seq)
}

// JobNamespaceOf 识别一个句柄属于哪个命名空间；第二个返回值报告它是不是**我们认得的形态**
// （未知形态 = 老句柄 / 外来字符串，调用方据此给出"这不是本表的句柄"这类显式错误，而不是
// 把它当成"可能是我表里的一条"）。
func JobNamespaceOf(handle string) (JobNamespace, bool) {
	for _, namespace := range JobNamespaces {
		if strings.HasPrefix(handle, jobNamespacePrefix(namespace)) {
			return namespace, true
		}
	}
	return "", false
}

// JobHandleSeq 从句柄里取回**派发序号**（第二个返回值 = 这个形态认不认得）。
//
// 投影要按派发序稳定排，而句柄文本的字典序不成立（`bt-10` 排在 `bt-2` 前）；同时排序
// 不许多写一份形状知识——所以解析只在这里，调用方拿到的是序号本身。
func JobHandleSeq(handle string) (int, bool) {
	namespace, known := JobNamespaceOf(handle)
	if !known {
		return 0, false
	}
	seq, err := strconv.Atoi(strings.TrimPrefix(handle, jobNamespacePrefix(namespace)))
	if err != nil {
		return 0, false
	}
	return seq, true
}
