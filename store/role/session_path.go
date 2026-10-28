package role

// session_path.go — 角色会话子树的物理路径与提交身份推导。
//
// M9 从根 `store/role_session.go` 迁入。这些是**纯函数**：只由 (mainRoot,
// roleName, roleSessionID) 决定，不读盘、不持锁、不碰引擎。迁出去让根包只剩
// 单层转发，领域包自己拥有"角色会话落在哪、提交身份叫什么"的判据。
//
// 零数据迁移：目录名与提交身份字符串必须逐字保持——它们已经写在用户磁盘上，
// 任何"改进"都会让旧角色会话变成孤儿目录。

import (
	"encoding/json"
	"path/filepath"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

// 角色名保留字：main 复用主会话本身，tl 使用 goal_ 前缀，其它角色使用 role_ 前缀。
const (
	RoleUser = "user"
	RoleMain = "main"
	RoleTL   = "tl"
)

// draftDirName 是角色 draft WAL 的子目录名。
const draftDirName = "draft"

// SessionRoot 返回角色会话子树的物理根。main 直接使用主会话根；tl 使用
// goal_<hash>/；其它未来 agent-team 角色使用 role_<hash>/。
func SessionRoot(mainRoot, roleName, roleSessionID string) string {
	if roleName == RoleMain {
		return mainRoot
	}
	prefix := "role_"
	if roleName == RoleTL {
		prefix = "goal_"
	}
	return filepath.Join(mainRoot, prefix+storeroot.Hash(roleSessionID))
}

// SessionCommitID 是角色会话创建时的提交身份。
func SessionCommitID(roleName, roleSessionID string) string {
	return "role-session-" + roleName + "-" + storeroot.Hash(roleSessionID)
}

// BackupCommitID 是角色备份 append 的提交身份。
func BackupCommitID(roleName, roleSessionID string, rows []protocol.TranscriptEvent) string {
	return "role-backup-" + roleName + "-" + storeroot.Hash(roleSessionID+"|"+storesession.MessageRowsCommitID(rows))
}

// JoinPayload 是 role_session 结构化事件的负载（字段名逐字保持）。
//
// map 字面量序列化失败只可能来自不可序列化的值类型；四个入参都是 string/uint64，
// 因此错误恒为 nil，与迁移前的 `payload, _ :=` 一致。
func JoinPayload(mainSessionID, roleName, roleSessionID string, joinSeq uint64) []byte {
	payload, _ := json.Marshal(map[string]any{
		"main_session_id": mainSessionID,
		"role_name":       roleName,
		"role_session_id": roleSessionID,
		"join_seq_id":     joinSeq,
	})
	return payload
}
