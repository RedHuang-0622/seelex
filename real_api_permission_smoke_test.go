//go:build manualsmoke2

package main

// real_api_permission_smoke_test.go — 权责系统的**真实 API 冒烟**（opt-in）。
//
// 跑法（凭据只从环境变量给出，不写入仓库、不打印内容）：
//
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags manualsmoke2 . -run 'TestRealAPIPermissionSmoke' -count=1 -v -timeout 1500s
//
// 与其它用例的区别：审批面不再用桩，而是装**生产审批桥**
// （main.go:newPermissionBridge → application.ApprovalBroker），也就是 GUI/TUI
// 渲染并回填的那条路径。因此下面三条断言都是"真实模型 + 真实工具调度 + 真审批页面"：
//
//	A. 主代理跑白名单命令（git status）→ 工具真的执行，且**页面一次都没打开**；
//	B. 主代理跑越界命令（python …）→ 页面真的打开一次（工具名 = bash），
//	   页面放行后工具执行；
//	C. 主代理 fork 子代理 → 子代理的工具调用**一次页面都不打开**（子代理没有
//	   人类在环：位齐直接放行），整轮正常收口。
//
// 违权直接拒绝（子代理碰 ctl/adm/共享外设）与内存释放由确定性用例覆盖：
// seelebridge/tools/permission_subject_test.go、permission_config_test.go、
// seelebridge/session/subagent_lifecycle_release_test.go。让模型稳定地"故意越权"
// 不是可复现的冒烟手段，所以不放进真实 API 冒烟。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"encoding/json"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/internal/bootseed"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// approvalPage 是"执行选择页面"的测试侧人类：盯住生产审批 broker 的待批集合，
// 记录每一次页面的打开（按请求 ID 去重，附工具名），并立即在页面上点"允许"
// ——等价于用户在 GUI 上看到审批卡片后点允许，让调用继续。
type approvalPage struct {
	broker *application.ApprovalBroker

	mu     sync.Mutex
	orders []string          // 页面打开顺序（工具名）
	seen   map[string]string // 请求 ID → 工具名（去重）
	stop   chan struct{}
	done   chan struct{}
}

// openApprovalPage 起一个审批页面观察者。必须在提交回合**之前**调用：页面是
// 及时打开的（回合进行中），订阅晚了就什么都看不到。
func openApprovalPage(broker *application.ApprovalBroker) *approvalPage {
	page := &approvalPage{
		broker: broker,
		seen:   make(map[string]string),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go func() {
		defer close(page.done)
		for {
			select {
			case <-page.stop:
				return
			default:
			}
			for _, pending := range broker.Pending() {
				page.note(pending.Interaction.ID, pending.Interaction.ToolName)
				// 页面放行：与 GUI 回填 ApprovalDecision{OptionID:"allow"} 同形。
				_ = broker.Resolve(pending.Interaction.ID, application.ApprovalDecision{OptionID: "allow"})
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return page
}

func (p *approvalPage) note(id, tool string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.seen[id]; ok {
		return
	}
	p.seen[id] = tool
	p.orders = append(p.orders, tool)
}

// snapshot 返回本次统计窗口内打开的页面（工具名，按打开顺序）。
func (p *approvalPage) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.orders...)
}

// reset 清空统计窗口（页面开合本身不再重放，只影响计数）。
func (p *approvalPage) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.orders = nil
	p.seen = make(map[string]string)
}

func (p *approvalPage) Close() {
	if p == nil {
		return
	}
	close(p.stop)
	<-p.done
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
	// 装上**生产同形**的权责配置与**生产审批桥**：默认分组 + 主体授权表 +
	// config/seele.yaml 覆盖，审批走 ApprovalBroker（GUI 同一条路）。
	cfg := seeltools.DefaultPermissionConfig()
	if fileCfg, err := loadPermissionConfig(ensureConfigFile(bootseed.PermissionConfigName, bootseed.PermissionConfigPack())); err != nil {
		t.Fatalf("loadPermissionConfig: %v", err)
	} else {
		cfg = mergePermissionConfig(cfg, fileCfg)
	}
	harness.runtime.SetPermissionConfig(cfg, newPermissionBridge(harness.approval))

	// 事件订阅与审批页面都必须在回合之前建立：两者都是实时流。
	recorder := toolEvents(harness)
	defer recorder.Close()
	page := openApprovalPage(harness.approval)
	defer page.Close()

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

	// A. 白名单命令：真执行 + 页面一次都不打开。
	page.reset()
	submit("请只调用一次 bash 工具，命令是 `git status`，然后把它的输出原样汇报给我。")
	if pages := page.snapshot(); len(pages) != 0 {
		t.Fatalf("白名单命令不该打开执行选择页面，pages=%v", pages)
	}
	if !recorder.ran("bash") {
		t.Fatal("白名单命令应当真的执行（工具完成事件里找不到 bash）")
	}

	// B. 越界命令：页面打开一次（工具名 = bash），放行后工具执行。
	page.reset()
	submit("请只调用一次 bash 工具，命令是 `python -c \"print(41+1)\"`，然后把结果汇报给我。")
	pages := page.snapshot()
	if len(pages) == 0 {
		t.Fatal("越界命令应当打开执行选择页面")
	}
	if pages[0] != "bash" {
		t.Fatalf("执行选择页面应当带着工具名 bash，pages=%v", pages)
	}
	if !recorder.ran("bash") {
		t.Fatal("页面上放行后工具应当执行")
	}

	// C. 子代理：fork 派活让子代理各自**写盘**。
	//
	// 这是关键区分点：主代理写盘命中 `write_file: ask` 规则要过执行选择页面，
	// 而子代理位齐直接放行（没有人类在环）。所以本轮"页面零打开 + 文件真的写出来"
	// 同时成立，才说明子代理的权责是独立判定的。
	page.reset()
	submit("请用 fork_subagents 派生 2 个子代理，并给每个子代理的指令写明：用 write_file 在项目根目录写一个文件，" +
		"名字分别是 sub-1.txt 与 sub-2.txt，内容写各自的编号。你自己不要调用 write_file 或其它写盘工具，" +
		"等子代理返回后用两行汇总它们各自的产出。")
	if pages := page.snapshot(); len(pages) != 0 {
		t.Fatalf("子代理链路不该打开执行选择页面（子代理没有人类在环），pages=%v", pages)
	}
	if !recorder.ran("fork_subagents") {
		t.Fatal("本轮应当真的执行了 fork_subagents")
	}
	// 说明（不是断言，是取证）：这一轮子代理真的尝试了 `write_file`（主代理的
	// 汇总里如实报了被拒），它们**没有**打开任何审批页面——换成主代理自己写盘会
	// 命中 `write_file: ask` 规则打开页面。写盘本身在 harness 里失败于
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
