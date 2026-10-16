package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	seelebuiltin "github.com/RedHuang-0622/Seele/jobs/builtin"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 工具面与提示词的一致性门禁（2026-10-15 审计 D5 的落地）。
//
// 缺陷本体：`$teamwork` / `$goal` 的 SKILL 描述了一套工具面，而注册面是另一套——文档承诺
// 与注册事实**各长一半**，谁也不知道哪一半是真的。这条门禁把"两半"钉在一起：
//
//  1. **注册面 → 提示词**：凡注册给 leader 的 `team_*` 必须在 leader 的 SKILL 里出现
//     （注册了却没人教模型怎么用，等于没注册）；
//  2. **提示词 → 注册面**：SKILL 里出现的每个工具名必须在注册面存在；退役的老名字只有在
//     带退役标记（旧名 / 已删除 / 已退场…）的那一行里才允许出现——与
//     `job_migration_residue_test.go` 同一条纪律；
//  3. **句柄命名空间同源**：提示词里教的 token 集合必须**恰好等于**契约层的
//     `dto.JobNamespaces`——多教一个（模型去用不存在的表）或少教一个（新表没人知道）
//     都在这里红。
//
// 名单一律**从注册面读**（源码扫描，与 `TestEveryRegisteredToolDeclaresGroups` 同一把
// 扫描器），不在这条用例里另写一份工具名单——否则门禁本身就成了第三半。

// promptToolFaces 是"模型读得到的工具面"文件：leader 的两个 SKILL，加基础系统提示词。
var promptToolFaces = []string{
	filepath.Join("plugins", "default", "teamwork", "SKILL.md"),
	filepath.Join("plugins", "default", "goal", "SKILL.md"),
	filepath.Join("internal", "promptassets", "assets", "system", "instructions.md"),
}

// leaderSkillFiles 是"leader 工具面必须被教到"的文件（方向 1 的判据面）。
var leaderSkillFiles = []string{
	filepath.Join("plugins", "default", "teamwork", "SKILL.md"),
}

// retirementMarkers 与 `job_migration_residue_test.go` 同一套口径：只有在写明"它已退役"
// 的那一行里，老名字才允许出现（否则下一个人会照着它写回来）。
var retirementMarkers = []string{"旧名", "旧口径", "已下线", "下线", "已删除", "已退场", "退役", "retired"}

// toolMentionPattern 认模型面里**可能**是工具名的 token：team_* 一族 + 两个作业管理工具。
// 它刻意宽（会捞到参数名），由下面的 allowlist 与退役标记逐条解释，而不是靠更聪明的正则。
var toolMentionPattern = regexp.MustCompile(`\b(team_[a-z_]+|bg_manage|jobs_manage)\b`)

// toolFaceNonToolTokens 是"长得像工具名但其实不是"的登记表，每条都要写明理由。
var toolFaceNonToolTokens = map[string]string{
	"team_id": "team_plan 的入参名，不是工具",
}

// namespaceMentionPattern 认提示词里教的句柄形态（bt-3 / bs-7 / bteam-7 …）。
var namespaceMentionPattern = regexp.MustCompile(`\b([a-z]+)-\d`)

func TestPromptToolFacesMatchTheRegistrationSurface(t *testing.T) {
	registered, unresolved, files := scanToolRegistrations(t, ".")
	if len(registered) == 0 {
		t.Fatalf("没扫到任何工具注册点（扫了 %d 个文件）：用例本身失效，比断言失败更糟", files)
	}
	if len(unresolved) > 0 {
		t.Fatalf("注册点里有无法静态解析的名字，本门禁无从比对：%v", unresolved)
	}
	// 框架 builtin 不在 seelex 的源码注册点里：它的名字来自 Seele 的常量，读它才是同源。
	registered[seelebuiltin.ManageToolName] = "Seele jobs/builtin"

	// ── 方向 1：注册面 → 提示词 ────────────────────────────────────────
	registeredTeamTools := make([]string, 0)
	for name := range registered {
		if strings.HasPrefix(name, "team_") {
			registeredTeamTools = append(registeredTeamTools, name)
		}
	}
	sort.Strings(registeredTeamTools)
	if len(registeredTeamTools) == 0 {
		t.Fatal("注册面里没有任何 team_* 工具：扫描器口径变了，本门禁已经失效")
	}
	skillText := map[string]string{}
	for _, path := range leaderSkillFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s: %v", path, err)
		}
		skillText[path] = string(data)
	}
	for _, name := range registeredTeamTools {
		for path, text := range skillText {
			if !strings.Contains(text, name) {
				t.Errorf("工具 %s 注册了却没人教模型怎么用：%s 里没有它（注册面与提示词不许各长一半）", name, path)
			}
		}
	}

	// ── 方向 2：提示词 → 注册面 ────────────────────────────────────────
	for _, path := range promptToolFaces {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s: %v", path, err)
		}
		for index, line := range strings.Split(string(data), "\n") {
			for _, token := range toolMentionPattern.FindAllString(line, -1) {
				if _, ok := registered[token]; ok {
					continue
				}
				if reason, ok := toolFaceNonToolTokens[token]; ok {
					_ = reason
					continue
				}
				if hasRetirementMarker(line) {
					continue // 写明它已退役的行：允许出现，防的是"没人知道它是旧的"
				}
				t.Errorf("%s:%d 提到工具 %q，但注册面里没有它（提示词承诺 ≠ 注册事实）",
					filepath.ToSlash(path), index+1, token)
			}
		}
	}

	// ── 方向 3：句柄命名空间同源 ───────────────────────────────────────
	want := map[string]struct{}{}
	for _, namespace := range dto.JobNamespaces {
		want[string(namespace)] = struct{}{}
	}
	taught := map[string]struct{}{}
	for _, path := range promptToolFaces {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s: %v", path, err)
		}
		for _, match := range namespaceMentionPattern.FindAllStringSubmatch(string(data), -1) {
			if _, isNamespace := want[match[1]]; isNamespace {
				taught[match[1]] = struct{}{}
			}
		}
	}
	for token := range want {
		if _, ok := taught[token]; !ok {
			t.Errorf("句柄命名空间 %q 没有出现在模型面：模型不知道这一类句柄归哪张表", token)
		}
	}
	for token := range taught {
		if _, ok := want[token]; !ok {
			t.Errorf("模型面教了句柄 token %q，契约层没有它（提示词又跑到注册面前面去了）", token)
		}
	}
}

func hasRetirementMarker(line string) bool {
	for _, marker := range retirementMarkers {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}
