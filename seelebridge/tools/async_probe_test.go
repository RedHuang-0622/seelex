package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode"
)

// 实时探针的判据（台账 §10 S3/探针）。
//
// 探针要回答的是界面上那三个问题：**这条后台命令是什么（描述）、在跑什么（指令）、
// 现在怎么样（字节数 + 末行 + 死活）**。它不推进游标、不进上下文，所以允许带命令行
// 原文、绝对路径与时间；进模型的那份载荷仍受确定性纪律约束（async_exec.go）。

// writeLogForTest 往某次执行的输出文件写内容，模拟命令已经在产出。
func writeLogForTest(t *testing.T, path, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProbeReadsDescriptionCommandBytesAndTail(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, started, err := registry.begin("sess-p", "make -j8 build", "编译整个仓库", "req-7")
	if err != nil || !started {
		t.Fatalf("begin: started=%v err=%v", started, err)
	}
	writeLogForTest(t, run.logPath, "go: downloading x\nmake: Entering directory 'G:/p'\r\n")

	infos := registry.infos()
	if len(infos) != 1 {
		t.Fatalf("探针读数条数 = %d，want 1", len(infos))
	}
	info := infos[0]
	if info.Description != "编译整个仓库" || info.Command != "make -j8 build" {
		t.Fatalf("描述/指令没进探针: %+v", info)
	}
	if info.SessionID != "sess-p" || info.BatchID != "req-7" {
		t.Fatalf("归属/批次没进探针: %+v", info)
	}
	if info.State != asyncStateRunning || info.Exit != -1 {
		t.Fatalf("在跑的执行必须报 running 且退出码为 -1: state=%q exit=%d", info.State, info.Exit)
	}
	if info.LogBytes != int64(len("go: downloading x\nmake: Entering directory 'G:/p'\r\n")) {
		t.Fatalf("LogBytes = %d", info.LogBytes)
	}
	if info.Tail != `make: Entering directory 'G:/p'` {
		t.Fatalf("Tail = %q，want 末个非空行", info.Tail)
	}
	if info.LastByteAt.IsZero() {
		t.Fatal("LastByteAt 为空：界面无法显示最后一次产出时刻")
	}
	if info.LogPath != run.logPath {
		t.Fatalf("LogPath = %q", info.LogPath)
	}
}

// 末行原样进 DOM 之前必须压成一行、去掉控制字符并限长：进度条类命令一行能到几 KB。
func TestProbeTailIsOneLineSanitizedAndClamped(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-p", "progress", "带进度条的下载", "")
	if err != nil {
		t.Fatal(err)
	}
	noisy := "\x1b[2K" + strings.Repeat("█", asyncProbeTailRunes+80) + "\n"
	writeLogForTest(t, run.logPath, noisy)

	tail := registry.infos()[0].Tail
	if len([]rune(tail)) > asyncProbeTailRunes+1 { // +1 = 省略号
		t.Fatalf("末行没限长: %d runes", len([]rune(tail)))
	}
	for _, r := range tail {
		if unicode.IsControl(r) {
			t.Fatalf("末行残留控制字符 %q：%q", r, tail)
		}
	}
	if !strings.HasSuffix(tail, "…") {
		t.Fatalf("超长末行应带省略号: %q", tail)
	}
}

// 句柄是 "a<seq>"：字符串序会把 a10 排到 a2 前，界面上就成了乱序的派发史。
func TestProbeOrdersByDispatchSequenceNotHandleText(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	for index := 1; index <= 12; index++ {
		if _, _, err := registry.begin("sess-p", fmt.Sprintf("echo %d", index), "", ""); err != nil {
			t.Fatal(err)
		}
	}
	infos := registry.infos()
	if len(infos) != 12 {
		t.Fatalf("读数条数 = %d", len(infos))
	}
	for index, info := range infos {
		if want := fmt.Sprintf("a%d", index+1); info.Handle != want {
			t.Fatalf("第 %d 条句柄 = %s，want %s（必须按派发序）", index, info.Handle, want)
		}
	}
}

type stubTreeForProbe struct{ degraded bool }

func (s stubTreeForProbe) Terminate() error { return nil }
func (s stubTreeForProbe) Close()           {}
func (s stubTreeForProbe) Degraded() bool   { return s.degraded }

// 树挂不上时"杀"只能打到直接子进程：探针必须把这条差别带到界面上，不能让用户以为
// 杀干净了。
func TestProbeReportsDegradedProcessTree(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-p", "sleep 5", "睡一会儿", "")
	if err != nil {
		t.Fatal(err)
	}
	if registry.infos()[0].Degraded {
		t.Fatal("还没挂树就报 degraded")
	}
	registry.attach(run.handle, stubTreeForProbe{degraded: true})
	if !registry.infos()[0].Degraded {
		t.Fatal("降级的进程树没被探针如实报出")
	}
	registry.attach(run.handle, stubTreeForProbe{})
	if registry.infos()[0].Degraded {
		t.Fatal("换回可用树后仍报 degraded")
	}
	// 收尾由登记表负责：留一条在跑的执行体会让 close 删不掉目录。
	registry.finish(run.handle, 0)
}

// 去抖：短时间内的连续写入只发一次信号；过了节流窗口还在产出才再发。
// 信号频率被压住 ≠ 状态被猜——终态一律由 cmd.Wait 的返回说话。
func TestOutputSignalsAreDebounced(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-p", "yes", "持续产出", "")
	if err != nil {
		t.Fatal(err)
	}
	drainSignal(registry) // 吃掉 begin 自己那次

	// 第一次"有新字节"必须立刻发信号（否则界面要空等一整个窗口），之后的都要被压住。
	registry.noteOutput(run.handle)
	if !waitSignal(registry, 200*time.Millisecond) {
		t.Fatal("首次新字节没有发信号：实时查看要空等节流窗口")
	}
	for index := 0; index < 199; index++ {
		registry.noteOutput(run.handle)
	}
	if waitSignal(registry, 60*time.Millisecond) {
		t.Fatal("节流窗口内的连续写入发出了多于一次信号")
	}

	time.Sleep(asyncProbeThrottle + 50*time.Millisecond)
	registry.noteOutput(run.handle)
	if !waitSignal(registry, 200*time.Millisecond) {
		t.Fatal("越过节流窗口后的新输出没有再发信号：实时查看就成了摆设")
	}

	// 终态之后不再有"新字节"可言，noteOutput 不得再吵投影。
	registry.finish(run.handle, 0)
	drainSignal(registry)
	registry.noteOutput(run.handle)
	if waitSignal(registry, 60*time.Millisecond) {
		t.Fatal("已终态的执行还在发新字节信号")
	}
}

// 派发经过工具面时，描述与批次必须真的落到登记表；缺描述要当场拒绝，
// 而不是让工作表格冒出一行没有标题的后台行。
func TestBackgroundDispatchCarriesDescriptionAndBatch(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-desc")

	args, err := json.Marshal(map[string]interface{}{
		"command": "sleep 30", "description": "跑一整轮集成测试",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.scopedBashBg(ctx, string(args)); err != nil {
		t.Fatalf("带描述的派发被拒: %v", err)
	}
	infos := router.AsyncRuns()
	if len(infos) != 1 {
		t.Fatalf("投影读数条数 = %d", len(infos))
	}
	if infos[0].Description != "跑一整轮集成测试" {
		t.Fatalf("描述没落到登记表: %+v", infos[0])
	}
	if infos[0].BatchID != "req-test" {
		t.Fatalf("批次没落到登记表: %q", infos[0].BatchID)
	}
	if infos[0].Command != "sleep 30" {
		t.Fatalf("指令没落到登记表: %q", infos[0].Command)
	}
	router.CloseSessionAsync("sess-desc")

	missing, err := json.Marshal(map[string]interface{}{"command": "sleep 1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.scopedBashBg(ctx, string(missing)); err == nil ||
		!strings.Contains(err.Error(), "description") {
		t.Fatalf("bash_bg 缺描述必须被拒，实得 err=%v", err)
	}
	// 同步执行不受影响：description 只是作业派的必填项。
	syncArgs, _ := json.Marshal(map[string]interface{}{"command": "echo hi"})
	if _, err := router.scopedBash(ctx, string(syncArgs)); err != nil {
		t.Fatalf("同步 bash 被描述要求误伤: %v", err)
	}
}

// 能力关闭时既没有行也没有句柄可看（不得留一个空壳工具面）。
func TestProbeIsEmptyWhenCapabilityOff(t *testing.T) {
	router := asyncTestRouter(t, false)
	ctx := asyncTestCtx(t.TempDir(), "sess-off")
	if got := router.AsyncRuns(); got != nil {
		t.Fatalf("能力未开却有投影行: %+v", got)
	}
	args, _ := json.Marshal(map[string]interface{}{"command": "echo hi", "description": "x"})
	if _, err := router.scopedBashBg(ctx, string(args)); err == nil {
		t.Fatal("能力未开时 bash_bg 必须报错")
	}
}

func waitSignal(registry *asyncRegistry, within time.Duration) bool {
	timer := time.NewTimer(within)
	defer timer.Stop()
	select {
	case <-registry.Events():
		return true
	case <-timer.C:
		return false
	}
}

func drainSignal(registry *asyncRegistry) {
	for {
		select {
		case <-registry.Events():
			continue
		default:
			return
		}
	}
}
