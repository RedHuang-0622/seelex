package ssh

// files_test.go —— 远端文件面与作业面的判据：**路径与内容怎么过线**。
//
// 全部跑在进程内 sshd 上（与 ssh_test.go 同一套夹具）。这里断言的不是"远端真跑对了"，
// 而是**线上字节**：路径经 base64 承载（命令正文里不许出现路径原文），内容走 stdin
// （命令正文里不许出现内容），以及作业面把"仍在跑"与"跑完了"分得开。

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"

	xssh "golang.org/x/crypto/ssh"
)

// newClient 起一个假 sshd 并连上去（各用例共用，省掉重复的 known_hosts 样板）。
func newClient(t *testing.T, cfg serverConfig) *Client {
	t.Helper()
	srv := startServer(t, cfg)
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})
	client, err := Dial(context.Background(), spec)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// newBackend 同上但要 Backend（Backend 面每次调用自建连接）。
func newBackend(t *testing.T, cfg serverConfig) *Backend {
	t.Helper()
	srv := startServer(t, cfg)
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})
	return NewBackend(spec)
}

// assertWireSafe 断言命令正文是"单行 + 纯 ASCII"（否则它就不该走内联）。
func assertWireSafe(t *testing.T, command string) {
	t.Helper()
	if strings.ContainsAny(command, "\r\n") {
		t.Fatalf("生成的脚本必须是单行（远端用内联执行）：%q", command)
	}
	for _, r := range command {
		if r > 0x7f {
			t.Fatalf("生成的脚本必须纯 ASCII（非 ASCII 会经多层代码页静默损坏）：U+%04X in %q", r, command)
		}
	}
}

// decodeFirstPayload 取出脚本里第一个 base64 载荷并解码（判据：路径必须这样过线）。
func decodeFirstPayload(t *testing.T, command string) string {
	t.Helper()
	start := strings.Index(command, "printf %s '")
	if start < 0 {
		t.Fatalf("脚本里没有 base64 载荷：%q", command)
	}
	rest := command[start+len("printf %s '"):]
	end := strings.Index(rest, "'")
	if end < 0 {
		t.Fatalf("载荷没有闭合：%q", command)
	}
	decoded, err := base64.StdEncoding.DecodeString(rest[:end])
	if err != nil {
		t.Fatalf("载荷不是合法 base64：%v", err)
	}
	return string(decoded)
}

// ── 1) 读：路径经 base64 过线，内容按字节回来 ─────────────────────────

func TestReadFileCarriesPathAsBase64(t *testing.T) {
	remotePath := "/srv/配置 目录/app.yaml"
	content := "第一行\r\n第二行\n"
	var seen string
	backend := newBackend(t, serverConfig{
		exec: func(cmd string, ch xssh.Channel) uint32 {
			seen = cmd
			_, _ = ch.Write([]byte(base64.StdEncoding.EncodeToString([]byte(content))))
			return 0
		},
	})
	got, err := backend.ReadFile(context.Background(), remotePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertWireSafe(t, seen)
	// ① 路径不过 shell 词法：命令正文里不许出现路径原文，解码载荷必须等于路径。
	if strings.Contains(seen, "配置") || strings.Contains(seen, remotePath) {
		t.Fatalf("路径原文出现在命令正文里（会过 shell 词法）：%q", seen)
	}
	if decoded := decodeFirstPayload(t, seen); decoded != remotePath {
		t.Fatalf("载荷解码 = %q, want %q", decoded, remotePath)
	}
	if string(got.Data) != content {
		t.Fatalf("内容在传输中被改动：%q", string(got.Data))
	}
	if got.Truncated {
		t.Fatal("没有超上限不该标 truncated")
	}
}

func TestReadFileReportsRemoteIOFailure(t *testing.T) {
	backend := newBackend(t, serverConfig{
		exec: func(_ string, ch xssh.Channel) uint32 {
			_, _ = ch.Write([]byte("not a regular file"))
			return remoteExitNotRegularFile
		},
	})
	_, err := backend.ReadFile(context.Background(), "/nope")
	if err == nil {
		t.Fatal("远端失败必须报错")
	}
	if !strings.Contains(err.Error(), CodeRemoteFileIO) {
		t.Fatalf("错误应带远端文件 IO 码（不是传输码）：%v", err)
	}
}

// ── 2) 写：内容走 stdin（不进命令正文）────────────────────────────────

func TestWriteFileSendsContentOnStdin(t *testing.T) {
	content := "第一行\n$(rm -rf /tmp/none) `echo hi`\n"
	remotePath := "/srv/中文/配置.yaml"
	var (
		seen      string
		stdinBody string
	)
	backend := newBackend(t, serverConfig{
		exec: func(cmd string, ch xssh.Channel) uint32 {
			seen = cmd
			raw, _ := io.ReadAll(ch)
			stdinBody = string(raw)
			return 0
		},
	})
	written, err := backend.WriteFile(context.Background(), remotePath, content)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if written != len(content) {
		t.Fatalf("written = %d, want %d", written, len(content))
	}
	assertWireSafe(t, seen)
	if strings.Contains(seen, "rm -rf") || strings.Contains(seen, "配置") {
		t.Fatalf("内容/路径原文进了命令正文：%q", seen)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(stdinBody))
	if err != nil {
		t.Fatalf("stdin 不是 base64：%v", err)
	}
	// ② 数据走内容通道：字节透明，一个字节都不许变。
	if string(decoded) != content {
		t.Fatalf("内容在数据通道上被改动：%q", string(decoded))
	}
}

// ── 3) 改：替换语义与本地一致（全部出现处 + 计数）─────────────────────

func TestEditFileReplacesAllAndCounts(t *testing.T) {
	var stored string
	backend := newBackend(t, serverConfig{
		exec: func(cmd string, ch xssh.Channel) uint32 {
			switch {
			case strings.Contains(cmd, "base64 <"): // 读
				_, _ = ch.Write([]byte(base64.StdEncoding.EncodeToString([]byte(stored))))
				return 0
			case strings.Contains(cmd, "base64 -d >"): // 写
				// 写分支必须自己把 stdin 读干：内容走的是内容通道。
				raw, _ := io.ReadAll(ch)
				decoded, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
				stored = string(decoded)
				return 0
			}
			return 1
		},
	})
	stored = "旧值 旧值 别的\n"
	count, err := backend.EditFile(context.Background(), "/srv/app.conf", "旧值", "新值")
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2（全部出现处替换）", count)
	}
	if stored != "新值 新值 别的\n" {
		t.Fatalf("写回内容 = %q", stored)
	}
}

func TestEditFileReportsMissingOldString(t *testing.T) {
	backend := newBackend(t, serverConfig{
		exec: func(_ string, ch xssh.Channel) uint32 {
			_, _ = ch.Write([]byte(base64.StdEncoding.EncodeToString([]byte("没有那个串"))))
			return 0
		},
	})
	_, err := backend.EditFile(context.Background(), "/srv/app.conf", "不存在", "x")
	if err == nil || !strings.Contains(err.Error(), CodeRemoteEditNoMatch) {
		t.Fatalf("应报 old_string 不存在（带码），err=%v", err)
	}
}

// ── 4) 作业面：句柄自描述、状态三态、仍在跑拿得到句柄 ────────────────

func TestJobHandleRoundTripAndForeignRejected(t *testing.T) {
	handle := mintJobHandle("/tmp/seelex-host-job-1")
	dir, err := ParseJobHandle(handle)
	if err != nil || dir != "/tmp/seelex-host-job-1" {
		t.Fatalf("句柄回读失败：dir=%q err=%v", dir, err)
	}
	if !IsRemoteJobHandle(handle) {
		t.Fatal("自铸的句柄必须被认作远端句柄")
	}
	if _, err := ParseJobHandle("bt-7"); err == nil || !strings.Contains(err.Error(), "bt-") {
		t.Fatalf("本机句柄必须被远端面显式拒绝，err=%v", err)
	}
}

// jobServerConfig 是一个"作业面假远端"：按命令形状回答起/查/取。
func jobServerConfig(t *testing.T, exitLine, outPayload string) serverConfig {
	t.Helper()
	return serverConfig{
		exec: func(cmd string, ch xssh.Channel) uint32 {
			// 先把 stdin 读干（起作业的脚本正文走内容通道）：不读就回话并关通道，
			// 客户端那一侧的写会撞上已关闭的通道，用例会随机报 EOF。
			_, _ = io.ReadAll(ch)
			switch {
			case strings.Contains(cmd, "setsid"):
				_, _ = ch.Write([]byte(startMarker + "/tmp/seelex-host-job-1\n"))
				return 0
			case strings.Contains(cmd, `"$__d/pid"`):
				_, _ = ch.Write([]byte("pid=42\n" + exitLine + "\n"))
				return 0
			case strings.Contains(cmd, `base64 < "$__d/out"`):
				_, _ = ch.Write([]byte(outPayload))
				return 0
			}
			return 0
		},
	}
}

func TestStartJobMintsSelfDescribingHandle(t *testing.T) {
	backend := newBackend(t, jobServerConfig(t, JobRunning, ""))
	started, err := backend.StartJob(context.Background(), "echo 中文\n", "/srv/工作 目录")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Dir != "/tmp/seelex-host-job-1" {
		t.Fatalf("dir = %q（必须是远端展开后的路径）", started.Dir)
	}
	if started.Handle != mintJobHandle(started.Dir) {
		t.Fatalf("handle = %q", started.Handle)
	}
	if started.PID != "42" {
		t.Fatalf("pid = %q, want 42（监管进程写的）", started.PID)
	}
	if started.OutPath != started.Dir+"/out" {
		t.Fatalf("out path = %q", started.OutPath)
	}
}

func TestRunJobCompletedReadsOutputAndExitCode(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("远端输出\n"))
	backend := newBackend(t, jobServerConfig(t, JobExited+" 0", payload))
	res, err := backend.RunJob(context.Background(), "echo hello", "", 5*time.Second)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("status = %q, want completed", res.Status)
	}
	if res.ExitCode != 0 || res.Stdout != "远端输出\n" {
		t.Fatalf("exit=%d stdout=%q", res.ExitCode, res.Stdout)
	}
	if res.StillRunning {
		t.Fatal("跑完了不该 still_running")
	}
}

func TestRunJobStillRunningCarriesHandle(t *testing.T) {
	backend := newBackend(t, jobServerConfig(t, JobRunning, ""))
	res, err := backend.RunJob(context.Background(), "sleep 600", "", 60*time.Millisecond)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// ④ 预算用尽不是失败：仍在跑 + 句柄 + 日志落点，三项都要有。
	if res.Status != StatusChannelTimeout || !res.StillRunning {
		t.Fatalf("status=%q still_running=%v", res.Status, res.StillRunning)
	}
	if res.Handle == "" || res.LogPath == "" {
		t.Fatalf("仍在跑必须给 handle + log_path：%+v", res)
	}
	if res.Failed() {
		t.Fatal("预算用尽不得报成 failed")
	}
}
