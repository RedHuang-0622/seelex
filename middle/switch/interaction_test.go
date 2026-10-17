package switching

import (
	"testing"
	"time"
)

// TestInteractionVocabularyIsPinned 钉住词表与文案：它们是前端按钮的机器名与用户可见文字，
// 改一个字符就会让按钮失效或让模型读到别的指令。
func TestInteractionVocabularyIsPinned(t *testing.T) {
	kinds := map[string]string{
		InteractionKindApproval: "approval",
		InteractionKindSession:  "session",
		InteractionKindAccount:  "account",
	}
	for got, want := range kinds {
		if got != want {
			t.Fatalf("kind = %q，期望 %q", got, want)
		}
	}
	options := map[string]string{
		InteractionCancelOptionID: "__CANCEL__",
	}
	for got, want := range options {
		if got != want {
			t.Fatalf("选项 id = %q，期望 %q", got, want)
		}
	}
	if InteractionTitleSession == "" || InteractionTitleAccount == "" {
		t.Fatal("交互标题不得为空")
	}
}

// TestUnsupportedInteractionKind 未列出的 kind 必须带引号报错（宿主原样冒泡给前端）。
func TestUnsupportedInteractionKind(t *testing.T) {
	err := UnsupportedInteractionKind("mystery")
	if err == nil {
		t.Fatal("未知 kind 必须报错")
	}
	if want := `unsupported interaction kind "mystery"`; err.Error() != want {
		t.Fatalf("文案 = %q，期望 %q", err.Error(), want)
	}
}

// TestInteractionIDFormats 两类交互 ID 前缀不同（互不覆盖），且按 UnixNano 单调可区分。
func TestInteractionIDFormats(t *testing.T) {
	now := time.Unix(1700000000, 42)
	if got, want := SessionInteractionID(now), "session-1700000000000000042"; got != want {
		t.Fatalf("会话交互 ID = %q，期望 %q", got, want)
	}
	if got, want := AccountInteractionID(now), "account-1700000000000000042"; got != want {
		t.Fatalf("账号交互 ID = %q，期望 %q", got, want)
	}
	other := SessionInteractionID(now.Add(time.Nanosecond))
	if other == SessionInteractionID(now) {
		t.Fatal("不同时间戳必须给出不同 ID")
	}
}

// TestBuildSessionInteraction 标签回退与描述格式；ShortID 端口只在名称缺失时被调用。
func TestBuildSessionInteraction(t *testing.T) {
	openedAt := time.Unix(1700000000, 0)
	shortIDCalls := 0
	sessions := []SessionChoice{
		{ID: "seelex-1-1", Name: "带名字的会话", ShortID: func() string { shortIDCalls++; return "should-not-be-used" }, TokenCount: 12, UpdatedAt: time.Date(2026, 10, 5, 9, 7, 0, 0, time.UTC)},
		{ID: "seelex-2-2", ShortID: func() string { shortIDCalls++; return "seelex-2-2-…" }, TokenCount: 0, UpdatedAt: time.Date(2026, 10, 5, 9, 7, 0, 0, time.UTC)},
	}
	interaction := BuildSessionInteraction("session-1", sessions, openedAt)

	if interaction.Kind != InteractionKindSession || interaction.Title != InteractionTitleSession {
		t.Fatalf("kind/title = %q/%q", interaction.Kind, interaction.Title)
	}
	if interaction.ID != "session-1" || !interaction.OpenedAt.Equal(openedAt) {
		t.Fatalf("ID/OpenedAt = %q/%v", interaction.ID, interaction.OpenedAt)
	}
	if len(interaction.Options) != 2 {
		t.Fatalf("选项数 = %d，期望 2", len(interaction.Options))
	}
	if got := interaction.Options[0].Label; got != "带名字的会话" {
		t.Fatalf("有名字时标签 = %q，不得走回退", got)
	}
	if got := interaction.Options[0].Description; got != "tok:12  10-05 09:07" {
		t.Fatalf("描述 = %q", got)
	}
	if got := interaction.Options[1].Label; got != "seelex-2-2-…" {
		t.Fatalf("名缺失时标签 = %q，期望短 ID", got)
	}
	if shortIDCalls != 1 {
		t.Fatalf("短 ID 端口被调用 %d 次，期望 1（有名字的行不得调用）", shortIDCalls)
	}
	if got := interaction.Options[1].Description; got != "tok:0  10-05 09:07" {
		t.Fatalf("零 token 描述 = %q", got)
	}
	if interaction.SessionID != "" {
		t.Fatal("视图单格交互不填 SessionID")
	}
}

// TestBuildSessionInteractionWithoutShortIDPort 没装端口时不得 panic（名称缺失就留空标签）。
func TestBuildSessionInteractionWithoutShortIDPort(t *testing.T) {
	interaction := BuildSessionInteraction("session-1", []SessionChoice{{ID: "x"}}, time.Now())
	if got := interaction.Options[0].Label; got != "" {
		t.Fatalf("无端口且无名时标签 = %q，期望空", got)
	}
}

// TestBuildAccountInteraction 停用标记、`provider model` 去尾空与空账号池。
func TestBuildAccountInteraction(t *testing.T) {
	interaction := BuildAccountInteraction("account-1", []AccountChoice{
		{Name: "work", Provider: "anthropic", Model: "claude"},
		{Name: "personal", Disabled: true, Provider: "openai"},
		{Name: "bare"},
	}, time.Now())

	if interaction.Kind != InteractionKindAccount || interaction.Title != InteractionTitleAccount {
		t.Fatalf("kind/title = %q/%q", interaction.Kind, interaction.Title)
	}
	if got := interaction.Options[0].Label; got != "work" {
		t.Fatalf("可用账号标签 = %q", got)
	}
	if got := interaction.Options[0].Description; got != "anthropic claude" {
		t.Fatalf("描述 = %q", got)
	}
	if got := interaction.Options[1].Label; got != "personal [禁用]" {
		t.Fatalf("停用标签 = %q", got)
	}
	if got := interaction.Options[1].Description; got != "openai" {
		t.Fatalf("缺 model 的描述 = %q，期望去尾空", got)
	}
	if got := interaction.Options[2].Description; got != "" {
		t.Fatalf("空 provider/model 的描述 = %q，期望空", got)
	}
	if empty := BuildAccountInteraction("account-2", nil, time.Now()); len(empty.Options) != 0 {
		t.Fatalf("空账号池选项数 = %d，期望 0（且不是 nil）", len(empty.Options))
	}
}
