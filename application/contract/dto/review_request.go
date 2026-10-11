// Package dto：本文件是「agent 驱动 GUI 在侧边资源管理器打开待审阅文件」这一格
// 的契约（唯一事实）。
//
// 生态位：它是**会话作用域的可重放状态**，不是一次性事件。原因写在
// ReviewRequest 的注释里（GUI 可能没开/正在重连/用户正在别的会话上）。
// 前端按它开 chip、逐枚走既有 Bridge.WorkspaceFileContent 读受控字节——
// 待审阅清单本身**只带路径**，绝不携带文件内容（不新增第二条读文件的通道，
// 也就没有一个新入口能绕过 workspace 域的 containment / 敏感过滤 / 上限）。
package dto

import "time"

// MaxReviewFilesPerRequest 是一次批处理召唤的硬上限（用户口径 2026-10-12：
// 「一次使用 = 一次批处理召唤，最多一次召唤 10 个文档」）。
//
// 它是**契约**而不是实现细节：后端按它显式拒绝超限请求（不静默截断），前端
// chip 条按它做「可见 N 枚 + 溢出计数」的宽度预算。两边必须是同一个数——
// 所以只有这一份字面量，谁都不许就地再写一个 10。
const MaxReviewFilesPerRequest = 10

// ReviewFileRef 是待审阅清单里的一条文件引用：路径 + 可选的行区间/备注。
//
// 只有路径，**没有内容**：字节仍由前端按路径走既有的
// Bridge.WorkspaceFileContent（containment / 敏感过滤 / 二进制探测 / 上限在
// workspace 域统一裁决；默认文本 4 MiB、硬钳 64 MiB）。因此「agent 叫出来的
// 文件」与人点开文件走的是同一条可见性边界，不存在"agent 那条更宽"。
type ReviewFileRef struct {
	Path string `json:"path"`
	// Note 是这次为什么请人看它（写进 chip 的悬停提示；不含文件内容）。
	Note string `json:"note,omitempty"`
	// StartLine/EndLine 是可选的关注区间（1 起、闭区间）：前端只用于滚动/高亮
	// 定位，不改变读取通道。0 = 不指定（任一端为 0 视为不指定区间）。
	StartLine int `json:"start_line,omitempty"`
	EndLine   int `json:"end_line,omitempty"`
}

// 跳过原因词表（ReviewSkip.Reason）：工具返回值、前端提示文案与文档共用这
// 一份字面量（与 PermissionGroup* 同样只住契约包）。
const (
	// ReviewSkipSandbox 是沙盒边界拒绝：越出工作区根 / 绝对路径 / 符号链接 /
	// 忽略目录（.git、node_modules、.seelex 等）/ 非常规文件（目录、设备…）。
	ReviewSkipSandbox = "sandbox"
	// ReviewSkipSensitive 是敏感文件名拒绝（accounts.yaml、*.local.yaml 等）。
	ReviewSkipSensitive = "sensitive"
	// ReviewSkipBinary 是二进制探测未通过（FileContent.TextLike=false）。
	ReviewSkipBinary = "binary"
	// ReviewSkipMissing 是目标不存在或不可读（inspect/open 失败）。
	ReviewSkipMissing = "missing"
	// ReviewSkipDuplicate 是同一次召唤里的重复路径（只保留第一份）。
	ReviewSkipDuplicate = "duplicate"
)

// ReviewSkip 是一份**未被纳入**待审阅清单的文件与原因。
//
// 它存在的理由只有一个：工具返回值必须回契约。模型据此在后续回合里知道
// "哪几份没进去、为什么"，人也能看见同一句话——而不是"叫了 10 份、弹了 7 份、
// 剩下 3 份无声消失"。
type ReviewSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ReviewRequest 是 agent 发起的一次「请 GUI 在侧边资源管理器打开这些文件供人
// 审阅」的**批处理请求**。
//
// 为什么是状态而不是事件：GUI 可能没开、可能正在重连，或者用户此刻正在别的
// 会话上。一次性事件在"当下没有接收者"时就等于丢失；落在这份**会话作用域**的
// 快照里，前端起来/切回该会话时按快照补放即可。它因此满足两条口径：
//
//   - 可重放：快照即真相源，不依赖送达时序；
//   - 不抢视线：它是详情抽屉的请求（人可忽略、可关掉），不是模态框、不是置顶窗。
//
// 失效语义对齐既有口径（docs/gui/modules/right-sidebar.md）：
//   - 会话切换 → 各会话各持一份（切到别的会话看到的自然是那一份，通常为空）；
//   - 工作区切换 → 连带清空（清单里的路径是工作区相对路径，换根之后不再指向
//     "同一份文件"）；
//   - 不落盘：这是"现在请你看一下"的请求，不是会话事实，冷加载/重启后为空。
type ReviewRequest struct {
	// RequestID 是这一次召唤的身份：前端据此判定"这是新的一次请求"还是
	// "同一份快照的补放"——后者不重复开 tab、不抢当前显示的文件。
	RequestID string `json:"request_id"`
	// Files 是通过沙盒/敏感/二进制裁决的待审阅清单（1..MaxReviewFilesPerRequest）。
	Files []ReviewFileRef `json:"files"`
	// Skipped 是这一次被跳过的文件与原因（回契约的那一半）。
	Skipped []ReviewSkip `json:"skipped,omitempty"`
	// RequestedAt 是这次召唤的时刻。
	RequestedAt time.Time `json:"requested_at,omitempty"`
}

// CloneReviewRequest 深拷贝一份待审阅请求（快照是并发读者的共享值，浅拷贝会
// 让两个读者共享同一张 Files/Skipped 底层数组）。
func CloneReviewRequest(request *ReviewRequest) *ReviewRequest {
	if request == nil {
		return nil
	}
	cloned := *request
	cloned.Files = append([]ReviewFileRef(nil), request.Files...)
	cloned.Skipped = append([]ReviewSkip(nil), request.Skipped...)
	return &cloned
}
