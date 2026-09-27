package main

// Windows 图标资源的回归护栏。
//
// 图标是"表现层但最容易悄悄丢"的东西：换了品牌图没重新生成 .ico/.syso、.syso 被误删，
// exe 都会静默退回默认图标（快捷方式/任务栏/资源管理器一起变），而没有任何编译期信号。
// 这里把"源图 → .ico → .syso"这条链钉住：
//   - icon.rc 必须引用 seelex.ico；
//   - .ico 必须是多档 ICO（资源管理器/任务栏/标题栏各取一档）；
//   - .syso 必须是 x86-64 COFF，且**内容里能原样找到 .ico 的最大一档**——用内容包含
//     而不是时间戳，因为 git 不保留 mtime（克隆后时间戳不可信，内容关系恒真）。

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func TestWindowsIconResourcePipeline(t *testing.T) {
	t.Parallel()

	rc := readRepoFile(t, "gui/icons/icon.rc")
	if !strings.Contains(rc, `ICON "seelex.ico"`) {
		t.Fatalf("gui/icons/icon.rc 必须声明 ICON \"seelex.ico\"，实际内容：%q", rc)
	}

	script := readRepoFile(t, "scripts/make-icon.sh")
	for _, token := range []string{"windres", "--syso-only", "seelex.ico", "assets/seelex-logo.png"} {
		if !strings.Contains(script, token) {
			t.Errorf("scripts/make-icon.sh 缺少 %q（图标链的唯一再生成入口）", token)
		}
	}

	ico, err := os.ReadFile("gui/icons/seelex.ico")
	if err != nil {
		t.Fatalf("read gui/icons/seelex.ico: %v", err)
	}
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[0:2]) != 0 || binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		t.Fatalf("gui/icons/seelex.ico 不是 ICO 文件（magic 不对）")
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	if count < 4 {
		t.Fatalf("ICO 只有 %d 档：至少要给 16/32/48/256", count)
	}
	want := map[int]bool{16: false, 32: false, 48: false, 256: false}
	for i := 0; i < count; i++ {
		entry := ico[6+i*16:]
		if len(entry) < 16 {
			t.Fatalf("ICO 目录第 %d 项被截断", i)
		}
		width := int(entry[0])
		if width == 0 {
			width = 256 // ICO 用 0 表示 256
		}
		if _, ok := want[width]; ok {
			want[width] = true
		}
	}
	for size, found := range want {
		if !found {
			t.Errorf("ICO 缺少 %d×%d 档（资源管理器 16/32、任务栏 32/48、高 DPI 256 各取一档）", size, size)
		}
	}

	syso, err := os.ReadFile("rsrc_windows_amd64.syso")
	if err != nil {
		t.Fatalf("read rsrc_windows_amd64.syso: %v —— 该文件入库（Go 在 windows/amd64 构建时按文件名自动链接），删了就没了图标；重建：scripts/make-icon.sh", err)
	}
	if machine := binary.LittleEndian.Uint16(syso[0:2]); machine != 0x8664 {
		t.Fatalf("rsrc_windows_amd64.syso 不是 x86-64 COFF（machine=0x%04x），无法被 windows/amd64 链接", machine)
	}

	// 内容关系：.syso 里必须原样含 .ico 的最大一档（windres 把 ICON 负载按原字节写进
	// .rsrc）。改了品牌图只重新生成 .ico 而忘了 .syso 时，这条会红并指出怎么修。
	last := ico[6+(count-1)*16:]
	off := int(binary.LittleEndian.Uint32(last[12:16]))
	size := int(binary.LittleEndian.Uint32(last[8:12]))
	if off <= 0 || size <= 0 || off+size > len(ico) {
		t.Fatalf("ICO 目录项越界：off=%d size=%d total=%d", off, size, len(ico))
	}
	if !bytes.Contains(syso, ico[off:off+size]) {
		t.Errorf("rsrc_windows_amd64.syso 里找不到 ICO 最大档（%d B）——图标已更新但资源没重建，跑 scripts/make-icon.sh", size)
	}
}
