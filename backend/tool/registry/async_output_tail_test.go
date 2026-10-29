package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// writeTempLog 落一份作业输出文件（读口用例的输入）。
func writeTempLog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "job.log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// mustCursorForTest 取某句柄"已交付给模型"的偏移（读口不得推进它）。
func (g *asyncRegistry) mustCursorForTest(t *testing.T, handle string) int64 {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	run, ok := g.runs[handle]
	if !ok {
		t.Fatalf("句柄 %q 不在册", handle)
	}
	return run.cursor
}

// 后台作业的「实时输出读口」判据（详情弹窗那条链）。
//
// 它要同时满足三件事，缺一件就会让界面说谎：
//  1. **只看尾巴**：弹窗每 1.5s 拉一次，给全文就是按秒烧带宽；
//  2. **不消费**：拉多少次都不推进游标——取回语义只属于 bg_manage(op=fetch)；
//  3. **能直接进 DOM**：UTF-8 边界对齐、控制字符与 ANSI 转义清掉。

func TestAsyncOutputTailReadsTailWithoutAdvancingCursor(t *testing.T) {
	router := asyncTestRouter(t, true)
	run, _, err := router.async.begin("sess-t", "go test ./...", "跑一整轮集成测试", "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	writeLogForTest(t, run.logPath, "=== RUN   TestFoo\n--- PASS: TestFoo (1.20s)\n")

	tail := router.AsyncOutputTail(run.handle, 4096)
	if !tail.Found {
		t.Fatal("在册句柄必须 Found=true")
	}
	if !strings.Contains(tail.Output, "--- PASS: TestFoo") || !strings.Contains(tail.Output, "=== RUN") {
		t.Fatalf("输出窗口必须带多行正文: %q", tail.Output)
	}
	if tail.Windowed {
		t.Fatal("4096 字节窗口装得下这段输出，不该标 Windowed")
	}

	// 不消费：游标仍停在 0，随后的一次取回还能拿到全部内容。
	if cursor := router.async.mustCursorForTest(t, run.handle); cursor != 0 {
		t.Fatalf("读屏不得推进游标: cursor=%d", cursor)
	}
	_, delta, _, ok := router.async.advanceTail(run.handle, 4096)
	if !ok || !strings.Contains(delta, "=== RUN   TestFoo") {
		t.Fatalf("读屏之后取回仍必须给出全部未交付字节: ok=%v delta=%q", ok, delta)
	}
}

func TestAsyncOutputTailUnknownHandleDoesNotFabricate(t *testing.T) {
	router := asyncTestRouter(t, true)
	tail := router.AsyncOutputTail("bt-999", 4096)
	if tail.Found || tail.Output != "" || tail.Windowed {
		t.Fatalf("不在册的句柄不得编造读数: %+v", tail)
	}
}

// 窗口切在多字节字符中间时，起点必须对齐到字符边界；残行（前半截不在窗口里）
// 直接丢掉——半个汉字进了 DOM 就是乱码。
func TestReadLogTailAlignsUTF8AndDropsPartialFirstLine(t *testing.T) {
	path := writeTempLog(t, strings.Repeat("中文输出行\n", 200))

	output, windowed := readLogTail(path, 64)
	if !windowed {
		t.Fatal("64 字节窗口必然只覆盖文件末尾")
	}
	if !utf8.ValidString(output) {
		t.Fatalf("窗口正文不是合法 UTF-8: %q", output)
	}
	if strings.HasPrefix(output, "文") || strings.HasPrefix(output, "出") {
		t.Fatalf("残行必须丢掉，不能从半行中间开始: %q", output)
	}
	if !strings.Contains(output, "中文输出行") {
		t.Fatalf("窗口正文丢了完整行: %q", output)
	}
}

// ANSI 转义与控制字符不得进渲染：进度条类输出会把界面咬花。
func TestReadLogTailStripsANSISequences(t *testing.T) {
	path := writeTempLog(t, "\x1b[2K\x1b[32mPASS\x1b[0m ok\r\nnext\tcell\x00end\n")

	output, windowed := readLogTail(path, 4096)
	if windowed {
		t.Fatal("4444 字节窗口装得下这段输出")
	}
	if strings.ContainsRune(output, 0x1b) || strings.Contains(output, "[2K") || strings.Contains(output, "[32m") {
		t.Fatalf("ANSI 转义没清掉: %q", output)
	}
	if strings.ContainsRune(output, '\r') || strings.ContainsRune(output, 0) {
		t.Fatalf("控制字符没清掉: %q", output)
	}
	if !strings.Contains(output, "PASS") || !strings.Contains(output, "ok") || !strings.Contains(output, "next\tcell") {
		t.Fatalf("清洗不得吞掉正文（换行/制表保留）: %q", output)
	}
}
