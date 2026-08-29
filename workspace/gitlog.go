package workspace

// Git 提交记录树（Git Log Tree）只读查询：GitLog 在 workspace root 内执行
// 固定 argv 的 git log --graph，解析为结构化提交行 + 拓扑 graph 前缀。
// 与文件树同级的只读元数据能力：只返回 hash/作者/时间/标题，绝不返回
// 文件内容或 diff；命令参数固定（不经过 shell），带超时；非 git 仓库以
// Result.Error 返回（前端友好展示），不当作 Go error 中断调用。

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

const (
	// gitLogTimeout 是 git log 查询的单次超时（大仓库 graph 计算可能较慢）。
	gitLogTimeout = 5 * time.Second
	// gitLogDefaultLimit 是默认返回的提交条数（前端可传 limit 覆盖）。
	gitLogDefaultLimit = 20
	// gitLogMaxLimit 是单次查询的提交条数上限（防御性钳制）。
	gitLogMaxLimit = 200
)

// GitLog 返回 workspace root 内最近 limit 条提交的 --graph 拓扑行。
// limit <= 0 使用默认 20；超过 gitLogMaxLimit 钳制。非 git 仓库或 git
// 不可用时返回 Result.Error 描述（调用方仍可展示错误态）。
func (r *Repo) GitLog(root string, limit int) (dto.GitLogResult, error) {
	if strings.TrimSpace(root) == "" {
		return dto.GitLogResult{}, fmt.Errorf("git log: root required")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return dto.GitLogResult{}, fmt.Errorf("git log: inspect root: %w", err)
	}
	if limit <= 0 {
		limit = gitLogDefaultLimit
	}
	if limit > gitLogMaxLimit {
		limit = gitLogMaxLimit
	}
	// 固定 argv，不经过 shell：--graph 生成拓扑字符前缀；\x00 分隔 graph
	// 前缀与提交字段；\x01 分隔结构化字段。date 用短格式（MM-dd HH:mm）。
	argv := []string{
		"-C", root,
		"log",
		"--all",
		"--graph",
		"--date=format:%m-%d %H:%M",
		"--pretty=format:%x00%H%x01%h%x01%an%x01%ad%x01%s",
		"-n", fmt.Sprintf("%d", limit),
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitLogTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = runErr.Error()
		}
		if ctx.Err() != nil {
			message = "git log 查询超时"
		}
		return dto.GitLogResult{
			Root:  root,
			Lines: nil,
			Error: message,
		}, nil
	}
	result := parseGitLogOutput(stdout.String(), limit)
	result.Root = root
	return result, nil
}

// parseGitLogOutput 把 git log --graph 的原始输出解析为拓扑行 + 提交索引。
// 纯函数（无 IO），供单元测试直接喂入固定样例。
//
// 行形态：
//
//	graphPrefix \x00 hash \x01 shortHash \x01 author \x01 date \x01 subject
//
// 不含 \x00 的行是 graph 延续线（merge 的 | \ / 等），保留前缀但无提交。
func parseGitLogOutput(output string, limit int) dto.GitLogResult {
	if limit <= 0 {
		limit = gitLogDefaultLimit
	}
	if limit > gitLogMaxLimit {
		limit = gitLogMaxLimit
	}
	result := dto.GitLogResult{Commits: make([]dto.GitCommitNode, 0, 16)}
	commitSeen := 0
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 2)
		graph := strings.TrimRight(parts[0], " ")
		if len(parts) < 2 {
			// 延续线：只有 graph 字符（无提交字段）。
			if graph != "" {
				result.Lines = append(result.Lines, dto.GitLogLine{Graph: graph})
			}
			continue
		}
		fields := strings.Split(parts[1], "\x01")
		if len(fields) < 5 {
			continue
		}
		commit := dto.GitCommitNode{
			Hash:      strings.TrimSpace(fields[0]),
			ShortHash: strings.TrimSpace(fields[1]),
			Author:    strings.TrimSpace(fields[2]),
			Date:      strings.TrimSpace(fields[3]),
			Subject:   strings.TrimSpace(fields[4]),
		}
		if commit.Hash == "" {
			continue
		}
		commitSeen++
		if commitSeen > limit {
			break
		}
		result.Lines = append(result.Lines, dto.GitLogLine{Graph: graph, Commit: &commit})
		result.Commits = append(result.Commits, commit)
	}
	result.Truncated = commitSeen >= limit
	return result
}
