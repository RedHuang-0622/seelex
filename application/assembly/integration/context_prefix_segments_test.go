//go:build !extended

package integration

import (
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

// probeSegmentsAndMessages 把一次请求快照切成命名段与消息起点表，供
// prefixprobe 归因分叉位置。
//
// 段口径与装配顺序对齐（arch 文档 §7.4）：
//
//	system —— 常量前缀
//	truth  —— 从 transcript 重建出来的历史消息（应当逐字节冻结）
//	input  —— 当前输入（生产里承载工作打点表 / 环境表等动态尾块）
//
// 消息字节偏移由 serializeRequest 自身推导（对前 k 条消息序列化取长度），
// 因此与真正发出去的字节流始终同源，不会因格式改动而错位。
func probeSegmentsAndMessages(system string, messages []contract.EngineMessage, wire string) ([]prefixprobe.Segment, []prefixprobe.MessageRef) {
	systemLen := len(serText("SYSTEM", system))
	segments := []prefixprobe.Segment{{Name: "system", Offset: 0, Len: systemLen}}
	refs := make([]prefixprobe.MessageRef, 0, len(messages))
	for index := range messages {
		refs = append(refs, prefixprobe.MessageRef{
			Index: index,
			Role:  messages[index].Role,
			Start: probeMessageStart(system, messages, index),
		})
	}
	if len(messages) == 0 {
		if block := len(wire) - systemLen; block > 0 {
			segments = append(segments, prefixprobe.Segment{Name: "draft", Offset: systemLen, Len: block})
		}
		return segments, refs
	}

	lastStart := refs[len(refs)-1].Start
	if lastStart > systemLen {
		segments = append(segments, prefixprobe.Segment{Name: "truth", Offset: systemLen, Len: lastStart - systemLen})
	}
	if end := len(wire); end > lastStart {
		segments = append(segments, prefixprobe.Segment{Name: "input", Offset: lastStart, Len: end - lastStart})
	}
	return segments, refs
}

// probeMessageStart 返回第 index 条消息在 serializeRequest 输出里的起始字节偏移。
//
// 推导方式：serializeRequest(system, messages[:index], "", nil) 的长度即
// 「system 段 + 前 index 条消息」，再减去尾部固定的 input 占位长度。
func probeMessageStart(system string, messages []contract.EngineMessage, index int) int {
	if index <= 0 {
		return len(serText("SYSTEM", system)) + len("\n||\n")
	}
	head := serializeRequest(system, messages[:index], "", nil)
	return len(head) - probeInputTrailerLen
}
