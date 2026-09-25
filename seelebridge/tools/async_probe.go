package tools

import (
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

// 后台命令的轮询型执行域·实时探针：把登记表里的一次执行采样成"能看懂的一眼"，
// 供工作表格投影（application/core/work_table_async.go）消费。
//
// 探针和取回（async_output）是两回事，别混：
//   - async_output 推进游标、把**新增字节**交给模型，一次一问一答，花一轮往返；
//   - 探针不推进游标、不进上下文，只回答"此刻它在不在动、动到哪一行"，
//     由派发/终态/新字节三类信号驱动（见 asyncRegistry.Events / noteOutput）。
//
// 因此 AsyncRunInfo 允许带命令行原文、绝对路径和时间戳——它们只到 GUI。
// 进模型上下文的字节仍然必须是确定性的那份（asyncPayload），这条纪律不因探针松动。

// AsyncRunInfo 是一次后台执行的探针读数（投影输入；跨包只读）。
type AsyncRunInfo struct {
	Handle      string
	SessionID   string
	Description string    // 模型写的"这条命令在干什么"
	Command     string    // 命令行原文（探针侧不脱敏，GUI 才需要看得懂）
	State       string    // running | done | failed | killed
	Exit        int       // 终态退出码；running 时为 -1
	LogPath     string    // 输出文件（绝对路径，只上 GUI 的附件列）
	LogBytes    int64     // 已落盘字节数：真实进展信号，不是墙钟猜的
	LastByteAt  time.Time // 输出文件最后修改时间（无输出则零值）
	Tail        string    // 末行采样（已压成单行、限长）
	Truncated   bool      // 输出是否已按上限截断
	Degraded    bool      // 进程树挂不上：终止只能打到直接子进程
	BatchID     string    // 派发它的那次 chat 请求（工作表格批次归属）
	StartedAt   time.Time
	EndedAt     time.Time // 零值 = 还没终态
}

// AsyncRuns 返回全部后台执行的探针读数，按派发顺序稳定排序（句柄是 "a<seq>"，
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
		})
	}
	g.mu.Unlock()

	sort.SliceStable(infos, func(left, right int) bool {
		return handleSeq(infos[left].Handle) < handleSeq(infos[right].Handle)
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

// handleSeq 从 "a<seq>" 取回序号；非规范句柄回 0（排序退化为稳定序，不影响正确性）。
func handleSeq(handle string) int {
	if len(handle) < 2 || handle[0] != 'a' {
		return 0
	}
	seq := 0
	for _, ch := range handle[1:] {
		if ch < '0' || ch > '9' {
			return 0
		}
		seq = seq*10 + int(ch-'0')
	}
	return seq
}
