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
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

const (
	// gitLogTimeout 是 git log 查询的单次超时（大仓库拓扑排序可能较慢）。
	gitLogTimeout = 5 * time.Second
	// gitLogDefaultLimit 是默认返回的提交条数（前端可传 limit 覆盖）。
	gitLogDefaultLimit = 20
	// gitLogMaxLimit 是单次查询的提交条数上限（防御性钳制）。
	gitLogMaxLimit = 200
	// gitCommitPrettyFormat 是"一个提交的头部事实"的字段口径：\x01 分隔，
	// 父提交放 %P（空格分隔），主题放最后（主题里出现 \x01 也不破坏字段边界），
	// 日期用短格式。提交列表（GitLog）与单提交详情（GitCommitDetail）共用它——
	// "这个提交是谁、什么时候、说了什么"在两个面上必须是同一份事实。
	gitCommitPrettyFormat = "%H%x01%h%x01%an%x01%ad%x01%P%x01%s"
)

// GitLog 返回 workspace root 内最近 limit 条提交（含父提交拓扑）。
// limit <= 0 使用默认 20；超过 gitLogMaxLimit 钳制。非 git 仓库或 git
// 不可用时返回 Result.Error 描述（调用方仍可展示错误态）。
func (r *Repo) GitLog(root string, limit int) (dto.GitLogResult, error) {
	rootAbs, err := resolveGitRoot(root, "git log")
	if err != nil {
		return dto.GitLogResult{}, err
	}
	if limit <= 0 {
		limit = gitLogDefaultLimit
	}
	if limit > gitLogMaxLimit {
		limit = gitLogMaxLimit
	}
	// 固定 argv，不经过 shell：--topo-order 保证同一分支的提交连续（泳道
	// 布局的前提，等价于 --graph 隐含的排序）；\x01 分隔结构化字段；字段口径
	// 见 gitCommitPrettyFormat（与提交详情共用）。
	argv := []string{
		"-C", rootAbs,
		"log",
		"--all",
		"--topo-order",
		"--date=format:%m-%d %H:%M",
		"--pretty=format:" + gitCommitPrettyFormat,
		"-n", fmt.Sprintf("%d", limit),
	}
	stdout, message := runGitRead(argv, gitLogTimeout, "git log 查询超时")
	if message != "" {
		return dto.GitLogResult{
			Root:    rootAbs,
			Commits: nil,
			Error:   message,
		}, nil
	}
	result := parseGitLogOutput(string(stdout), limit)
	result.Root = rootAbs
	return result, nil
}

// parseGitLogOutput 把 git log 的原始输出解析为提交行（保序）。
// 纯函数（无 IO），供单元测试直接喂入固定样例。
//
// 行形态见 gitCommitPrettyFormat（\x01 分隔，主题在最后故可包含任意字符）：
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
		commit, ok := parseGitCommitFields(line)
		if !ok {
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

// parseGitCommitFields 解析一行 gitCommitPrettyFormat 口径的提交头部事实。
// 纯函数：字段不足（< 6）或 hash 为空时返回 ok=false，不产生半条提交。
// 提交列表与单提交详情共用它，字段口径只有一处。
func parseGitCommitFields(line string) (dto.GitCommitNode, bool) {
	fields := strings.SplitN(strings.TrimRight(line, "\r"), "\x01", 6)
	if len(fields) < 6 {
		return dto.GitCommitNode{}, false
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
		return dto.GitCommitNode{}, false
	}
	return commit, true
}

// splitParents 把 %P（空格分隔的父提交 hash）解析成切片；根提交返回 nil。
func splitParents(raw string) []string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	return fields
}
