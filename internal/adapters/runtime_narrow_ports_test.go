package adapters

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 窄可选端口的**闸门**：application/core 用类型断言探测的每一项能力，本包的
// RuntimePort 都必须真的转发（否则断言静默 false，功能整块消失且无任何信号）。
//
// 为什么要扫源码而不是只写 `var _` 断言：`var _` 只保证"我列出来的还在"，挡不住
// "core 里新加了一个断言、adapter 没跟上"——而那正是 2026-10-02 这两处漏接的成因
// （TeamworkBoardSnapshot / SessionContextStoreFor）。本用例把**探测点**当事实源：
// application/core 里每出现一个新的窄端口断言，这里就红，并要求补转发 + 补断言。

// narrowPortForwarded 是**已接线**的窄可选端口清单：探测点标识 → 该方法集里的方法名。
//
// 标识取断言窗口里出现的跨包命名接口，或（内联接口/包内命名接口）第一个方法名。
// 下表的 8 个标识 = 2026-10-02 实测扫出的全部探测点（`go test -run
// TestRuntimePortForwardsEveryNarrowPort -v` 会逐个打印）。新增窄端口时两件事必须
// 同时做：runtime_narrow_ports.go 补转发方法 + 这里补一行。
var narrowPortForwarded = map[string]string{
	// contract.TeamworkBoardProjection（application/core/teamwork_service.go）。
	// **本轮补**：漏了它 = 团队看板投影恒 nil = 面板整块退场。
	"contract.TeamworkBoardProjection": "TeamworkBoardSnapshot",
	// contract.TeammateSessionProjection（application/core/teamwork_service.go）。
	// **本轮补**：漏了它 = "查看这件事的会话"只能读会话库，而 teammate 的一轮活是
	// 进程内执行面、正文不落盘 → 读出来是主会话的历史（用户报"全是历史会话"）。
	"contract.TeammateSessionProjection": "TeammateSessionLive",
	// context_runtime.CompactionIndexPort（service_assembler.go）。
	"context_runtime.CompactionIndexPort": "PushCompactionFrame",
	// goal 第五栈的会话上下文存储取用面（service_assembler.go）。
	// **本轮补**：漏了它 = goal 不落盘、board_goal.json 从不写。
	"SessionContextStoreFor": "SessionContextStoreFor",
	// persistedPlanRestorer（session_history.go，包内命名接口）。
	"RestorePlan": "RestorePlan",
	// fork 在飞闸门（service_input.go 两处）。
	"ForkInFlight": "ForkInFlight",
	// 冻结会话执行（session_scope.go）。
	"PerSessionExecution": "PerSessionExecution",
	// 按会话 replan 指标（view_state/coordinator.go）。**本轮补**。
	"ReplanMetricsFor": "ReplanMetricsFor",
	// 按会话任务快照（work_table.go）。
	"TaskSnapshotFor": "TaskSnapshotFor",
	// 按轮插件装配（service_scheduler.go，定时任务的插件装配）。
	// 漏了它 = 任务照跑但用的是宿主全局激活插件（面板却显示装配了 X）。
	"WithPluginAssembly": "WithPluginAssembly",
	// 档位 → wire 思考强度下发（session_scope.go 的 syncSessionReasoningEffort）。
	// 漏了它 = 切档只换提示词与 loop 次数，模型那边照旧（界面上档位换了、行为没换）。
	"contract.ReasoningEffortPort": "SetSessionReasoningEffort",
}

// runtimePortAssertionRe 命中一次 "….Runtime.(" 形态的窄端口探测。
var runtimePortAssertionRe = regexp.MustCompile(`[.\w]*Runtime\.\(`)

// runtimePortMethodRe 收集候选标识：方法名（大写开头 + "("）与点分类型名。
var (
	candidateMethodRe = regexp.MustCompile(`\b([A-Z]\w*)\s*\(`)
	// namedPortTypeRe 只认"端口身份"来自这些包的命名接口：其余点分类型是载荷。
	namedPortTypeRe = regexp.MustCompile(`\b((?:contract|context_runtime|seelebridge|seelexctx)\.[A-Z]\w*)\b`)
)

// scanRoot 是探测点来源（core 及其子协调器都在 application/core 下）。
const scanRoot = "../../application/core"

// TestRuntimePortForwardsEveryNarrowPort 是本闸门的实现。
func TestRuntimePortForwardsEveryNarrowPort(t *testing.T) {
	sites := scanRuntimePortAssertions(t)
	if len(sites) == 0 {
		t.Fatal("没有扫到任何窄端口探测点——扫描口径失效（目录/写法改名？）")
	}
	for _, site := range sites {
		t.Logf("探测点 %s", site)
	}
	var unknown []string
	for _, site := range sites {
		if _, declared := narrowPortForwarded[site.key]; !declared {
			unknown = append(unknown, site.String())
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		t.Fatalf("application/core 里出现未接线的窄可选端口探测点（断言在 RuntimePort 上恒 false，功能会整块消失）：\n  %s\n\n"+
			"修法：internal/adapters/runtime_narrow_ports.go 补转发方法 + var _ 断言，并把标识加进本文件的 narrowPortForwarded。",
			strings.Join(unknown, "\n  "))
	}
}

// TestNarrowPortForwardingListIsReal 钉住清单本身不是凭想象写的：清单里每个方法
// 都必须真的存在于 RuntimePort 上（`var _` 断言把这条变成编译期事实，这里让它在
// 测试输出里也可查）。
func TestNarrowPortForwardingListIsReal(t *testing.T) {
	portType := reflect.TypeOf(RuntimePort{})
	for key, method := range narrowPortForwarded {
		if _, ok := portType.MethodByName(method); !ok {
			t.Fatalf("清单声称 %q 由 RuntimePort.%s 承载，但方法不存在", key, method)
		}
	}
}

type assertionSite struct {
	file string
	line int
	key  string
}

func (site assertionSite) String() string {
	return site.file + ":" + itoa(site.line) + " → " + site.key
}

// scanRuntimePortAssertions 扫 application/core 的非测试源码，收集窄端口探测点并
// 归一出"这个探测点要的是哪一项能力"。
//
// 归一顺序（先命名类型、后方法名）：命名接口（contract.X / context_runtime.Y /
// core 内的持久化类型）直接以类型名作标识；内联接口取窗口里第一个出现的方法名——
// 探测点写成一行还是拆成多行都能收住。
func scanRuntimePortAssertions(t *testing.T) []assertionSite {
	t.Helper()
	var sites []assertionSite
	err := filepath.Walk(scanRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lines := strings.Split(string(data), "\n")
		for index, line := range lines {
			loc := runtimePortAssertionRe.FindStringIndex(line)
			if loc == nil {
				continue
			}
			end := index + 8
			if end > len(lines) {
				end = len(lines)
			}
			// 窗口从断言运算符之后开始：`.Runtime.(` 本身不属于断言内容，
			// 留在窗口里会把 `deps.Runtime` 误当成候选类型名。
			restOfLine := line[loc[1]:]
			window := strings.Join(append([]string{restOfLine}, lines[index+1:end]...), "\n")
			sites = append(sites, assertionSite{
				file: filepath.ToSlash(path), line: index + 1, key: narrowPortKey(window),
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫 %s: %v", scanRoot, err)
	}
	return sites
}

// narrowPortKey 把断言窗口归一成一个标识。
//
// 优先取"命名接口"（跨包类型名，如 contract.TeamworkBoardProjection）；否则取窗口里
// 第一个方法名（内联接口与包内命名接口都落这一支）。不取参数/返回值的点分类型
// （如 *sessionstore.SessionContextStore）——那是载荷类型，不是端口身份。
func narrowPortKey(window string) string {
	if match := namedPortTypeRe.FindStringSubmatch(window); match != nil {
		return match[1]
	}
	if match := candidateMethodRe.FindStringSubmatch(window); match != nil {
		return match[1]
	}
	return strings.TrimSpace(window)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
