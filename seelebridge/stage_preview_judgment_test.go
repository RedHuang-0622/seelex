package seelebridge

// stage_preview_judgment_test.go — 步骤②的**跨层一致性红灯**：同一份"打点"在两条链上必须
// 走出**同一份载荷形状**、**同一个预览上界**、**同一种计量单位**。
//
// 这几条用例在收口前是红的（据此固定"要收什么"），收口后转绿：
//
//	① 预览上界有四份实现、两个数（阶段钩子按**字节**裁到 200；契约/teammate/恢复说明按
//	   rune 裁到 240）——同一份超长中文预览在两层落盘后**解出的打点不相等**；
//	② 按字节裁会把多字节字符切断 ⇒ 落盘/恢复说明里出现非法 UTF-8；
//	③ 同一份收尾（同一 stage + 同一预览）在 subagent 链与 teammate 链上必须解出同一条 Stage。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// wantClipPreview 是**测试自己**的那份期望（不调用生产实现）：同一份正文按 rune 裁到契约
// 上界、加省略号。用它断言"生产方的裁剪 = 契约口径"，而不是"生产方 = 生产方"。
func wantClipPreview(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if runes := []rune(flat); len(runes) > workunit.StagePreviewLimit {
		return string(runes[:workunit.StagePreviewLimit]) + "…"
	}
	return flat
}

// stageCapture 接住阶段钩子记录下来的打点（子代理写侧那条路的观察口）。
type stageCapture struct{ logs []model.NodeStageLog }

func (c *stageCapture) RecordStage(_ string, log model.NodeStageLog) {
	c.logs = append(c.logs, log)
}

// hookStagePreview 用**真正的生产者**（阶段钩子，子代理写侧）产出一条打点。
func hookStagePreview(t *testing.T, text string) model.NodeStageLog {
	t.Helper()
	capture := &stageCapture{}
	hook := telemetry.NewStageHook(capture)(nil)
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{NodeID: "n-1", Role: model.RoleSubAgent})
	if _, _, err := hook.Before(ctx, frameworktelemetry.Action{
		Type: frameworktelemetry.EventLLMBefore, Name: text,
	}); err != nil {
		t.Fatalf("hook.Before: %v", err)
	}
	if len(capture.logs) != 1 {
		t.Fatalf("阶段钩子必须记录一条打点，实际 %d 条", len(capture.logs))
	}
	return capture.logs[0]
}

// TestStagePreviewBoundIsOneAcrossProducers：同一份超长预览，两条链落盘后解出的打点必须
// **相等**，且各自的预览都不超过契约那一份上界、都是合法 UTF-8。
func TestStagePreviewBoundIsOneAcrossProducers(t *testing.T) {
	long := strings.Repeat("很长的预览", 200) // 1000 rune

	// subagent 链：阶段钩子产出 → 记录写侧 marshal 自己的打点类型（超集载荷）。
	hookLog := hookStagePreview(t, long)
	subPayload, err := json.Marshal([]model.NodeStageLog{hookLog})
	if err != nil {
		t.Fatalf("编码子代理打点载荷: %v", err)
	}
	// teammate 链：同一份收尾（同一 stage + 同一预览）走契约**唯一一份**编码。
	teamPayload := workunit.EncodeStages([]workunit.Stage{{Stage: hookLog.Stage, Preview: long}})

	subStages := workunit.DecodeStages(subPayload)
	teamStages := workunit.DecodeStages(teamPayload)
	if len(subStages) != 1 || len(teamStages) != 1 {
		t.Fatalf("两层都必须解出一条打点：subagent=%+v teammate=%+v", subStages, teamStages)
	}
	if subStages[0] != teamStages[0] {
		t.Errorf("同一份收尾在两层解出的打点不相同：subagent=%+v teammate=%+v", subStages[0], teamStages[0])
	}
	if !utf8.ValidString(hookLog.Preview) {
		t.Errorf("阶段钩子产出的预览不是合法 UTF-8（按字节裁切断了多字节字符）：%q", hookLog.Preview)
	}
	if runes := len([]rune(hookLog.Preview)); runes > workunit.StagePreviewLimit+1 {
		t.Errorf("阶段钩子的预览 = %d rune，超过契约上界 %d（+1 省略号）", runes, workunit.StagePreviewLimit)
	}
}

// TestResumeNotePreviewNeverSplitsARune：恢复说明里那条"之前做到哪"的预览，同样受**同一份**
// 上界约束，且不得把多字节字符切断（此前那份实现按字节裁）。
func TestResumeNotePreviewNeverSplitsARune(t *testing.T) {
	// 301 字节：第 240 个字节落在第 80 个汉字中间（字节切法必然切出一个非法 UTF-8）。
	long := "z" + strings.Repeat("汉", 100)
	record := sessionstore.NodeSessionRecord{
		StagesJSON: workunit.EncodeStages([]workunit.Stage{{Stage: "round_output", Preview: long}}),
	}
	got := lastSubagentStagePreview(record)
	if !utf8.ValidString(got) {
		t.Errorf("恢复说明的\"之前做到哪\"不是合法 UTF-8（切断多字节字符）：%q", got)
	}
	if want := "round_output: " + wantClipPreview(long); got != want {
		t.Errorf("恢复说明的预览 = %q，想要 %q（唯一一份裁切口径）", got, want)
	}
}

// TestResumeNoteContainerIsOneAndKeepsCallSiteSemantics：恢复说明的**容器只有一份**，而
// "读完即消 / 非消费读 / 丢掉"这三种语义留在此前的调用口径上（键语义在各层）。
func TestResumeNoteContainerIsOneAndKeepsCallSiteSemantics(t *testing.T) {
	runtime := &Runtime{}

	// subagent 侧：节点装配时是**非消费读**（同一次装配可能读多次），收尾时清掉。
	runtime.setSubagentResumeNote("n-1", "恢复说明")
	for i := 0; i < 2; i++ {
		if got := runtime.SubagentResumeNote("n-1"); got != "恢复说明" {
			t.Fatalf("第 %d 次非消费读 = %q，想要 %q", i+1, got, "恢复说明")
		}
	}
	runtime.clearSubagentResumeNote("n-1")
	if got := runtime.SubagentResumeNote("n-1"); got != "" {
		t.Fatalf("清掉之后不得再读出来：%q", got)
	}

	// teammate 侧：worker 回合装配系统提示时**取走即消**。
	runtime.setTeamResumeNote("role-1", "恢复说明")
	if got := runtime.consumeTeamResumeNote("role-1"); got != "恢复说明" {
		t.Fatalf("取走 = %q，想要 %q", got, "恢复说明")
	}
	if got := runtime.consumeTeamResumeNote("role-1"); got != "" {
		t.Fatalf("读完即消：第二次取走 = %q，想要空串", got)
	}
	runtime.setTeamResumeNote("role-2", "恢复说明")
	runtime.clearTeamResumeNote("role-2")
	if got := runtime.consumeTeamResumeNote("role-2"); got != "" {
		t.Fatalf("丢掉之后不得再取出来：%q", got)
	}

	// 空键 / 空说明 = 不记（幂等），不制造"空恢复说明"这种假事实。
	runtime.setSubagentResumeNote("", "x")
	runtime.setSubagentResumeNote("n-2", "   ")
	if got := runtime.SubagentResumeNote("n-2"); got != "" {
		t.Fatalf("空说明不得入容器：%q", got)
	}
}
