// message.go — 可见对话消息的跨层形状（Message / MessageOrigin / 群聊角色名）。
//
// 为什么从 state.go 拆出来：快照契约（Snapshot/SessionState/...）与「一条可见消息
// 长什么样」是两件事，后者的字段（尤其用户消息随行的图片）会随交互能力继续长。
// 拆开之后两边各自守自己的预算（contract 的 TestBM001FileSizeBudget 是 frozen
// 预算：超了只能拆，不能抬）。

package model

import (
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

type Message struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
	// EventSeq 是这条可见消息对应的持久事件行序号（0 = 旧数据/消息不在完整
	// 段落中）。编辑与 fork 只认这个后端事实，不按前端窗口位置猜。
	EventSeq uint64 `json:"event_seq,omitempty"`
	// Editable / EditDisabledReason 由应用层按父会话运行态、段落完整性和
	// 上下文压缩保留窗口统一生成。前端只消费，不自行推导。
	Editable           bool   `json:"editable,omitempty"`
	EditDisabledReason string `json:"edit_disabled_reason,omitempty"`
	// Kind 是消息在多线谱中的显式类别（llm/tool_call/tool_output/user_input/…）。
	// 空串 = 旧数据未标注，前端按 Role/Tool 回退分类。
	Kind string `json:"kind,omitempty"`
	// Images 是这条**用户消息随行附上的图片**（输入框拖拽 / 粘贴进来的图）。
	//
	// 为什么挂在消息上而不是只留在输入框：用户发出去的是"这条话 + 这张图"，图
	// 一旦不在消息里，对话区就只能显示半句话（图片上传了、模型也看了，但用户自己
	// 永远看不到自己发了什么）。字段只带**引用 + 展示元数据**（ref/name/mime/尺寸），
	// 字节不进快照——渲染层按 ref 经 Bridge 读回缩略图，点开才由宿主打开原图
	// （与 `dto.ComposerImage` 同一形状，前端与桥面共用一份定义）。
	// 只有 role=user 的行会带；其余角色为空。
	Images []dto.ComposerImage `json:"images,omitempty"`
	// ReasoningContent 是模型推理内容（thinking）。可见消息里与 Content 分离：
	// 聊天区只做一行带过，轨迹区完整查看；不做 HTML 注入，前端按纯文本渲染。
	ReasoningContent string    `json:"reasoning_content,omitempty"`
	Tool             *ToolCall `json:"tool,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	// RoleName / RoleSessionID / RoundID / UnitSeq 是群聊角色归属（my_design
	// §8.3）：前端按"主持该轮次的 agent"渲染过程。空 = 单 agent 会话的旧数据。
	RoleName      string `json:"role_name,omitempty"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	RoundID       uint64 `json:"round_id,omitempty"`
	UnitSeq       uint64 `json:"unit_seq,omitempty"`
	// ConfigRole / ConfigModel / ConfigProvider 是这条 assistant 回应的实际
	// 承接路由（请求解析中间件在账号池租约路径上得出，随 response 回填）：
	// config role = 承接账号所属的配置角色（agent/subagent/goalplan），
	// config model / provider = 承接账号的模型与厂商类型。进真相源（归属审计
	// + 模型能力追溯），旧数据/非 assistant 行为空，前端缺省不渲染。
	ConfigRole     string `json:"config_role,omitempty"`
	ConfigModel    string `json:"config_model,omitempty"`
	ConfigProvider string `json:"config_provider,omitempty"`
}

// 群聊逻辑角色名（role_name）：只做 metadata 与展示归属，provider role 仍只有
// system/user/assistant/tool（docs/arch/a2a-agent-team-factory.md §2.1）。
const (
	RoleNameUser = "user"
	RoleNameMain = "main"
	RoleNameTL   = "tl"
)

// MessageOrigin 是可见消息的群聊归属（my_design §8.3）：谁说的（逻辑角色名）、
// 属于哪个角色会话、哪一轮哪一单元。零值 = 单 agent 会话的旧数据，前端
// 按 provider role 文案回退（AGENT/SYSTEM），因此生产者只在有归属时填写。
type MessageOrigin struct {
	RoleName      string
	RoleSessionID string
	RoundID       uint64
	UnitSeq       uint64
	// EventSeq 是该消息对应的 transcript 事件序号（用户消息编辑/fork 定位用）。
	EventSeq uint64
	// Kind 是这条可见消息的多线谱类别（同 Message.Kind）。生产者只在"这条行是
	// 什么"不能由 Role 推断时才填（例如 b→a 的裁决行 tl_directive）——轨迹区/
	// 探针据此按类别取行，不靠正文措辞猜。空 = 按 Role/Tool 回退分类。
	Kind string
	// Images 是这条用户行随行的图片附件（同 Message.Images）。它跟着行为一起
	// 进可见消息：可见消息是本层的投影，图片是这行的内容之一，不是归属之外的
	// 旁路数据——放在这里，两个 append 点（起始 / 队列晋升）都只用照抄一次，
	// 不会出现"实时行没图、冷恢复有图"的两套观感。
	Images []dto.ComposerImage
}

// MessageOriginOf 把一条 transcript 事件行映射成可见消息的归属（含随行图片）。
//
// 为什么只有这一处：可见用户行有两个 append 点（回合起始、队列晋升），两处都必须
// 从**同一条事件行**推出同一份归属。映射各写一遍的话，每加一个字段（比如这次的
// 用户图片）就必然会漏掉其中一边——"实时行有图、晋升后没图"正是这么长出来的。
func MessageOriginOf(event TranscriptEvent) MessageOrigin {
	return MessageOrigin{
		RoleName: event.RoleName, RoleSessionID: event.RoleSessionID,
		RoundID: event.RoundID, UnitSeq: event.UnitSeq, EventSeq: event.Seq,
		Images: event.Images,
	}
}
