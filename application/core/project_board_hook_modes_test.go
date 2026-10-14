package core

// project_board_hook_modes_test.go — 钉住触发档名的**词表唯一性**（M3 装配侧）。
//
// 两处要说同一件事：
//   - `ProjectBoardHookModes()` 是**装配期校验**用的词表（配置里拼错的名字要让启动
//     显式失败，而不是被 TriggerFor 的默认档兜成"看起来生效"）；
//   - `projectBoardTriggerTable` 是**运行时判据**（档名 → 命中集合）。
//
// 两者一旦分家，就会出现"配置里能写、运行时不认"（或反过来）这种只能靠读代码才能
// 发现的缝。这条用例把"恰好同一个集合"钉成不变量。

import "testing"

func TestProjectBoardHookModeVocabularyMatchesTable(t *testing.T) {
	modes := ProjectBoardHookModes()
	if len(modes) == 0 {
		t.Fatal("触发档名词表为空")
	}
	seen := make(map[string]struct{}, len(modes))
	for _, mode := range modes {
		if _, duplicated := seen[mode]; duplicated {
			t.Fatalf("档名 %q 在词表里出现两次", mode)
		}
		seen[mode] = struct{}{}
		if _, ok := projectBoardTriggerTable[mode]; !ok {
			t.Fatalf("档名 %q 在词表里，运行时判据表里却没有", mode)
		}
	}
	if len(seen) != len(projectBoardTriggerTable) {
		t.Fatalf("词表 %d 项、判据表 %d 项，二者必须恰好同一个集合（%v）",
			len(seen), len(projectBoardTriggerTable), modes)
	}

	// 装配期校验的判据就是这份词表：缺省档（空串）**不在**词表里（它由装配门补成
	// todo_list），而词表里的每一档都必须能被 TriggerFor 认下。
	if _, ok := seen[""]; ok {
		t.Fatal("空串是「未配置」，不是词表里的档名")
	}
	for _, mode := range modes {
		trigger := TriggerFor(mode)
		switch mode {
		case ProjectBoardHookModeOff:
			if !trigger.Off() {
				t.Fatal("off 档必须什么都不触发")
			}
		default:
			if trigger.Off() {
				t.Fatalf("%s 档不该是空触发面", mode)
			}
		}
	}

	// 默认档（缺省与未知都落到它）必须命中最典型的那两个动作、且不命中任意工具。
	defaultTrigger := TriggerFor("")
	if !defaultTrigger.Matches("todo_add") || !defaultTrigger.Matches("task_complete") {
		t.Fatal("缺省档（todo_list）必须命中 todo/task 族")
	}
	if defaultTrigger.Matches("read_file") {
		t.Fatal("缺省档不该命中普通读工具（否则等于 tool_complete 档）")
	}
}
