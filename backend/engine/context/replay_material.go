// replay_material.go — 重放素材的选择（装配层这一侧）。
//
// 一句话：重放素材是「上一次真实请求的历史字节」，靠它命中 provider 的前缀缓存；
// 但这份字节**不一定存在**，也不一定**含对话正文**。两种缺失都要回退到本次装配
// 从 transcript 投影出来的那份历史（fullContext = "这次不压缩就会发出去的对话"）。
package context_runtime

import (
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
)

// replayMaterialFor 选这次压缩喂给重放的那份素材。
//
// 首选 existing——上一次真实请求的引擎历史字节（前缀重放靠它命中 provider 的前缀
// 缓存）。但以下两种情形改用 fullContext：
//
//  1. existing 为空：没有在飞请求（冷加载 / 刚清空的会话还没物化引擎历史），而这类
//     会话的压缩恰恰最常见（/compact、会话级维护身份、冷加载后第一条消息触发的自动
//     压缩）。否则每一帧都落在 no-replay-material 上（2026-09-29 现场）。
//  2. existing **非空但没有对话正文**（只剩 system 前缀）：本次要压的对话正文在
//     transcript 里躺着，素材里却一个字都没有。
//
// 第 2 种是 2026-10-12 现场：`if len(replayMaterial) == 0` 是**形状**判据，判的是
// "有没有东西"而不是"有没有可压的对话"。现场形态 messages_before=1——引擎历史非空、
// 却只剩上一次压缩折出保留窗口后的 system 行，于是回退不触发：模型被叫到了、只收到
// 「system 行 + 固定压缩指令」，回一句「本次请求未附带任何待压缩的对话正文」的元回答，
// 而链上的成功判据只问「回复非空」——这句元回答就被记成一次成功的模型读后感
// （summary_source=replay），落进帧正文。
//
// 两处调用（读数闸与推帧）共用这一个实现：同日二次口径要求"两处各自 existing→
// fullContext 的同款回退"不许各写一份（一处修好而另一处漏改，帧素材与读数素材就会
// 分叉）。判据与 seelexctx 的 chapter2Node 同口径——那里同样以"素材里有没有对话正文"
// 决定发不发重放请求。
func replayMaterialFor(existing, fullContext []contract.EngineMessage) []contract.EngineMessage {
	if hasConversationBody(existing) {
		return existing
	}
	return fullContext
}

// hasConversationBody 报告素材里有没有**对话正文**：非 system 角色、且正文非空或带
// 工具调用的消息。只剩 system 前缀的素材不满足——它是请求的固定开销，不是要压的对话。
func hasConversationBody(messages []contract.EngineMessage) bool {
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "system") {
			continue
		}
		if strings.TrimSpace(message.Content) != "" || len(message.ToolCalls) > 0 {
			return true
		}
	}
	return false
}
