package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 工具记录读数的会话级落盘：一行一批（整份快照）、读侧取最后一行、按会话隔离、
// 残尾/坏行跳过——2026-10-15 用户口径："走会话粒度下的 store + 批次更新"。
func TestToolRecordSnapshotIsSessionScopedAndLastLineWins(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()

	if _, found, err := repository.ReadToolRecordSnapshot(ctx, key); err != nil || found {
		t.Fatalf("还没写过时必须返回 found=false 且不报错：found=%v err=%v", found, err)
	}

	first := map[string]any{"at": "2026-10-15T10:00:00Z", "tools": []any{map[string]any{"tool": "bash", "success": 1}}}
	second := map[string]any{"at": "2026-10-15T10:05:00Z", "tools": []any{map[string]any{"tool": "bash", "success": 3, "failure": 1}}}
	if err := repository.AppendToolRecordSnapshot(ctx, key, first); err != nil {
		t.Fatalf("AppendToolRecordSnapshot: %v", err)
	}
	if err := repository.AppendToolRecordSnapshot(ctx, key, second); err != nil {
		t.Fatalf("AppendToolRecordSnapshot: %v", err)
	}

	raw, found, err := repository.ReadToolRecordSnapshot(ctx, key)
	if err != nil || !found {
		t.Fatalf("读回失败：found=%v err=%v", found, err)
	}
	if !strings.Contains(string(raw), `"success":3`) {
		t.Fatalf("必须取**最后一行**（整份最新读数）：%s", raw)
	}

	// 一行一批：文件行数 == 批次数。
	payload, err := os.ReadFile(filepath.Join(repository.layout.sessionRoot(key), ToolRecordFile))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if lines := strings.Count(string(payload), "\n"); lines != 2 {
		t.Fatalf("两次批量更新应恰好两行：%d\n%s", lines, payload)
	}

	// 按会话隔离：同项目另一个会话读不到这份读数。
	other := key
	other.SessionID = key.SessionID + "-other"
	if _, found, err := repository.ReadToolRecordSnapshot(ctx, other); err != nil || found {
		t.Fatalf("另一个会话不该看到这份读数：found=%v err=%v", found, err)
	}
}

// 崩溃语义：最后一行没写完（残尾）= 没提交，读侧回退到**上一行完整快照**。
func TestToolRecordSnapshotSkipsCrashTail(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	if err := repository.AppendToolRecordSnapshot(ctx, key, map[string]any{"at": "ok-1"}); err != nil {
		t.Fatalf("AppendToolRecordSnapshot: %v", err)
	}
	path := filepath.Join(repository.layout.sessionRoot(key), ToolRecordFile)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := file.WriteString(`{"at":"half`); err != nil { // 残尾：没有收尾换行
		t.Fatalf("WriteString: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, found, err := repository.ReadToolRecordSnapshot(ctx, key)
	if err != nil || !found {
		t.Fatalf("残尾不该让整份读数消失：found=%v err=%v", found, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("回退到上一行完整快照：%v（%s）", err, raw)
	}
	if decoded["at"] != "ok-1" {
		t.Fatalf("回退的行不对：%v", decoded)
	}
}
