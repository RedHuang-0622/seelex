package workunit

// record.go — node/subagent 会话记录的**形状**（M9 从根 `store` 迁入，wire 逐字不变）。
//
// 每个子代理会话一条 JSON 记录，文件名 = `<mainSessionID>-<subSessionID>.json`，
// 存放在主会话存储目录的 `subagents/` 子目录下：
//
//	sessions-json/<project>/session-<mainID>/subagents/<mainID>-<subID>.json
//
// 因此从主会话索引（会话目录列举 / Router.List）即可直接定位全部子会话记录，
// 无需全局扫描。内容格式参考主会话 state blob（opaque JSON，含 schema 版本），
// 由调用方（backend/multiagent/subagent）写入 History / ContextSnapshot / 阶段日志 /
// 语义结果 / worktree 现场。

import (
	"errors"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

// SchemaVersion 是 Record 的当前 schema 版本。
const SchemaVersion = 1

// WorktreeRecord 是节点 worktree 现场的持久化摘要（恢复数据面）。
//
// 四栏**缺一不可**：记录投影（`WorktreeManager.Restore`）是恢复链上先到的那一份来源，
// 它缺栏就会被后来的"计划 + 账本认领"（`Adopt`）挡回（"已在册不覆盖"），于是收尾拿不到
// `MainBranch`/`BaseCommit`。写侧必须写全（`teamUnitWorktreeRecord` 从注册表现查四栏）。
type WorktreeRecord struct {
	Path       string `json:"path,omitempty"`
	Branch     string `json:"branch,omitempty"`
	MainBranch string `json:"main_branch,omitempty"`
	BaseCommit string `json:"base_commit,omitempty"`
}

// UnitRecord 是记录快照里的**身份**那一格：这条记录属于哪一类执行单元，以及这一类
// 单元独有/可选的配置摘要。恢复链据此**分派策略**（见 `backend/engine/assemble/runtime_unit_recovery.go`），
// 不再靠 nodeID 的形状去猜——记录文件与子代理节点记录共用同一个 `subagents/` 目录，
// 光看文件名分不出"崩溃遗留的子代理节点"与"团队现场的正本"。
//
// 只记身份，不重复别的事实：
//   - 会话正文在自己的字段里（`History` / `ContextJSON`）；
//   - 现场在自己的字段里（`Worktree`，四栏齐全）；
//   - **本轮派发正文就是既有的 `Goal`**（同一份事实不存两遍：`Prompt` 不另立一栏）。
//
// 词表只有一份契约（`backend/workunit.Kind`：subagent | teammate）；本字段是**存储侧的 wire 串**
// （落盘格口径：字段留在本包，读回经唯一转换点 `unitKindOf`）。**空 = subagent**：
// 加固之前写下的老记录没有这一格，读回按团队事实兜底（见 `unitKindOf`）。
type UnitRecord struct {
	Kind         string   `json:"kind,omitempty"`          // backend/workunit.Kind 的词：subagent | teammate
	Role         string   `json:"role,omitempty"`          // teammate 独有：角色名（`<role>-<itemID>` 的前半）
	Plugins      []string `json:"plugins,omitempty"`       // teammate 独有：按会话插件装配（能力轴）
	SystemPrompt string   `json:"system_prompt,omitempty"` // teammate 可选：本轮装配的系统提示（含中断恢复说明）
}

// Record 是单个子代理会话的持久化记录。
// History/ContextJSON/StagesJSON/ResultJSON 均为可独立解析的 JSON 载荷，
// 避免本包依赖 seelebridge/internal/model 或 seelexctx/snapshot。
type Record struct {
	SchemaVersion int             `json:"schema_version"`
	NodeID        string          `json:"node_id"`
	SessionID     string          `json:"session_id"` // 子会话 ID（node-<hash>）/ teammate 的角色会话 ID
	MainSessionID string          `json:"main_session_id"`
	Goal          string          `json:"goal"`
	Status        string          `json:"status"` // queued | running | done | failed
	Unit          UnitRecord      `json:"unit,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	Error         string          `json:"error,omitempty"`
	History       []types.Message `json:"history"`
	ContextJSON   []byte          `json:"context_json,omitempty"`
	StagesJSON    []byte          `json:"stages_json,omitempty"`
	ResultJSON    []byte          `json:"result_json,omitempty"`
	Worktree      WorktreeRecord  `json:"worktree,omitempty"`
	StartedAt     time.Time       `json:"started_at,omitempty"`
	EndedAt       time.Time       `json:"ended_at,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ErrNotFound 表示子代理会话记录不存在。
var ErrNotFound = errors.New("session storage: node session record not found")
