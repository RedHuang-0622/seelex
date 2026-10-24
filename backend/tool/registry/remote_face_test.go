package registry

// remote_face_test.go —— 六个内置工具的**远端面判据**（假传输驱动，不碰网络）。
//
// 六个判据，每个都对应一条硬缺口：
//
//  1. 读/写/改/跑/前台与后台六个工具在选中 ssh 时**真的走远端面**，且回执自报
//     execution_surface 与 env；
//  2. 远端路径与命令正文**原样**送到对端（不做本地根解析、不改写命令里的路径）；
//  3. 内容与 stdin 数据作为**独立字段**送出（不拼进命令正文）；
//  4. 超时 / 截断 / 仍在跑三件事在回执里**分开**（不互相顶替）；
//  5. 本地面行为逐字不变（read_file 仍是正文，其它回执只多两格）；
//  6. 远端作业句柄与本机作业句柄**互不相认**（语法上不可能误命中）。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/seelex/backend/limit/gate"
	"github.com/RedHuang-0622/seelex/backend/tool/io/fs"
)

// remoteFaceRouter 造一个只够远端面判据用的 Router（本地依赖给最小可用值）。
func remoteFaceRouter(t *testing.T) *Router {
	t.Helper()
	root := t.TempDir()
	scope := security.NewProjectScope()
	if err := scope.BindFor("sess-face", root); err != nil {
		t.Fatalf("bind scope: %v", err)
	}
	return NewRouter(Deps{
		ProjectScope:           scope,
		SessionKey:             func(context.Context) string { return "sess-face" },
		FileSystem:             fs.NewFileSystemActor(),
		AsyncExecEnabled:       true,
		DisableDockerAutoStart: true,
	})
}

// jsonString 把任意字符串折成 JSON 字面量（引号、换行、非 ASCII 全部按 JSON 规则转义）。
//
// 不复用 quoteJSON（那个只转义反斜杠与引号，够给路径用）：本文件的入参里有换行与中文，
// 拼错会变成"args 解析失败"，看着像实现坏了。
func jsonString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %q: %v", value, err)
	}
	return string(encoded)
}

// decodeReceipt 把工具回执解成字段表（远端回执是 JSON）。
func decodeReceipt(t *testing.T, payload string) map[string]any {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(payload), &fields); err != nil {
		t.Fatalf("回执不是 JSON（%v）：%s", err, payload)
	}
	return fields
}

// requireFace 断言回执里的执行面与环境名两格。
func requireFace(t *testing.T, fields map[string]any, surface, env string) {
	t.Helper()
	if got := fields["execution_surface"]; got != surface {
		t.Fatalf("execution_surface = %v, want %q（回执：%v）", got, surface, fields)
	}
	if got := fields["env"]; got != env {
		t.Fatalf("env = %v, want %q（回执：%v）", got, env, fields)
	}
}

// ── 1) read_file：远端面 ────────────────────────────────────────────

func TestRemoteReadFileUsesRemoteFaceAndKeepsPathVerbatim(t *testing.T) {
	router := remoteFaceRouter(t)
	// 远端路径带中文与空格：它**不能**过 shell 词法，也不该被本地根解析。
	remotePath := "/srv/配置 目录/app.yaml"
	fake := &fakeTransport{readOut: TransportReadOutput{
		Content: "第一行\n第二行", TotalLines: 2, Bytes: 37,
	}}
	args := `{"path":` + jsonString(t, remotePath) + `,"start_line":1,"end_line":2}`

	payload, err := router.scopedReadFile(sshContext(fake, "proj-1", "deploy.internal"), args)
	if err != nil {
		t.Fatalf("远端读失败：%v", err)
	}
	fields := decodeReceipt(t, payload)
	requireFace(t, fields, "ssh", "deploy.internal")
	if fields["content"] != "第一行\n第二行" {
		t.Fatalf("content = %v", fields["content"])
	}
	if fields["path"] != remotePath {
		t.Fatalf("回执 path = %v, want 原样远端路径", fields["path"])
	}
	reads := fake.readCalls()
	if len(reads) != 1 || reads[0].Path != remotePath {
		t.Fatalf("远端实际收到 %+v，want 一条 path=%q", reads, remotePath)
	}
	if reads[0].StartLine != 1 || reads[0].EndLine != 2 {
		t.Fatalf("行窗口没有传给远端：%+v", reads[0])
	}
}

func TestLocalReadFileStillReturnsRawContent(t *testing.T) {
	router := remoteFaceRouter(t)
	root, err := router.resolveNodePath(context.Background(), ".", true)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("本机正文"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	payload, err := router.scopedReadFile(context.Background(), `{"path":"plain.txt"}`)
	if err != nil {
		t.Fatalf("本地读失败：%v", err)
	}
	if payload != "本机正文" {
		t.Fatalf("本地读应逐字返回正文（历史契约），got %q", payload)
	}
}

// ── 2) write_file：远端面 ───────────────────────────────────────────

func TestRemoteWriteFileCarriesContentAsDataField(t *testing.T) {
	router := remoteFaceRouter(t)
	// 内容里放上 shell 会解释的东西：它作为**字段**送出，工具层不做任何拼接。
	content := "第一行 $(rm -rf /tmp/none)\n`echo hi` \"引用\"\n"
	remotePath := "/srv/中文/配置.yaml"
	args := `{"path":` + jsonString(t, remotePath) + `,"content":` + jsonString(t, content) + `}`

	payload, err := router.scopedWriteFile(sshContext(&fakeTransport{}, "proj-1", "deploy.internal"), args)
	if err != nil {
		t.Fatalf("远端写失败：%v", err)
	}
	fields := decodeReceipt(t, payload)
	requireFace(t, fields, "ssh", "deploy.internal")
	if size, _ := fields["size"].(float64); int(size) != len(content) {
		t.Fatalf("size = %v, want %d", fields["size"], len(content))
	}

	fake := &fakeTransport{}
	if _, err := router.scopedWriteFile(sshContext(fake, "proj-1", "ssh:22"), args); err != nil {
		t.Fatalf("远端写失败：%v", err)
	}
	writes := fake.writeCalls()
	if len(writes) != 1 {
		t.Fatalf("写调用次数 = %d, want 1", len(writes))
	}
	if writes[0].Path != remotePath || writes[0].Content != content {
		t.Fatalf("远端实际收到 path=%q content=%q", writes[0].Path, writes[0].Content)
	}
}

// ── 3) edit_file：远端面 ────────────────────────────────────────────

func TestRemoteEditFileReportsReplacements(t *testing.T) {
	fake := &fakeTransport{editOut: TransportEditOutput{Replacements: 3}}
	router := remoteFaceRouter(t)
	args := `{"path":"/srv/app/main.go","old_string":"旧","new_string":"新"}`

	payload, err := router.scopedEditFile(sshContext(fake, "proj-1", "deploy.internal"), args)
	if err != nil {
		t.Fatalf("远端改失败：%v", err)
	}
	fields := decodeReceipt(t, payload)
	requireFace(t, fields, "ssh", "deploy.internal")
	if count, _ := fields["replacements"].(float64); int(count) != 3 {
		t.Fatalf("replacements = %v, want 3", fields["replacements"])
	}
	edits := fake.editCalls()
	if len(edits) != 1 || edits[0].OldString != "旧" || edits[0].NewString != "新" {
		t.Fatalf("远端实际收到 %+v", edits)
	}
}

// ── 4) bash：远端面（命令 + stdin 数据；三种结局分开）────────────────

func TestRemoteBashSendsCommandAndStdinSeparatelyAndKeepsWorkdir(t *testing.T) {
	fake := &fakeTransport{execOut: TransportOutput{
		Stdout: "远端输出\n", Stderr: "", ExitCode: 7, Status: "completed",
	}}
	router := remoteFaceRouter(t)
	args := `{"command":` + jsonString(t, "echo 中文\nexit 7") + `,"stdin":` + jsonString(t, "喂进去的数据\n第二行") +
		`,"workdir":` + jsonString(t, "~/项目 目录") + `,"timeout":30}`

	payload, err := router.scopedBash(sshContext(fake, "proj-1", "deploy.internal"), args)
	if err != nil {
		t.Fatalf("远端执行失败：%v", err)
	}
	fields := decodeReceipt(t, payload)
	requireFace(t, fields, "ssh", "deploy.internal")
	if code, _ := fields["exit_code"].(float64); int(code) != 7 {
		t.Fatalf("exit_code = %v, want 7（远端非零退出仍是「跑完了」）", fields["exit_code"])
	}
	if running, _ := fields["still_running"].(bool); running {
		t.Fatalf("跑完了不该报 still_running：%v", fields)
	}

	calls := fake.execCalls()
	if len(calls) != 1 {
		t.Fatalf("执行调用次数 = %d, want 1", len(calls))
	}
	if calls[0].Command != "echo 中文\nexit 7" {
		t.Fatalf("命令正文被改写了：%q", calls[0].Command)
	}
	// ②：数据是独立字段，不是拼进命令正文的一部分。
	if calls[0].Stdin != "喂进去的数据\n第二行" {
		t.Fatalf("stdin 数据没有单独送出：%q", calls[0].Stdin)
	}
	// ③：workdir 原样交给远端（没有被解析成本机的绝对路径）。
	if calls[0].Workdir != "~/项目 目录" {
		t.Fatalf("workdir 被改写了：%q", calls[0].Workdir)
	}
	if calls[0].Timeout <= 0 {
		t.Fatalf("等待预算没有传给远端：%v", calls[0].Timeout)
	}
}

func TestRemoteBashSeparatesTimeoutTruncationAndStillRunning(t *testing.T) {
	cases := []struct {
		name           string
		out            TransportOutput
		wantStill      bool
		wantTruncated  bool
		wantStatus     string
		wantHandleKept bool
	}{
		{
			name: "预算用尽但远端仍在跑",
			out: TransportOutput{
				Status: "channel_timeout", StillRunning: true, ExitCode: -1,
				Handle: "ssh:ZmFrZQ", LogPath: "/tmp/seelex-job-1/out",
				Stdout: "到目前为止\n", Note: "远端作业仍在运行",
			},
			wantStill: true, wantStatus: "channel_timeout", wantHandleKept: true,
		},
		{
			name: "输出被截断（命令跑完了）",
			out: TransportOutput{
				Status: "completed", ExitCode: 0, Truncated: true, Stdout: "前 256KB",
			},
			wantTruncated: true, wantStatus: "completed",
		},
		{
			name:       "干净完成",
			out:        TransportOutput{Status: "completed", ExitCode: 0, Stdout: "ok"},
			wantStatus: "completed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTransport{execOut: tc.out}
			router := remoteFaceRouter(t)
			payload, err := router.scopedBash(sshContext(fake, "proj-1", "deploy.internal"), `{"command":"sleep 999"}`)
			if err != nil {
				t.Fatalf("执行失败：%v", err)
			}
			fields := decodeReceipt(t, payload)
			requireFace(t, fields, "ssh", "deploy.internal")
			if got, _ := fields["still_running"].(bool); got != tc.wantStill {
				t.Fatalf("still_running = %v, want %v", fields["still_running"], tc.wantStill)
			}
			if got, _ := fields["truncated"].(bool); got != tc.wantTruncated {
				t.Fatalf("truncated = %v, want %v", fields["truncated"], tc.wantTruncated)
			}
			if fields["status"] != tc.wantStatus {
				t.Fatalf("status = %v, want %q", fields["status"], tc.wantStatus)
			}
			if tc.wantHandleKept {
				if fields["handle"] == nil || fields["log_path"] == nil {
					t.Fatalf("仍在跑必须给 handle + log_path：%v", fields)
				}
			}
		})
	}
}

func TestLocalBashReceiptMarksSurface(t *testing.T) {
	router := remoteFaceRouter(t)
	payload, err := router.scopedBash(context.Background(), `{"command":"echo hello-face","timeout":20}`)
	if err != nil {
		t.Fatalf("本地执行失败：%v", err)
	}
	fields := decodeReceipt(t, payload)
	requireFace(t, fields, "local", "local")
	if !strings.Contains(fields["stdout"].(string), "hello-face") {
		t.Fatalf("stdout = %v", fields["stdout"])
	}
}

// ── 5) bg_manage：远端面 ────────────────────────────────────────────

func TestRemoteBashBackgroundUsesRemoteStartAndKeepsReceiptShape(t *testing.T) {
	fake := &fakeTransport{
		name: "ssh-bg",
		startOut: TransportManageOutput{
			Handle: "ssh:Li4vc3J2L2FwcA", LogPath: "/tmp/seelex-job/out",
		},
	}
	router := remoteFaceRouter(t)
	payload, err := router.scopedBashBg(sshContext(fake, "project-1", "test"),
		`{"command":"npm run dev","description":"启动测试服务","workdir":"/srv/app"}`)
	if err != nil {
		t.Fatalf("scopedBashBg: %v", err)
	}
	fields := decodeReceipt(t, payload)
	requireFace(t, fields, "ssh", "test")
	if fields["status"] != "accepted" || fields["handle"] != "ssh:Li4vc3J2L2FwcA" ||
		fields["log_path"] != "/tmp/seelex-job/out" {
		t.Fatalf("remote bash_bg receipt = %v", fields)
	}
	calls := fake.startCalls()
	if len(calls) != 1 || calls[0].Command != "npm run dev" || calls[0].Workdir != "/srv/app" {
		t.Fatalf("remote Start calls = %+v", calls)
	}
	if len(fake.execCalls()) != 0 {
		t.Fatal("background dispatch must not wait through Exec")
	}
}

func TestRemoteBgManageRoutesRemoteHandles(t *testing.T) {
	fake := &fakeTransport{manageOut: TransportManageOutput{
		State: "running", StillRunning: true, LogPath: "/tmp/seelex-job-1/out",
		Stdout: "增量\n", Note: "远端面取回的是当前落盘日志（不是增量）",
	}}
	router := remoteFaceRouter(t)
	payload, err := router.scopedJobManage(sshContext(fake, "proj-1", "deploy.internal"),
		`{"op":"fetch","handle":"ssh:ZmFrZS1kaXI"}`)
	if err != nil {
		t.Fatalf("远端取回失败：%v", err)
	}
	fields := decodeReceipt(t, payload)
	requireFace(t, fields, "ssh", "deploy.internal")
	if fields["state"] != "running" || fields["stdout"] != "增量\n" {
		t.Fatalf("回执 = %v", fields)
	}
	manages := fake.manageCalls()
	if len(manages) != 1 || manages[0].Op != "fetch" || manages[0].Handle != "ssh:ZmFrZS1kaXI" {
		t.Fatalf("远端实际收到 %+v", manages)
	}
}

func TestJobHandlesDoNotCrossSurfaces(t *testing.T) {
	router := remoteFaceRouter(t)
	// 本机句柄在本地面上照旧（只是查不到这条作业）。
	if _, err := router.scopedJobManage(context.Background(), `{"op":"observe","handle":"bt-7"}`); err == nil {
		t.Fatal("不存在的本机作业应报错")
	}
	// 远端句柄落到本地面：句柄表互不相认，必须给可照做的错误。
	_, err := router.scopedJobManage(context.Background(), `{"op":"observe","handle":"ssh:ZmFrZQ"}`)
	if err == nil || !strings.Contains(err.Error(), "bt-") {
		t.Fatalf("远端句柄交给本地面应显式报错（提示本表句柄形态），err=%v", err)
	}
}

// ── 6) 路径门：显式指名 ssh 的路径属对端命名空间 ──────────────────────

func TestPathGateLetsExplicitSSHPathsThrough(t *testing.T) {
	root := t.TempDir()
	scope := security.NewProjectScope()
	if err := scope.BindFor("sess-face", root); err != nil {
		t.Fatalf("bind scope: %v", err)
	}
	gated := PathGate(Deps{
		ProjectScope: scope,
		SessionKey:   func(context.Context) string { return "sess-face" },
	})
	ran := 0
	handler := gated(ReadFileToolName, frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
			ran++
			return "ok", nil
		}))
	// 远端绝对路径：本地根之外。
	remotePath := filepath.Join(string(filepath.Separator), "etc", "nginx", "nginx.conf")
	args := `{"path":` + jsonString(t, remotePath) + `}`

	// 未指名：仍按本地根判（越界拒绝）——这是本地面该有的行为。
	if _, err := handler.Execute(context.Background(), args); err == nil {
		t.Fatal("未指名远端时，本地根之外的路径应被路径门拒绝")
	}
	if ran != 0 {
		t.Fatalf("被拒的调用不得进入 handler：ran=%d", ran)
	}
	// 显式指名 ssh：例外放行（远端路径的包含关系由对端权限面负责）。
	if _, err := handler.Execute(WithExplicitTransport(context.Background(), TransportSSH), args); err != nil {
		t.Fatalf("显式指名 ssh 的远端路径应放行，err=%v", err)
	}
	if ran != 1 {
		t.Fatalf("放行的调用应进入 handler：ran=%d", ran)
	}
	// 入参声明 transport=ssh 同样算显式指名。
	if _, err := handler.Execute(context.Background(),
		`{"path":`+jsonString(t, remotePath)+`,"transport":"ssh"}`); err != nil {
		t.Fatalf("入参指名 ssh 的远端路径应放行，err=%v", err)
	}
	if ran != 2 {
		t.Fatalf("入参指名的调用应进入 handler：ran=%d", ran)
	}
}
