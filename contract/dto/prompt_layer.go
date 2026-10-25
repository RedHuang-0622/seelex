package dto

// PromptLayer 是一层 system prompt 或活动 Skill 的跨层只读形状。
//
// 类型放在契约层，使 backend 任务域可以表达该输入面，而不反向依赖
// application/prompt；application/prompt 以同身份 alias 继续承载宿主侧栈行为。
type PromptLayer struct {
	Kind string // "identity" | "base" | "effort" | "instructions" | "skill"
	Name string // plugin name / effort level / skill name
	Text string // prompt 内容
}
