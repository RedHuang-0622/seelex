package agentteam

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// prefix_test.go — 钉住「team work 正文前缀」的两个口径：
//
//  1. **作者是主会话上下文（含主会话 draft），不是本包**：Runtime.NoteMainContext
//     接收一次只读装配（RoleWireSnapshot，来自 assembleRoleWire 的 main 装配），
//     本包只把它投影成正文。前缀经调度器在交班时下发给下一个发言成员，并只读
//     投影进 Snapshot().Prefix 供前端快照查看。
//  2. **前端没有任何写入口**：gui 与 contract（端口/DTO 面）不得出现写前缀的
//     符号。用户明确担心的那条路径是"前端渲染结果回写后端 → 后端真值变成前端
//     派生物 → 前缀与帧账本/缓存前缀不再匹配"，这里用源码扫描把它挡住。

// TestNoteMainContextFeedsSpeakerPrefix：主会话 wire 的正文进前缀，交班时真的下发给
// 发言者；口径锚点随快照只读暴露。
func TestNoteMainContextFeedsSpeakerPrefix(t *testing.T) {
	runtime := newTestRuntime([]string{"main"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	runtime.NoteMainContext(dto.RoleWireSnapshot{
		MainSessionID: "main-1",
		RoleName:      "main",
		AppliedSeq:    7,
		TailStartSeq:  5,
		PrefixDigest:  "digest-abc",
		Messages: []dto.RoleWireMessage{
			{Role: "user", Content: "改了 chat.go 的注入顺序", Seq: 5},
			{Role: "assistant", Content: "跑了 go test ./application/core", Seq: 6},
			{Role: "assistant", ToolCalls: []dto.RoleToolCall{{Name: "go_test"}}, Seq: 7},
			{Role: "assistant", Content: "   ", Seq: 8}, // 空行不占位
		},
	})

	request, ok := runtime.Next()
	if !ok {
		t.Fatal("环绕一圈没有拿到发言者")
	}
	prefix := runtime.Prefix()
	if prefix == "" {
		t.Fatal("前缀为空：主会话 wire 没有进前缀")
	}
	if request.Prefix != prefix {
		t.Fatalf("交班下发的不是当前前缀：%q ≠ %q", request.Prefix, prefix)
	}
	for _, want := range []string{
		"起点 → 当前位置",
		"user: 改了 chat.go 的注入顺序",
		"assistant: 跑了 go test ./application/core",
		"assistant: (tool_calls: go_test)",
	} {
		if !strings.Contains(prefix, want) {
			t.Fatalf("前缀缺少 %q：%q", want, prefix)
		}
	}
	snapshot := runtime.Snapshot()
	if snapshot.Prefix != prefix || snapshot.PrefixParts != 3 || snapshot.PrefixChars != len([]rune(prefix)) {
		t.Fatalf("快照前缀读数不一致：parts=%d chars=%d prefix=%q", snapshot.PrefixParts, snapshot.PrefixChars, snapshot.Prefix)
	}
	if snapshot.PrefixDigest != "digest-abc" || snapshot.PrefixAppliedSeq != 7 || snapshot.PrefixTailSeq != 5 {
		t.Fatalf("口径锚点没有随快照暴露：digest=%q applied=%d tail=%d",
			snapshot.PrefixDigest, snapshot.PrefixAppliedSeq, snapshot.PrefixTailSeq)
	}
}

// TestNoteMainContextProjectsWholeWire：前缀就是主会话上下文本身——本包不再二次
// 截断、也不再自己攒段。若要收窄，只能由装配侧的 budget/k 决定（NeedCompact 是显式
// 信号），否则前缀当场不再等于主会话上下文（前后端口径分叉）。
func TestNoteMainContextProjectsWholeWire(t *testing.T) {
	runtime := newTestRuntime([]string{"main"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	messages := make([]dto.RoleWireMessage, 0, 40)
	for index := 0; index < 40; index++ {
		messages = append(messages, dto.RoleWireMessage{
			Role: "main", Content: fmt.Sprintf("第 %d 行长正文 %s", index, strings.Repeat("x", 200)), Seq: uint64(index + 1),
		})
	}
	runtime.NoteMainContext(dto.RoleWireSnapshot{Messages: messages, NeedCompact: true})

	snapshot := runtime.Snapshot()
	if snapshot.PrefixParts != len(messages) {
		t.Fatalf("前缀行数应等于 wire 行数 %d，得 %d", len(messages), snapshot.PrefixParts)
	}
	for _, want := range []string{"第 0 行长正文", "第 39 行长正文"} {
		if !strings.Contains(snapshot.Prefix, want) {
			t.Fatalf("前缀缺少 %q：本包不该二次截断主会话上下文", want)
		}
	}
	if !snapshot.PrefixNeedCompact {
		t.Fatal("装配侧要求压缩的信号应随快照暴露，而不是本包自己截")
	}
	if strings.Contains(snapshot.Prefix, "已截断") {
		t.Fatal("前缀里出现了本包自造的截断标记：前缀必须等于主会话上下文")
	}
}

// TestNoteMainContextEmptyWireStaysQuiet：主会话还没有可装配的正文时前缀为空，
// 而不是塞一段占位文本。
func TestNoteMainContextEmptyWireStaysQuiet(t *testing.T) {
	runtime := newTestRuntime([]string{"main"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	runtime.NoteMainContext(dto.RoleWireSnapshot{Messages: []dto.RoleWireMessage{
		{Role: "assistant", Content: "  "},
	}})
	if prefix := runtime.Prefix(); prefix != "" {
		t.Fatalf("没有正文的主会话不该产生前缀，得 %q", prefix)
	}
	if parts := runtime.Snapshot().PrefixParts; parts != 0 {
		t.Fatalf("空 wire 的行数应为 0，得 %d", parts)
	}
}

// TestTeamPrefixHasNoFrontendWriteEntry 是**守卫用例**：前缀的写入口只能在后端，
// 且只有一处。
//
// 为什么值得一条源码扫描：前缀一旦能被前端回写，后端真值就变成"前端渲染出来的
// 文本"，而 b（ADVISOR）回合输入与 work.progress 帧是按后端事实渲染的——两者会
// 立刻分叉，缓存前缀失效、上下文前缀大量不匹配（这正是用户提出的担心）。
// 单测只能证明"当前实现没这么做"，扫描才能证明"没人加回来"。
func TestTeamPrefixHasNoFrontendWriteEntry(t *testing.T) {
	root := prefixRepoRoot(t)
	sites := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && prefixSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		if !strings.Contains(text, "NoteMainContext(") && !strings.Contains(text, "SetPrefix(") {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relative = path
		}
		sites[filepath.ToSlash(relative)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("扫描仓库源码失败: %v", err)
	}

	want := []string{
		"application/core/agentteam/runtime.go",   // NoteMainContext 定义 + 唯一 SetPrefix 调用
		"application/core/agentteam/scheduler.go", // SetPrefix 定义（纯原语）
		"application/core/agentteam_runtime.go",   // 唯一作者调用点：读主会话上下文（含 draft）
	}
	for _, path := range want {
		if !sites[path] {
			t.Fatalf("前缀写面缺少 %s（实际 %v）", path, keysOf(sites))
		}
		delete(sites, path)
	}
	if len(sites) > 0 {
		t.Fatalf("前缀出现了预期外的写面 %v：唯一作者必须是主会话上下文（含主会话 draft）的只读装配", keysOf(sites))
	}

	// 前端面与端口/DTO 面：一个字符都不许出现（连读符号都不该有提议写的口子）。
	for _, dir := range []string{"gui", filepath.Join("application", "contract")} {
		func() {
			base := filepath.Join(root, dir)
			if _, statErr := os.Stat(base); statErr != nil {
				return
			}
			_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
					return nil
				}
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				text := string(data)
				if strings.Contains(text, "NoteMainContext(") || strings.Contains(text, "SetPrefix(") {
					t.Fatalf("%s 出现前缀写符号：前端/DTO 面只能是只读投影", path)
				}
				return nil
			})
		}()
	}
}

func keysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

// prefixSkipDir 报告扫描时应跳过的目录（构建产物 / 依赖缓存 / 临时现场）。
// 与 scheduler_wiring_test.go 同口径，额外跳过下划线前缀的临时目录。
func prefixSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "tmp", "dist", "node_modules", "vendor":
		return true
	}
	return false
}

func prefixRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("未找到仓库根（go.mod）")
		}
		dir = parent
	}
}
