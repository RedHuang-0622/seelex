// Package plan 承载 Plan 执行域：DSL 节点负载类型、规范化/校验、
// 事件投影、重规划护栏与执行组件。依赖方向为根 facade → plan；
// plan 不反向依赖 seelebridge 根包（共享账号/节点类型经 internal/model 或本包导出）。
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
)

// SeelexNodeInput 是产品 DSL 的节点负载（plan.md §3.3）。
// plan_load 规范 JSON 中每个节点形如 {"id","input","kind","budget"}。
type SeelexNodeInput struct {
	ID    string `json:"id"`
	Input string `json:"input"`
	Kind  string `json:"kind,omitempty"` // agent | auto | function | approve | verify | deliver
	// Budget 是节点级执行预算（plan.md §7.3）：可选覆盖 limits 默认值；
	// 缺省（nil/0）回退 seele.yaml limits，上限由 PlanPolicy 校验。
	Budget *NodeBudgetInput `json:"budget,omitempty"`
	// TaskID 是装配件模式绑定的现成 task（B6）：只绑 task_id，不注入 task
	// 内容到 prompt（保持子代理 prompt 格式纯净）；空 = 子代理自行开 task。
	TaskID string `json:"task_id,omitempty"`
}

// NodeBudgetInput 是节点子代理的预算参数（JSON 契约：max_loops /
// max_output_tokens 均可选，0 = 缺省回退 limits）。
type NodeBudgetInput struct {
	MaxLoops        int `json:"max_loops,omitempty"`
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
}

// canonicalPlanDocument 把规范化 plan_load JSON
// （{entry, nodes:{id:{input,kind}}, edges:{from:[targets]}}）转换为
// codec.Document[SeelexNodeInput]，供 codec.Import 严格校验 + Seal。
// 节点与边排序保证文档确定性（与 AdjacencyToEdges 的稳定顺序语义一致）。
func CanonicalPlanDocument(canonical string) (codec.Document[SeelexNodeInput], error) {
	var input PlanLoadSpec
	if err := json.Unmarshal([]byte(canonical), &input); err != nil {
		return codec.Document[SeelexNodeInput]{}, fmt.Errorf("plan_load: parse canonical plan: %w", err)
	}
	document := codec.Document[SeelexNodeInput]{Version: codec.Version, Entry: input.Entry}
	ids := make([]string, 0, len(input.Nodes))
	for id := range input.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var payload struct {
			Input  string           `json:"input"`
			Kind   string           `json:"kind,omitempty"`
			Budget *NodeBudgetInput `json:"budget,omitempty"`
		}
		if err := json.Unmarshal(input.Nodes[id], &payload); err != nil {
			return codec.Document[SeelexNodeInput]{}, fmt.Errorf("plan_load: node %q: %w", id, err)
		}
		document.Nodes = append(document.Nodes, codec.NodeSpec[SeelexNodeInput]{
			ID:    id,
			Kind:  payload.Kind,
			Input: SeelexNodeInput{ID: id, Input: payload.Input, Kind: payload.Kind, Budget: payload.Budget},
		})
	}
	edgeKeys := make([]string, 0, len(input.Edges))
	for from := range input.Edges {
		edgeKeys = append(edgeKeys, from)
	}
	sort.Strings(edgeKeys)
	for _, from := range edgeKeys {
		targets := append([]string(nil), input.Edges[from]...)
		sort.Strings(targets)
		for _, to := range targets {
			document.Edges = append(document.Edges, codec.EdgeSpec{From: from, To: to})
		}
	}
	return document, nil
}

// ── 节点实现 ─────────────────────────────────────────────────────────

// dslKindNodeKind 把 DSL kind 映射为 node.NodeKind 展示值（runner 结果 kind）。
func dslKindNodeKind(kind string) node.NodeKind {
	switch kind {
	case "auto", "agent":
		return node.KindAuto
	case "approve", "manual":
		return node.KindApprove
	default: // function / verify / deliver
		return node.KindMethod
	}
}

// productNode 是 auto/function/verify/deliver 的确定性执行节点：
// Run 直接返回节点自身的 input（不依赖 LLM 或前序输出解码），
// DAG 执行因此可离线复现；agent/approve 之外的 kind 在 slice 4 全部走此路径。
type productNode struct {
	node.BaseNode
	input SeelexNodeInput
}

func newProductNode(spec codec.NodeSpec[SeelexNodeInput], kind string) *productNode {
	return &productNode{
		BaseNode: node.NewBaseNode(spec.ID, dslKindNodeKind(kind)),
		input:    spec.Input,
	}
}

func (n *productNode) Run(_ context.Context, _ *types.WorkflowContext) (string, error) {
	return n.input.Input, nil
}

// approvalGateNode 是 kind:approve/manual 的审批门控节点：
// 经 SetPlanApprovalGate 注入的 approve.ApprovalGate 阻塞等待用户决策。
// "skip" → 返回输入继续 DAG；"abort" → 终止；其余决策 → 返回输入
// （slice 4 确定性执行；slice 5 起决策后可转入节点 agent 执行）。
type approvalGateNode struct {
	node.BaseNode
	input SeelexNodeInput
	gate  func() approve.ApprovalGate
}

func newApprovalGateNode(spec codec.NodeSpec[SeelexNodeInput], gate func() approve.ApprovalGate) *approvalGateNode {
	return &approvalGateNode{
		BaseNode: node.NewBaseNode(spec.ID, node.KindApprove),
		input:    spec.Input,
		gate:     gate,
	}
}

func (n *approvalGateNode) Run(ctx context.Context, _ *types.WorkflowContext) (string, error) {
	gate := n.gate()
	if gate == nil {
		return "", fmt.Errorf("approve node %q: no approval gate is configured", n.ID())
	}
	decision, err := gate.Ask(ctx, approve.Question{
		ID:      n.ID(),
		Content: n.input.Input,
		Options: approve.Choices("execute", "skip", "abort"),
	})
	if err != nil {
		return "", err
	}
	choice, _ := decision.(string)
	switch choice {
	case "skip":
		return n.input.Input, nil
	case "abort":
		return "", fmt.Errorf("aborted at approve node %q", n.ID())
	default:
		return n.input.Input, nil
	}
}

// NewProductNode 构造确定性执行节点（auto/function/verify/deliver；根包 buildNode 使用）。
func NewProductNode(spec codec.NodeSpec[SeelexNodeInput], kind string) node.Node {
	return newProductNode(spec, kind)
}

// NewApprovalGateNode 构造审批门控节点（approve/manual；根包 buildNode 使用）。
func NewApprovalGateNode(spec codec.NodeSpec[SeelexNodeInput], gate func() approve.ApprovalGate) node.Node {
	return newApprovalGateNode(spec, gate)
}
