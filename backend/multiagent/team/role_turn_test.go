package team

// role_turn_test.go 是员工回合三条判据的域级用例（P21-32：随实现从
// `backend/engine/assemble/runtime_role_turn.go` 搬进本域）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/session"
)

// TestWorkScopeRoundTripsThroughContext：身份经 ctx 往返；三项全空 = 不是员工回合（原样返回 ctx，
// 不挂值）；主会话或角色会话缺一 = "不是员工回合"（归属不成立，钩子不该播活动）。
func TestWorkScopeRoundTripsThroughContext(t *testing.T) {
	scope := WorkScope{MainSessionID: "s-main", RoleName: "exec", RoleSessionID: "role-exec"}
	ctx := WithWorkScope(context.Background(), scope)
	got, ok := WorkScopeFrom(ctx)
	if !ok || got != scope {
		t.Fatalf("身份往返 = %+v ok=%v", got, ok)
	}

	plain := context.Background()
	if value := WithWorkScope(plain, WorkScope{}); value != plain {
		t.Fatal("三项全空应当原样返回 ctx（不挂值）")
	}
	if _, ok := WorkScopeFrom(plain); ok {
		t.Fatal("没有身份时不该报告是员工回合")
	}
	if _, ok := WorkScopeFrom(nil); ok {
		t.Fatal("nil ctx 安全地回 false")
	}
	for name, partial := range map[string]WorkScope{
		"缺主会话":  {RoleName: "exec", RoleSessionID: "role-exec"},
		"缺角色会话": {RoleName: "exec", MainSessionID: "s-main"},
		"只有空白":  {MainSessionID: "  ", RoleSessionID: "\t", RoleName: "exec"},
	} {
		if _, ok := WorkScopeFrom(WithWorkScope(plain, partial)); ok {
			t.Fatalf("%s：归属不完整时不该报告是员工回合", name)
		}
	}
}

// TestToolActivityIDIsStableAcrossFrames：同一次调用（同轮次 + 同名 + 同参）的 started/completed
// 两帧必须同 ID（前端据此 upsert）；同名不同参不合并；不同轮次不合并。
func TestToolActivityIDIsStableAcrossFrames(t *testing.T) {
	base := session.ToolCallInfo{Name: "read_file", Turn: 3, Arguments: `{"path":"a.go"}`}
	first := ToolActivityID(base)
	completed := base
	completed.Result = "文件内容"
	completed.Duration = 12
	completed.Error = nil
	if ToolActivityID(completed) != first {
		t.Fatal("同一调用两帧必须同 ID（否则前端会把一次调用读成两条）")
	}
	otherArgs := base
	otherArgs.Arguments = `{"path":"b.go"}`
	if ToolActivityID(otherArgs) == first {
		t.Fatal("同名不同参不该合并")
	}
	otherTurn := base
	otherTurn.Turn = 4
	if ToolActivityID(otherTurn) == first {
		t.Fatal("不同轮次不该合并")
	}
	// 形状：<name>#<turn>#<8 位十六进制指纹>。
	parts := strings.Split(first, "#")
	if len(parts) != 3 || parts[0] != "read_file" || parts[1] != "3" || len(parts[2]) != 8 {
		t.Fatalf("活动 ID 形状 = %q", first)
	}
	// 失败帧同样按同一口径给 ID（错误信息不影响标识）。
	failed := base
	failed.Error = errors.New("boom")
	if ToolActivityID(failed) != first {
		t.Fatal("错误信息不该改变活动 ID")
	}
}

// TestDefaultRoleTurnPromptFramesTheRole：未登记提示词时的最小角色框架——点名角色名、
// 讲清"只处理本轮工作正文 / 不越权 / 证据落仓库 / 结论 ≤800 字"。
func TestDefaultRoleTurnPromptFramesTheRole(t *testing.T) {
	prompt := DefaultRoleTurnPrompt("exec")
	for _, want := range []string{"逻辑角色名 exec", "<task>", "<constraints>", "不越权", "≤800 字"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("最小角色框架缺 %q：%q", want, prompt)
		}
	}
	if strings.Contains(DefaultRoleTurnPrompt("pm"), "逻辑角色名 exec") {
		t.Fatal("角色名必须按参数落地")
	}
}
