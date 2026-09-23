package core

import (
	"context"
	"strings"
	"testing"
)

// 未知命令 / 帮助文案的服务发现一致性。
//
// 两个真实来路（用户直接踩到）：
//   - `/` 建议面板是"全量入口"，把**工具**也列成候选（completion.go 的
//     Suggestions），但提交路径只认命令与 Skill（input.go 的 submitCommand）
//     ——照着面板打 `/compact_context` 只会得到"未知命令"，且不提示该怎么办；
//   - `/help` 的提示行只写 "/=命令"，与面板口径（命令 + 工具 + Skill）不一致。
func TestUnknownCommandNoticeSeparatesToolsFromCommands(t *testing.T) {
	runtime := &fakeRuntime{visibleTools: []Tool{
		{Name: "compact_context", Description: "fold the transcript now"},
		{Name: "bash", Description: "run a command"},
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	cases := []struct {
		name      string
		wantHints []string
	}{
		// 工具名（`/` 面板会列出来）→ 说清工具不能从输入框执行 + 指出命令入口。
		{name: "compact_context", wantHints: []string{"模型侧工具", "不能从输入框直接执行", "/compact"}},
		// 纯工具名 → 只说明工具属性，不编造命令。
		{name: "bash", wantHints: []string{"模型侧工具", "不能从输入框直接执行"}},
		// 拼写错（相邻换位）→ 直接给出正确写法。
		{name: "comapct", wantHints: []string{"你是想用 /compact 吗"}},
		// 完全无关 → 只说未知 + 指向 /help，不硬凑提示。
		{name: "nonsense_xyz", wantHints: []string{"未知命令: nonsense_xyz", "/help"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			notice := service.unknownCommandNotice(testCase.name)
			for _, want := range testCase.wantHints {
				if !strings.Contains(notice, want) {
					t.Fatalf("提示 %q 缺少 %q", notice, want)
				}
			}
			if testCase.name == "bash" && strings.Contains(notice, "你是想用") {
				t.Fatalf("纯工具名不该被映射到命令：%q", notice)
			}
			if testCase.name == "nonsense_xyz" {
				if strings.Contains(notice, "模型侧工具") || strings.Contains(notice, "你是想用") {
					t.Fatalf("无关名字不该硬凑提示：%q", notice)
				}
			}
		})
	}
}

// TestUnknownCommandSubmitKeepsNoticeNotError：未知命令仍然不是错误（照常回
// notice），只是这次 notice 带能走下去的提示。
func TestUnknownCommandSubmitKeepsNoticeNotError(t *testing.T) {
	runtime := &fakeRuntime{visibleTools: []Tool{{Name: "compact_context"}}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	if err := service.submitCommand(context.Background(), "/compact_context"); err != nil {
		t.Fatalf("未知命令不应报错：%v", err)
	}
}

// TestHelpStatesPanelListsOnlyExecutableEntries：`/help` 的口径必须与建议面板一致——
// 面板只列能从输入框直接提交的入口（命令 + Skill），工具压根不列出（2026-09-23 口径
// 修订：旧句式"面板同时列出命令、工具与 Skill 候选"已随 toolSuggestions 一起撤销）。
func TestHelpStatesPanelListsOnlyExecutableEntries(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	command, ok := service.commands.Get("help")
	if !ok {
		t.Fatal("/help 未注册")
	}
	result, err := command.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("/help: %v", err)
	}
	notice := result.Notice
	if !strings.Contains(notice, "可用命令:") || !strings.Contains(notice, "/compact") {
		t.Fatalf("/help 应列出已注册命令：%q", notice)
	}
	for _, want := range []string{
		"只列可直接提交的入口",
		"工具由模型调用、不在此列出",
		"先把该能力注册成命令",
		SigilSkill + "<name>",
	} {
		if !strings.Contains(notice, want) {
			t.Fatalf("/help 文案缺少 %q：%q", want, notice)
		}
	}
}
