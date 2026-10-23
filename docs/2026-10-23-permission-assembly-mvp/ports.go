// Package main 是「权限装配：用户组 × 权限类型」的**孤立可行性 MVP**。
//
// 它不属于产品链路：不注册工具、不改判定、不碰审批面。它只回答三个问题——
//
//  1. 矩阵能否表达任意正交子集（含"只给 ssh"、"只给 write+ssh"）？
//  2. 一次调用需要**一组**权限这条规则，能否覆盖本地/远端/越界三种面？
//  3. 老档位（manual/edit/auto/full）能否翻译成预设而不放宽判据？
//
// 设计依据：docs/arch/permission-assembly.md
package main

// ports.go —— **契约草案**：这段将来原样搬进 `contract`，让 registry 只依赖端口，
// 不依赖 backend/limit/policy 的具体实现（避免依赖倒置）。
//
// 词表仍是"路由组名 + ssh 族的两个细分"，**不新造平行词表**：组名的唯一事实在
// contract/dto 与 backend/limit/policy 的 DefaultPermissionGroupList。

import "context"

// PermType 是权限类型（矩阵的列）。
type PermType string

const (
	// 既有 6 个路由组，语义不变（改不改共享状态）。
	PermRO        PermType = "ro"
	PermRW        PermType = "rw"
	PermRWSession PermType = "rw_session"
	PermRWDesktop PermType = "rw_desktop"
	PermCTL       PermType = "ctl"
	PermADM       PermType = "adm"

	// 新增：允许把工具调用打到远端执行面。
	PermSSH PermType = "ssh"
	// 新增：允许远端访问项目根之外的用户空间（~ / 绝对路径）。
	PermSSHUserSpace PermType = "ssh.userspace"
)

// GroupID 是用户组（矩阵的行）：既有 4 个主体 + 自定义组。
type GroupID string

const (
	GroupRoot  GroupID = "root"
	GroupSub   GroupID = "sub"
	GroupEmpRO GroupID = "emp_ro"
	GroupEmpRW GroupID = "emp_rw"
)

// Effect 是授予效果。
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectAsk   Effect = "ask"
	EffectDeny  Effect = "deny"
)

// rank 用于单调性断言（deny < ask < allow）。
func (e Effect) rank() int {
	switch e {
	case EffectAllow:
		return 2
	case EffectAsk:
		return 1
	default:
		return 0
	}
}

// Grant 是矩阵里的一格。
type Grant struct {
	Effect Effect `json:"effect"`
	// Bits 是 rwx 位；0 = 用该类型的既有 Mode（不另填一份需求位）。
	Bits uint8 `json:"bits,omitempty"`
}

// Assembly 是一份权限装配（矩阵）。**缺格 = deny**（默认拒绝）。
type Assembly struct {
	Version int                            `json:"version"`
	Cells   map[GroupID]map[PermType]Grant `json:"cells"`
}

// AssemblyPort 是读面端口（消费方定义，实现方满足）。
type AssemblyPort interface {
	AssemblyFor(group GroupID) (Assembly, error)
}

// Requirement 是一次调用对权限的**结构化需求**。
//
// 刻意不让上层拼 []PermType：哪个工具在哪个执行面需要哪几格，只能有一份判据
// （放在实现层）；否则五个工具各自的 handler 都要知道权限词表，那正是词表散落
// 各处的起点。
type Requirement struct {
	Tool string
	// Remote 表示走远端执行面（需要 ssh 权限）。
	Remote bool
	// OutOfRoot 表示远端路径越出项目根（需要 ssh.userspace）。
	// 判据由 pathGate 的同一份 containment 算出，本层不重复解析路径。
	OutOfRoot bool
	Path      string
}

// Decision 是判定结果。
type Decision struct {
	Effect   Effect
	Required []PermType
	Missing  []PermType
	Warnings []string
	Reason   string
}

// Gate 是判定端口。
type Gate interface {
	Decide(ctx context.Context, group GroupID, req Requirement) (Decision, error)
}
