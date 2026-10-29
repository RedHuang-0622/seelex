package registry

import (
	"bytes"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 后台命令的轮询型执行域·实时探针：把登记表里的一次执行采样成"能看懂的一眼"，
// 供工作表格投影（application/assembly/work_table_async.go）消费。
//
// 探针和取回（bg_manage(op=fetch)）是两回事，别混：
//   - fetch 推进游标、把**新增字节**交给模型，一次一问一答，花一轮往返；
//   - 探针不推进游标、不进上下文，只回答"此刻它在不在动、动到哪一行"，
//     由派发/终态/新字节三类信号驱动（见 asyncRegistry.Events / noteOutput）。
//
// 因此 AsyncRunInfo 允许带命令行原文、绝对路径和时间戳——它们只到 GUI。
// 进模型上下文的字节仍然必须是确定性的那份（asyncPayload），这条纪律不因探针松动。

// AsyncRunInfo 是一次后台执行的探针读数（投影输入；跨包只读）。
type AsyncRunInfo struct {
	Handle      string
	SessionID   string
	Description string         // 模型写的"这条命令在干什么"
	Command     string         // 命令行原文（探针侧不脱敏，GUI 才需要看得懂）
	State       dto.AsyncState // running | done | failed | killed
	Exit        int            // 终态退出码；running 时为 -1
	LogPath     string         // 输出文件（绝对路径，只上 GUI 的附件列）
	LogBytes    int64          // 已落盘字节数：真实进展信号，不是墙钟猜的
	LastByteAt  time.Time      // 输出文件最后修改时间（无输出则零值）
	Tail        string         // 末行采样（已压成单行、限长）
	Truncated   bool           // 输出是否已按上限截断
	Degraded    bool           // 进程树挂不上：终止只能打到直接子进程
	BatchID     string         // 派发它的那次 chat 请求（工作表格批次归属）
	StartedAt   time.Time
	EndedAt     time.Time // 零值 = 还没终态
	// Kind = process | inline | subagent（打点 K-2）：决定读取口与 kill 语义。
	Kind string
	// Summary 是终态**有界摘要**（≤512 字节；打点 K-5 的 K-2）：回填进投影的内容
	// 就是它。全文不入投影，只走取回或 log_path。
	Summary string
	// Lines 是输出行数；Notified 是"该作业的终态已回填过一次"（K-4 的幂等键）。
	Lines    int
	Notified bool
	// Index 是同批内 wire 下标（排序键，不是完成序）。
	Index int
}

// observeLine 是一条作业的**观察读数**（只读旁路：不推进游标、不消费输出）。
// 它把"此刻它在不在动、动到哪一行"压成一行——模型侧看的就是这一行。
func (g *asyncRegistry) observeLine(run asyncRun) string {
	bytes, _, tail := sampleLog(run.logPath)
	line := jobSummaryLine(run, bytes)
	if tail != "" && run.state == asyncStateRunning {
		line += " · 末行: " + clampProbeLine(tail)
	}
	if run.capHit {
		line += " · 输出已截断（全文见 log_path）"
	}
	if run.tree != nil && run.tree.Degraded() {
		line += " · 进程树挂不上，终止只及直接子进程"
	}
	return "- " + line
}

// observeSession 列某会话在册作业的观察读数（按派发序稳定排序）。
//
// 这是 V7 的落点：main agent"主动查看子代理/后台作业在干什么"不需要 OS 进程化，
// 只需要这份只读读数——句柄契约与执行体是 goroutine 还是进程正交。
func (g *asyncRegistry) observeSession(sessionID string) []string {
	infos := g.infos()
	lines := make([]string, 0, len(infos))
	for _, info := range infos {
		if sessionID != "" && info.SessionID != sessionID {
			continue
		}
		line := jobSummaryLine(asyncRun{
			handle: info.Handle, kind: info.Kind, state: info.State, exit: info.Exit,
			description: info.Description, command: info.Command, summary: info.Summary,
			lines: info.Lines, capHit: info.Truncated,
		}, info.LogBytes)
		if info.Tail != "" && info.State == asyncStateRunning {
			line += " · 末行: " + clampProbeLine(info.Tail)
		}
		lines = append(lines, "- "+line)
	}
	return lines
}

// AsyncRuns 返回全部后台执行的探针读数，按派发顺序稳定排序（句柄是 `<token>-<seq>`，
// 字符串序会把 a10 排到 a2 前）。能力未开时返回 nil：没开就没有行，也不该有行。
func (r *Router) AsyncRuns() []AsyncRunInfo {
	if !r.asyncEnabled() {
		return nil
	}
	return r.async.infos()
}

// AsyncRunEvents 返回后台执行表的变化信号口（派发/终态/驱逐/新字节）。
// 消费方必须自己汇聚（latest-wins），不要在信号里读表——那是把执行域按住在等消费者。
func (r *Router) AsyncRunEvents() <-chan struct{} {
	if r == nil || r.async == nil {
		return nil
	}
	return r.async.Events()
}

// AsyncOutputTail 返回某句柄输出文件末尾的有界窗口（详情弹窗的实时输出读口）。
//
// 三个"不"：不推进游标（取回语义只属于 bg_manage(op=fetch)）、不消费输出（同一段
// 正文可以被拉很多次）、不进模型上下文（正文只上 GUI）。句柄不在册时 Found=false。
// maxBytes 由调用方钳制（这里只兜 <=0 的无效值），窗口落在文件末尾。
func (r *Router) AsyncOutputTail(handle string, maxBytes int) dto.AsyncOutputTail {
	if r == nil || r.async == nil || handle == "" {
		return dto.AsyncOutputTail{}
	}
	return r.async.outputTail(handle, maxBytes)
}

// outputTail 是 AsyncOutputTail 在登记表上的实现：锁内只取日志路径，文件 I/O
// 一律在锁外（一条狂写日志的命令不得把派发/收尾按住）。
func (g *asyncRegistry) outputTail(handle string, maxBytes int) dto.AsyncOutputTail {
	g.mu.Lock()
	run, ok := g.runs[handle]
	logPath := ""
	if ok {
		logPath = run.logPath
	}
	g.mu.Unlock()
	if !ok {
		return dto.AsyncOutputTail{}
	}
	output, windowed := readLogTail(logPath, maxBytes)
	return dto.AsyncOutputTail{Found: true, Output: output, Windowed: windowed}
}

// readLogTail 读输出文件末尾的有界窗口，并按"给人读"的口径清洗：
//
//   - 窗口起点落在多字节字符中间时向后对齐到字符边界：半个 UTF-8 字符进了 DOM
//     就是乱码，而这些字节只服务渲染；
//   - 首行是残行（窗口从半行中间开始）时丢掉：残行的前半截已经不在窗口里，留着
//     只会让人以为那是完整的一行；
//   - 控制字符（含 ANSI 转义）清掉，保留换行与制表——进度条类输出里的 \r 与控制
//     序列会把界面咬花，但日志正文本身要能读。
func readLogTail(path string, maxBytes int) (string, bool) {
	if path == "" || maxBytes <= 0 {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return "", false
	}
	window := info.Size()
	windowed := false
	if window > int64(maxBytes) {
		window = int64(maxBytes)
		windowed = true
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	chunk := make([]byte, window)
	if _, err := file.ReadAt(chunk, info.Size()-window); err != nil {
		return "", false
	}
	if windowed {
		for len(chunk) > 0 && !utf8.RuneStart(chunk[0]) {
			chunk = chunk[1:]
		}
		if cut := bytes.IndexByte(chunk, '\n'); cut >= 0 {
			chunk = chunk[cut+1:]
		}
	}
	return sanitizeLogWindow(string(chunk)), windowed
}

// sanitizeLogWindow 清掉不适合直接进渲染的控制字符与 ANSI 转义序列。
//
// 保留 '\n'（多行正文就是靠它）与 '\t'（缩进）；'\r' 直接丢弃（Windows 换行与
// 进度条重绘都靠它，留着会把两行压成一行）。
func sanitizeLogWindow(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for index := 0; index < len(text); index++ {
		char := text[index]
		if char == 0x1b { // ESC：整段 CSI/OSC 序列都不进渲染
			index = skipANSISequence(text, index)
			continue
		}
		switch char {
		case '\n', '\t':
			builder.WriteByte(char)
		case '\r':
			// 丢弃
		default:
			if char < 0x20 || char == 0x7f {
				builder.WriteByte(' ')
				continue
			}
			builder.WriteByte(char)
		}
	}
	return builder.String()
}

// skipANSISequence 返回 ESC 之后应跳过的最后一个字节下标（调用方 index++ 续扫）。
// 认不出的形态只跳 ESC 自身，免得把正文一起吞掉。
func skipANSISequence(text string, esc int) int {
	if esc+1 >= len(text) {
		return esc
	}
	switch text[esc+1] {
	case '[': // CSI: ESC [ params final(0x40-0x7e)
		for index := esc + 2; index < len(text); index++ {
			if text[index] >= 0x40 && text[index] <= 0x7e {
				return index
			}
		}
		return len(text) - 1
	case ']': // OSC: ESC ] ... (BEL 或 ESC \)
		for index := esc + 2; index < len(text); index++ {
			if text[index] == 0x07 {
				return index
			}
			if text[index] == 0x1b && index+1 < len(text) && text[index+1] == '\\' {
				return index + 1
			}
		}
		return len(text) - 1
	default:
		return esc + 1
	}
}

// infos 采样整张表。锁内只取"表里有的那几条"的稳定字段，文件 I/O（stat + 读末窗）
// 一律在锁外做：一条命令狂写日志时，探针不得把派发/收尾路径挡住。
func (g *asyncRegistry) infos() []AsyncRunInfo {
	g.mu.Lock()
	infos := make([]AsyncRunInfo, 0, len(g.runs))
	for _, run := range g.runs {
		infos = append(infos, AsyncRunInfo{
			Handle:      run.handle,
			SessionID:   run.sessionID,
			Description: run.description,
			Command:     run.command,
			State:       run.state,
			Exit:        probeExit(run),
			LogPath:     run.logPath,
			Truncated:   run.capHit,
			Degraded:    run.tree != nil && run.tree.Degraded(),
			BatchID:     run.batchID,
			StartedAt:   run.startedAt,
			EndedAt:     run.endedAt,
			Kind:        run.kind,
			Summary:     run.summary,
			Lines:       run.lines,
			Notified:    run.notified,
			Index:       run.index,
		})
	}
	g.mu.Unlock()

	sort.SliceStable(infos, func(left, right int) bool {
		return asyncHandleSeq(infos[left].Handle) < asyncHandleSeq(infos[right].Handle)
	})
	for index := range infos {
		infos[index].LogBytes, infos[index].LastByteAt, infos[index].Tail = sampleLog(infos[index].LogPath)
	}
	return infos
}

// probeExit 把"还没终态"表达成 -1，而不是把零值 0 当成"退出码 0"。
func probeExit(run *asyncRun) int {
	if run.state == asyncStateRunning {
		return -1
	}
	return run.exit
}

// sampleLog 读输出文件的"一眼"：大小、最后修改时间、末行。文件还没建出来或读失败
// 都回零值——探针是只读观测面，不得因为自己读不动就去改登记表里的状态。
func sampleLog(path string) (int64, time.Time, string) {
	if path == "" {
		return 0, time.Time{}, ""
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		size := int64(0)
		if err == nil {
			size = info.Size()
		}
		return size, time.Time{}, ""
	}
	file, err := os.Open(path)
	if err != nil {
		return info.Size(), info.ModTime(), ""
	}
	defer file.Close()
	window := info.Size()
	if window > asyncProbeTailBytes {
		window = asyncProbeTailBytes
	}
	chunk := make([]byte, window)
	if _, err := file.ReadAt(chunk, info.Size()-window); err != nil {
		return info.Size(), info.ModTime(), ""
	}
	return info.Size(), info.ModTime(), lastLine(chunk)
}

// lastLine 取末个非空行并压成一行：界面上"在动"的证据就是最新那行输出。
// 控制字符（含 ANSI 转义）一律替换成空格——进度条类的原始字节进 DOM 会咬坏渲染。
func lastLine(chunk []byte) string {
	lines := strings.Split(string(chunk), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		candidate := strings.TrimSpace(lines[index])
		if candidate != "" {
			return clampProbeLine(candidate)
		}
	}
	return ""
}

func clampProbeLine(line string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, line)
	for strings.Contains(cleaned, "  ") {
		cleaned = strings.ReplaceAll(cleaned, "  ", " ")
	}
	cleaned = strings.TrimSpace(cleaned)
	runes := []rune(cleaned)
	if len(runes) <= asyncProbeTailRunes {
		return cleaned
	}
	return string(runes[:asyncProbeTailRunes]) + "…"
}

// asyncHandleSeq 取一条句柄的派发序号；不认得的形态回 0。
//
// 句柄文法只有一处（`dto.JobNamespace` 的 `<token>-<seq>`）：这里只**转调**，不自己
// 解析形状。前缀曾经靠"首字符是 a"猜，句柄带上 token 之后那套猜测会把每一条都解析成
// 同一个键，排序退化成 map 遍历顺序（界面上就成了乱序的派发史）。
func asyncHandleSeq(handle string) int {
	seq, _ := dto.JobHandleSeq(handle)
	return seq
}
