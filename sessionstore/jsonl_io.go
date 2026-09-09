package sessionstore

import (
	"bytes"
	"os"
)

// truncateCrashTail 把 JSONL 文件末尾未换行收尾的半行截断，保证后续追加
// 从完整行边界开始（崩溃恢复语义：残尾视为未提交，直接续写不拼坏 JSON）。
func truncateCrashTail(file *os.File) error {
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() == 0 {
		return nil
	}
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, stat.Size()-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	// 从文件尾向前找最后一个 '\n'；找不到则整段视为残尾。
	const scanChunk = 1 << 20
	position := stat.Size()
	for position > 0 {
		start := position - scanChunk
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, position-start)
		if _, err := file.ReadAt(chunk, start); err != nil {
			return err
		}
		if index := bytes.LastIndexByte(chunk, '\n'); index >= 0 {
			return file.Truncate(start + int64(index) + 1)
		}
		position = start
	}
	return file.Truncate(0)
}
