package workspace

// Git 提交记录树（Git Log Tree）只读查询：GitLog 在 workspace root 内执行
// 固定 argv 的 git log，返回结构化提交行（含 parents 拓扑）。与文件树同级
// 的只读元数据能力：只返回 hash/作者/时间/标题/父提交，绝不返回文件内容或
// diff；命令参数固定（不经过 shell），带超时；非 git 仓库以 Result.Error
// 返回（前端友好展示），不当作 Go error 中断调用。
//
// 拓扑表达：不再取 `--graph` 的字符画前缀（前端画不出真实分叉，只能等宽
// 贴字符串），改取 `%P` 父提交列表 + `--topo-order` 保序，由前端按泳道
// 算法渲染分叉（gui/frontend/dist/tree-fork.js）。

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/winhide"
)

const (
	// gitLogTimeout 是 git log 查询的单次超时（大仓库拓扑排序可能较慢）。
	gitLogTimeout = 5 * time.Second
	// gitLogDefaultLimit 是默认返回的提交条数（前端可传 limit 覆盖）。
	gitLogDefaultLimit = 20
	// gitLogMaxLimit 是单次查询的提交条数上限（防御性钳制）。
	gitLogMaxLimit = 200
)

// GitLog 返回 workspace root 内最近 limit 条提交（含父提交拓扑）。
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
	// 固定 argv，不经过 shell：--topo-order 保证同一分支的提交连续（泳道
	// 布局的前提，等价于 --graph 隐含的排序）；\x01 分隔结构化字段；父提交
	// 放 %P（空格分隔），主题放最后（主题里出现 \x01 也不破坏字段边界）。
	// date 用短格式（MM-dd HH:mm）。
	argv := []string{
		"-C", root,
		"log",
		"--all",
		"--topo-order",
		"--date=format:%m-%d %H:%M",
		"--pretty=format:%H%x01%h%x01%an%x01%ad%x01%P%x01%s",
		"-n", fmt.Sprintf("%d", limit),
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitLogTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", argv...)
	winhide.Apply(cmd)
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
			Root:    root,
			Commits: nil,
			Error:   message,
		}, nil
	}
	result := parseGitLogOutput(stdout.String(), limit)
	result.Root = root
	return result, nil
}

// parseGitLogOutput 把 git log 的原始输出解析为提交行（保序）。
// 纯函数（无 IO），供单元测试直接喂入固定样例。
//
// 行形态（\x01 分隔，主题在最后故可包含任意字符）：
//
//	hash \x01 shortHash \x01 author \x01 date \x01 parents \x01 subject
//
// parents 为空格分隔的父提交 hash（空串 = 根提交）；畸形行（字段不足、
// hash 为空）跳过，不产生半条提交。
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
		fields := strings.SplitN(line, "\x01", 6)
		if len(fields) < 6 {
			continue
		}
		commit := dto.GitCommitNode{
			Hash:      strings.TrimSpace(fields[0]),
			ShortHash: strings.TrimSpace(fields[1]),
			Author:    strings.TrimSpace(fields[2]),
			Date:      strings.TrimSpace(fields[3]),
			Parents:   splitParents(fields[4]),
			Subject:   strings.TrimSpace(fields[5]),
		}
		if commit.Hash == "" {
			continue
		}
		commitSeen++
		if commitSeen > limit {
			break
		}
		result.Commits = append(result.Commits, commit)
	}
	result.Truncated = commitSeen >= limit
	return result
}

// splitParents 把 %P（空格分隔的父提交 hash）解析成切片；根提交返回 nil。
func splitParents(raw string) []string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	return fields
}
