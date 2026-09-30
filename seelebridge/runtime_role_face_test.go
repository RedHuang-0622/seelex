package seelebridge

// runtime_role_face_test.go — 钉住 D6：teammate（员工 / ADVISOR 评审者）角色会话的
// 工具面**硬移除** fork_subagents。
//
// 为什么单独钉这条：fork_subagents 在权限面上是 control 类，对**非 root 主体**本来
// 就被 VisibleTools 隐去；但那条隐去依赖"主体解析正确命中 emp_<角色>"。一旦角色会话
// 归属索引冷启动/未命中，主体可能落回 root，control 隐藏就会失效——teammate 于是能
// 嵌套分叉子代理，绕过人数上限并派生孙 worktree。硬移除是**不依赖主体解析**的第二
// 道闸：装配进角色会话的工具面里根本不存在它（连开关都没有），派发口也直接拒绝。
//
// 它测的是收窄层本体（teammateToolFace）+ 装配点（newRoleEngine 确实用了它），不是
// 整轮：注入式假引擎会绕过 newRoleEngine 的工具面装配。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
)

// faceProbeAgent 是钉工具面的最小假 Agent：给定可见工具清单，并记录派发。
type faceProbeAgent struct {
	visible  []string
	dispatch []string
}

func (a *faceProbeAgent) LLM() types.ChatCompleter { return nil }

func (a *faceProbeAgent) VisibleTools(context.Context) []types.Tool {
	tools := make([]types.Tool, 0, len(a.visible))
	for _, name := range a.visible {
		tools = append(tools, types.Tool{Type: "function", Function: types.ToolFunction{Name: name}})
	}
	return tools
}

func (a *faceProbeAgent) Dispatch(_ context.Context, name, _ string) (string, error) {
	a.dispatch = append(a.dispatch, name)
	return "ok:" + name, nil
}

func visibleNamesOf(agent session.Agent, ctx context.Context) []string {
	tools := agent.VisibleTools(ctx)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func TestTeammateToolFaceHidesForkSubagents(t *testing.T) {
	inner := &faceProbeAgent{visible: []string{"read_file", "write_file", "fork_subagents"}}
	face := teammateToolFace(inner)
	if face == nil {
		t.Fatal("teammate 工具面不能为 nil（inner 非 nil 时）")
	}
	names := visibleNamesOf(inner, context.Background()) // 内层原样
	if len(names) != 3 {
		t.Fatalf("内层可见工具应保持不变，得 %v", names)
	}
	got := visibleNamesOf(face, context.Background())
	for _, name := range got {
		if name == "fork_subagents" {
			t.Fatalf("teammate 工具面不得出现 fork_subagents（D6 硬移除），得 %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("除 fork_subagents 外的工具应保留，得 %v", got)
	}
}

func TestTeammateToolFaceRefusesForkDispatch(t *testing.T) {
	inner := &faceProbeAgent{visible: []string{"read_file", "fork_subagents"}}
	face := teammateToolFace(inner).(*teammateAgent)
	if _, err := face.Dispatch(context.Background(), "fork_subagents", `{"subagents":[]}`); err == nil {
		t.Fatal("即使手写调用绕过可见面，teammate 工具面也必须拒绝 fork_subagents")
	}
	if len(inner.dispatch) != 0 {
		t.Fatalf("被拒绝的派发不应落到内层，得 %v", inner.dispatch)
	}
	if out, err := face.Dispatch(context.Background(), "read_file", `{}`); err != nil || out != "ok:read_file" {
		t.Fatalf("面内工具应原样放行，得 (%q, %v)", out, err)
	}
}

// TestTeammateToolFaceKeepsNil：未装配 agent 时收窄层不把 nil 包成非 nil 空壳
// （否则 newRoleEngine 的"agent 未装配"错误语义会被吞掉）。
func TestTeammateToolFaceKeepsNil(t *testing.T) {
	if teammateToolFace(nil) != nil {
		t.Fatal("inner 为 nil 时应原样返回 nil")
	}
}

// TestRoleEngineUsesTeammateToolFace 是装配点钉子：newRoleEngine 必须把 r.agt 经
// teammateToolFace 收窄后再交给 Session。若有人把这里改回裸 r.agt，收窄层就成了
// 没人用的死代码——而"teammate 工具面没有 fork_subagents"这条 D6 约束会静默失效。
func TestRoleEngineUsesTeammateToolFace(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	source, err := os.ReadFile(filepath.Join(dir, "runtime_role_turn.go"))
	if err != nil {
		t.Fatalf("读 runtime_role_turn.go 失败: %v", err)
	}
	if !strings.Contains(string(source), "teammateToolFace(r.agt)") {
		t.Fatal("newRoleEngine 未把 r.agt 经 teammateToolFace 收窄：D6『teammate 工具面硬移除 fork_subagents』在装配点被绕过")
	}
}
