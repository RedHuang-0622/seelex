package dto

// 思考强度（reasoning effort）是跨厂商的"想多深"旋钮：OpenAI / DeepSeek 用顶层
// `reasoning_effort`，Anthropic 用顶层 `output_config.effort`。
//
// **注意词表归属**：`low`/`medium`/`high`/`max` 是 **provider 的词表**；
// `lite`/`medium`/`high`/`max` 是 **Seelex 自己的 effort 档位名**。两者不是同一个
// 东西——"档位 → wire 值"的唯一映射在 application/prompt 的 effortProfiles 里。
//
// 放在 dto 的原因：这是**跨层契约**。seelebridge（账号装配与配置加载）与
// application（effort 策略）都要用它，而 application 依赖 seelebridge，反向引用
// 成环；dto 是两边都引的叶子包（同 AccountRole / PlanPolicy 的处理）。
const (
	// ReasoningEffortMinimal 只在部分模型上可用（如 GPT-5 系）。登记它是为了让
	// 配置校验不至于把合法值判错，并不代表所有 provider 都认。
	ReasoningEffortMinimal = "minimal"
	ReasoningEffortLow     = "low"
	ReasoningEffortMedium  = "medium"
	ReasoningEffortHigh    = "high"
	ReasoningEffortMax     = "max"
)

// ReasoningEffortSession 表示「跟随会话 effort 档位」：账号自己不钉死思考强度，
// 由当前会话的档位在运行时决定。
//
// 空串是同一个意思的另一条入口（都没钉死）；留着这个名字是为了让 accounts.yaml
// 能写出可读的意图，而不是靠一个空字段去猜。
const ReasoningEffortSession = "session"

// reasoningEffortVocabulary 是 provider 词表（校验用；"session" 单独判）。
var reasoningEffortVocabulary = []string{
	ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium,
	ReasoningEffortHigh, ReasoningEffortMax,
}

// ValidReasoningEffort 判断一个配置值是否为本系统认识的思考强度值。
// 认识 "session" 与 provider 词表；空值不由这里判——调用方总是先补上默认值，
// 空值不该走到这里。
func ValidReasoningEffort(value string) bool {
	if value == ReasoningEffortSession {
		return true
	}
	for _, level := range reasoningEffortVocabulary {
		if value == level {
			return true
		}
	}
	return false
}

// ReasoningEffortVocabulary 返回全部合法取值（"session" 在前），供配置报错提示用。
func ReasoningEffortVocabulary() []string {
	out := make([]string, 0, len(reasoningEffortVocabulary)+1)
	out = append(out, ReasoningEffortSession)
	return append(out, reasoningEffortVocabulary...)
}

// 角色默认思考强度。
//
// effort 同时携带"思考强度"与"loop 次数/预算"两样东西；这里**只定前者**。
// loop 次数与预算由 effort 档位在 application/prompt 里单独持有，两者互不影响
// ——调思考强度不会动 loop 次数，loop 的既有设定一个字都不改。
//
//   - agent：跟随会话档位（默认档 high）。主会话"想多深"应该就等于用户选的那一档，
//     否则界面上选的档位只在提示词与 loop 上生效、在思考强度上无声失效。
//   - subagent：low。子代理做的是被指派的窄任务，顶格思考既慢又贵。
//   - goalplan：high。规划属于"难且值得想"的一类，但**不取 max**：max 是给长周期
//     （>30 分钟）、大预算任务的档，而规划本身是单次短调用，high 通常已是拐点
//     （DeepSeek 上 max 会把输出上限放到 128K，成本是实的）。
const (
	DefaultSubAgentReasoningEffort = ReasoningEffortLow
	DefaultGoalPlanReasoningEffort = ReasoningEffortHigh
)

var defaultReasoningEffortByRole = map[AccountRole]string{
	RoleAgent:    ReasoningEffortSession,
	RoleSubAgent: DefaultSubAgentReasoningEffort,
	RoleGoalPlan: DefaultGoalPlanReasoningEffort,
}

// DefaultReasoningEffortForRole 返回角色的默认思考强度（可能是 "session"）。
// 未登记的角色一律按"跟随会话档位"处理：不替它钉死强度是最保守的选择。
func DefaultReasoningEffortForRole(role AccountRole) string {
	if effort, ok := defaultReasoningEffortByRole[role]; ok {
		return effort
	}
	return ReasoningEffortSession
}
