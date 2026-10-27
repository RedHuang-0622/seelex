package context

import (
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const (
	WireRoleSystem    = "system"
	WireRoleUser      = "user"
	WireRoleAssistant = "assistant"
	WireRoleTool      = "tool"
	DefaultWireBudget = 200_000
)

type WireMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []WireToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	Name             string         `json:"name,omitempty"`
	ResultRef        string         `json:"result_ref,omitempty"`
	Seq              uint64         `json:"seq,omitempty"`
	Internal         bool           `json:"internal,omitempty"`
	Repair           bool           `json:"repair,omitempty"`
	Attempt          bool           `json:"attempt,omitempty"`
}

type WireToolCall = protocol.TranscriptToolCall

type WireParams struct {
	K      int
	Budget int
	Repair bool
}

type WireResult struct {
	Messages     []WireMessage `json:"messages"`
	NeedCompact  bool          `json:"need_compact"`
	PrefixDigest string        `json:"prefix_digest"`
	TailStartSeq uint64        `json:"tail_start_seq"`
	FrameApplied bool          `json:"frame_applied,omitempty"`
	Open         bool          `json:"open,omitempty"`
}

func NormalizeWireParams(params WireParams) WireParams {
	if params.K <= 0 {
		params.K = 3
	}
	if params.Budget <= 0 {
		params.Budget = DefaultWireBudget
	}
	params.Repair = true
	return params
}

func ApplyWireBudget(settings storeroot.Settings, base WireParams) WireParams {
	budget, soft, _ := settings.WireBudget()
	if base.Budget <= 0 {
		base.Budget = soft
		return base
	}
	if budget > 0 {
		base.Budget = int(float64(base.Budget) * float64(soft) / float64(budget))
	}
	return base
}
