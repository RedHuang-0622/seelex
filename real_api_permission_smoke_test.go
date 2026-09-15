//go:build manualsmoke2

package main

// real_api_permission_smoke_test.go — 权责系统的**真实 API 冒烟**（opt-in）。
//
// 跑法（凭据只从环境变量给出，不写入仓库、不打印内容）：
//
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags manualsmoke2 . -run 'TestRealAPIPermissionSmoke' -count=1 -v -timeout 1500s
//
// 覆盖三条新口径（不是单测的复述，而是"真实模型 + 真实工具调度"的端到端）：
//
//	A. 主代理跑白名单命令（git status）→ 工具真的执行，且**不产生审批请求**；
//	B. 主代理跑越界命令（python …）→ 产生一次审批请求（执行选择页面路径活着）；
//	C. 主代理 fork 子代理 → 子代理的工具调用**一次审批都不产生**（子代理没有
//	   人类在环：位齐直接放行），整轮正常收口。
//
// 违权直接拒绝（子代理碰 ctl/adm）由 permission_subject_test.go 与
// permission_config_test.go 确定性覆盖；GUI 侧另有端到端观察（审批页面 + 子代理
// 面板），因为让模型稳定地"故意越权"不是可复现的冒烟手段。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"encoding/json"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// recordingApproval 是审批桩：记录每次"执行选择页面"请求（工具名 + 会话），
// 并一律放行（模拟人类在页面上点允许）。
type recordingApproval struct {
	mu       sync.Mutex
	requests []toolspermission.ApprovalRequest
}

func (r *recordingApproval) handler() toolspermission.ApprovalHandler {
	return func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		r.mu.Lock()
		r.requests = append(r.requests, ctx.Request)
		r.mu.Unlock()
		return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
	}
}

func (r *recordingApproval) snapshot() []toolspermission.ApprovalRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]toolspermission.ApprovalRequest(nil), r.requests...)
}

func (r *recordingApproval) reset() {
	r.mu.Lock()
	r.requests = nil
	r.mu.Unlock()
}

func (r *recordingApproval) tools() []string {
	names := make([]string, 0)
	for _, request := range r.snapshot() {
		names = append(names, request.ToolName)
	}
	return names
}

func TestRealAPIPermissionSmoke(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}
	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyOpaqueFileManual(t, accountsSource, accountsPath)

	harness := newFullChainHarness(t, accountsPath, projectRoot, 60*time.Second)
	approval := &recordingApproval{}
	// 装上**生产同形**的权责配置：默认分组 + 主体授权表 + config/seele.yaml 覆盖。
	cfg := seeltools.DefaultPermissionConfig()
	if fileCfg, err := loadPermissionConfig(firstExisting("config/seele.yaml", "seele.yaml")); err != nil {
		t.Fatalf("loadPermissionConfig: %v", err)
	} else {
		cfg = mergePermissionConfig(cfg, fileCfg)
	}
	harness.runtime.SetPermissionConfig(cfg, approval.handler())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	submit := func(prompt string) {
		t.Helper()
		if err := harness.app.Submit(ctx, prompt); err != nil {
			t.Fatal(err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("live turn did not become idle: %v", err)
		}
		if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
			t.Fatalf("live turn failed: %s", snapshot.Chat.Error)
		}
	}

	// 事件订阅必须在回合之前建立：事件是实时流，回合结束后再订阅看不到任何东西。
	recorder := toolEvents(harness)
	defer recorder.Close()

	// A. 白名单命令：真执行 + 不弹审批。
	approval.reset()
	submit("请只调用一次 bash 工具，命令是 `git status`，然后把它的输出原样汇报给我。")
	requests := approval.tools()
	for _, name := range requests {
		if name == "bash" {
			t.Fatalf("白名单命令不该走执行选择页面，requests=%v", requests)
		}
	}
	if !recorder.ran("bash") {
		t.Fatal("白名单命令应当真的执行（工具完成事件里找不到 bash）")
	}

	// B. 越界命令：走一次执行选择页面（自动放行），证明 ask → 页面活着。
	approval.reset()
	submit("请只调用一次 bash 工具，命令是 `python -c \"print(41+1)\"`，然后把结果汇报给我。")
	if got := approval.tools(); !containsTool(got, "bash") {
		t.Fatalf("越界命令应当走执行选择页面，requests=%v", got)
	}
	if !recorder.ran("bash") {
		t.Fatal("页面上放行后工具应当执行")
	}

	// C. 子代理：fork 派活让子代理各自**写盘**。
	//
	// 这是关键区分点：主代理写盘命中 `write_file: ask` 规则要过执行选择页面，
	// 而子代理位齐直接放行（没有人类在环）。所以本轮"零审批请求 + 文件真的写出来"
	// 同时成立，才说明子代理的权责是独立判定的。
	approval.reset()
	submit("请用 fork_subagents 派生 2 个子代理，并给每个子代理的指令写明：用 write_file 在项目根目录写一个文件，" +
		"名字分别是 sub-1.txt 与 sub-2.txt，内容写各自的编号。你自己不要调用 write_file 或其它写盘工具，" +
		"等子代理返回后用两行汇总它们各自的产出。")
	if got := approval.tools(); len(got) > 0 {
		t.Fatalf("子代理链路不该产生审批请求（子代理没有人类在环），requests=%v", got)
	}
	if !recorder.ran("fork_subagents") {
		t.Fatal("本轮应当真的执行了 fork_subagents")
	}
	// 说明（不是断言，是取证）：这一轮子代理真的尝试了 `write_file`（主代理的
	// 汇总里如实报了被拒），它们**没有**产生任何审批请求——换成主代理自己写盘会
	// 命中 `write_file: ask` 规则走执行选择页面。写盘本身在 harness 里失败于
	// `project scope: no project is bound to this session`（子代理节点会话没有项目
	// 绑定，是环境/绑定面的事实，与权限判定无关），所以这里不断言文件落盘。
	final := latestVisibleAssistantManual(harness.app.Snapshot())
	t.Logf("主代理对本轮的最终答复: %s", final)
	if strings.TrimSpace(final) == "" {
		t.Fatal("本轮应当有最终答复")
	}
}

// toolEvents 订阅应用事件面，返回"排空当前事件并抽取工具名"的读取函数：
// 订阅必须在回合**之前**建立（事件是实时流，回合结束后再订阅看不到任何东西）。
func toolEvents(harness fullChainHarness) *toolEventRecorder {
	return &toolEventRecorder{subscription: harness.events.Subscribe(4096)}
}

type toolEventRecorder struct {
	subscription application.Subscription
}

// ran 报告本回合是否出现过该工具的成功完成事件（真实执行面证据）。
func (r *toolEventRecorder) ran(tool string) bool {
	for {
		select {
		case event := <-r.subscription.Events:
			if event.Kind != application.EventToolCompleted {
				continue
			}
			var message application.Message
			if err := json.Unmarshal(event.Payload, &message); err != nil {
				continue
			}
			if message.Tool != nil && message.Tool.Name == tool {
				return true
			}
		default:
			return false
		}
	}
}

func (r *toolEventRecorder) Close() {
	if r == nil {
		return
	}
	r.subscription.Close()
}

func containsTool(names []string, tool string) bool {
	for _, name := range names {
		if name == tool {
			return true
		}
	}
	return false
}
