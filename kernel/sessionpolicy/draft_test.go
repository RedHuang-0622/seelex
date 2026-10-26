package switching

import (
	"strings"
	"testing"
	"time"
)

// TestDraftConstantsArePersistentKeys 钉住两个会被"顺手改掉"的常量：显示名影响前端第一眼，
// 前缀是**持久化键的一部分**（会话记录键、权限档位槽、工作区绑定都按它寻址），改动等于换 ID 口径。
func TestDraftConstantsArePersistentKeys(t *testing.T) {
	if DraftSessionName != "新会话" {
		t.Fatalf("草稿显示名 = %q，期望 %q", DraftSessionName, "新会话")
	}
	if DraftSessionIDPrefix != "seelex" {
		t.Fatalf("早分配 SID 前缀 = %q，期望 %q（改它等于换持久化键口径）", DraftSessionIDPrefix, "seelex")
	}
}

// TestNewDraftSessionID 口径：`<前缀>-<UnixNano>-<序号>`；同 tick 靠序号去重，序号相同则时间戳区分。
func TestNewDraftSessionID(t *testing.T) {
	now := time.Unix(1700000000, 123456789)

	first := NewDraftSessionID(now, 1)
	wantPrefix := DraftSessionIDPrefix + "-" + "1700000000123456789" + "-1"
	if first != wantPrefix {
		t.Fatalf("ID = %q，期望 %q", first, wantPrefix)
	}

	if same := NewDraftSessionID(now, 2); same == first {
		t.Fatalf("同一时间戳、不同序号不得撞车：%q", same)
	}
	if other := NewDraftSessionID(now.Add(time.Nanosecond), 1); other == first {
		t.Fatalf("同序号、不同时间戳不得撞车：%q", other)
	}
	if !strings.HasPrefix(first, DraftSessionIDPrefix+"-") {
		t.Fatalf("ID 必须以 %q 开头：%q", DraftSessionIDPrefix+"-", first)
	}
}

// TestDraftTargetMatches 是廉价闸门的两来源真值表：槽命中或视图命中即算命中，都不命中不算。
func TestDraftTargetMatches(t *testing.T) {
	cases := []struct {
		name   string
		target DraftTarget
		id     string
		want   bool
	}{
		{"槽命中（视图已切走）", DraftTarget{HasSlot: true, SlotID: "seelex-1-1", ViewDraft: false, ViewID: "seelex-9-9"}, "seelex-1-1", true},
		{"视图命中（槽已物化清空）", DraftTarget{HasSlot: false, ViewDraft: true, ViewID: "seelex-1-1"}, "seelex-1-1", true},
		{"两来源都指向同一份", DraftTarget{HasSlot: true, SlotID: "seelex-1-1", ViewDraft: true, ViewID: "seelex-1-1"}, "seelex-1-1", true},
		{"槽存在但 SID 空串、目标也是空串 = 命中（归位前口径）", DraftTarget{HasSlot: true, ViewDraft: true, ViewID: ""}, "", true},
		{"槽存在但 SID 空串、目标非空 = 不命中", DraftTarget{HasSlot: true, ViewDraft: false}, "seelex-1-1", false},
		{"无槽、视图非草稿 = 不命中", DraftTarget{ViewID: "seelex-2-2"}, "seelex-1-1", false},
		{"无槽、视图是草稿但 ID 不同 = 不命中", DraftTarget{ViewDraft: true, ViewID: "seelex-2-2"}, "seelex-1-1", false},
		{"槽指向别份会话 = 不命中", DraftTarget{HasSlot: true, SlotID: "seelex-2-2", ViewDraft: true, ViewID: "seelex-2-2"}, "seelex-1-1", false},
	}
	for _, testCase := range cases {
		if got := testCase.target.Matches(testCase.id); got != testCase.want {
			t.Fatalf("%s：Matches(%q) = %v，期望 %v", testCase.name, testCase.id, got, testCase.want)
		}
	}
}

// TestDraftTargetInView 是物化闸门的真值表：视图必须停在草稿上；槽为空（还没物化过）仍算本次。
func TestDraftTargetInView(t *testing.T) {
	cases := []struct {
		name   string
		target DraftTarget
		id     string
		want   bool
	}{
		{"视图停在草稿、槽认同一 SID", DraftTarget{HasSlot: true, SlotID: "seelex-1-1", ViewDraft: true, ViewID: "seelex-1-1"}, "seelex-1-1", true},
		{"视图停在草稿、槽已空（尚未物化）", DraftTarget{ViewDraft: true, ViewID: "seelex-1-1"}, "seelex-1-1", true},
		{"视图已切走但槽仍命中 = 不命中（这就是与 Matches 的差别）", DraftTarget{HasSlot: true, SlotID: "seelex-1-1", ViewID: "seelex-9-9"}, "seelex-1-1", false},
		{"视图停在草稿但槽指向别份 = 不命中", DraftTarget{HasSlot: true, SlotID: "seelex-2-2", ViewDraft: true, ViewID: "seelex-1-1"}, "seelex-1-1", false},
		{"视图非草稿、无槽 = 不命中", DraftTarget{ViewID: "seelex-1-1"}, "seelex-1-1", false},
		{"空目标且视图停在空 ID 的草稿 = 命中（锁内复判口径）", DraftTarget{HasSlot: true, ViewDraft: true}, "", true},
	}
	for _, testCase := range cases {
		if got := testCase.target.InView(testCase.id); got != testCase.want {
			t.Fatalf("%s：InView(%q) = %v，期望 %v", testCase.name, testCase.id, got, testCase.want)
		}
	}
}
