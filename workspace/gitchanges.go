package workspace

// 工作区未提交改动（Work Tree Changes）只读查询：GitChanges 在 workspace
// root 内执行固定 argv 的 git status，返回结构化改动行（状态/路径/重命名原
// 路径）。与工作树/提交记录（tree.go、gitlog.go）同级的只读元数据能力：
// 只返回路径与状态字符，绝不返回文件内容、diff 补丁或 blob；
// --no-optional-locks 以只读方式取状态（不刷新索引、不抢 index 锁），带超时；
// 非 git 仓库以 Result.Error 返回（前端友好展示），不当作 Go error 中断调用。
//
// 两处与「直接读 git」不同的收敛，都在这里做掉，不让展示层各自实现：
//  1. 路径基准——git status 的路径以**仓库根**为基准，而面板以**工作区根**为
//     基准（绑定的目录可能是仓库子目录）。这里按 rev-parse --show-toplevel 剥
//     掉前缀，工作区之外的兄弟路径直接丢弃并计入 Result.Filtered。
//  2. 可见性边界——与 ListTree/ReadFile 一致：路径任一环节命中敏感文件名
//     （accounts.yaml、*.local.yaml）不展示。目录噪音边界交给 git 自己的
//     .gitignore：把 ignoreDirNames（node_modules/dist/tmp…）也套上去，会连
//     「被仓库跟踪的 dist/ 改动」一起藏掉，那是误报而不是降噪。

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/winhide"
)

const (
	// gitChangesTimeout 是 git status 查询的单次超时（-uall 在大仓库上可能较慢）。
	gitChangesTimeout = 8 * time.Second
	// gitChangesDefaultLimit 是默认返回的改动条数（前端可传 limit 覆盖）。
	gitChangesDefaultLimit = 200
	// gitChangesMaxLimit 是单次查询的改动条数上限（防御性钳制）。
	gitChangesMaxLimit = 1000
	// gitChangesScanBudget 是解析记录预算：--porcelain -uall 在未收敛的仓库
	// （几十万个未跟踪文件）上能一次吐出上百 MB，解析前先按预算截断并标记。
	gitChangesScanBudget = 20000
)

// GitChanges 返回工作区 root 内未提交的改动（含暂存/未暂存/未跟踪/冲突）。
// limit <= 0 使用默认 200；超过 gitChangesMaxLimit 钳制。非 git 仓库或 git
// 不可用时返回 Result.Error 描述（调用方仍可展示错误态）。
func (r *Repo) GitChanges(root string, limit int) (dto.WorkspaceChangesResult, error) {
	rootAbs, err := resolveGitRoot(root, "git changes")
	if err != nil {
		return dto.WorkspaceChangesResult{}, err
	}
	if limit <= 0 {
		limit = gitChangesDefaultLimit
	}
	if limit > gitChangesMaxLimit {
		limit = gitChangesMaxLimit
	}

	// 仓库顶层目录：工作区根可能是仓库子目录，路径需要按它剥前缀。
	// 非 git 仓库返回空串（下面的 status 会给出友好错误）。
	topLevel := gitTopLevel(rootAbs)
	prefix := workspacePathPrefix(topLevel, rootAbs)

	// 固定 argv，不经过 shell：-b 取分支头（-z 下也是首条 NUL 记录）；
	// -z 用 NUL 分隔且**不做引号转义**（含空格/中文/换行的路径原样返回，
	// 重命名是「新路径 NUL 旧路径」两条记录）；-uall 逐个列出未跟踪文件
	// （不用目录折叠形式，前端才能逐个打开预览）；-- . 把范围钉在工作区
	// 子树内（否则 -C 子目录仍会带出工作区之外的改动）。
	argv := []string{
		"-C", rootAbs,
		"--no-optional-locks",
		"status",
		"--porcelain=v1",
		"-b",
		"-z",
		"-uall",
		"--",
		".",
	}
	stdout, message := runGitRead(argv, gitChangesTimeout, "git 改动查询超时")
	if message != "" {
		return dto.WorkspaceChangesResult{Root: rootAbs, Error: message}, nil
	}

	parsed := parseGitStatusOutput(stdout, gitChangesScanBudget)
	result := dto.WorkspaceChangesResult{
		Root:   rootAbs,
		Branch: parsed.branch,
	}
	kept := make([]dto.WorkspaceChangeEntry, 0, len(parsed.entries))
	keptCount := 0
	for _, change := range parsed.entries {
		kind := classifyChange(change.index, change.worktree)
		if kind == "" {
			// 畸形/未知状态字符：不产生半条改动，也不计入任何统计。
			continue
		}
		path, ok := relativeToWorkspace(change.path, prefix)
		if !ok {
			result.Filtered++
			continue
		}
		oldPath := ""
		if change.oldPath != "" {
			old, okOld := relativeToWorkspace(change.oldPath, prefix)
			if !okOld {
				result.Filtered++
				continue
			}
			oldPath = old
		}
		if isSensitiveChangePath(path) || (oldPath != "" && isSensitiveChangePath(oldPath)) {
			result.Filtered++
			continue
		}
		staged := change.index != ' ' && change.index != '?'
		keptCount++
		// 统计先于截断：头部数字说的是工作区状态，不是本屏行数。
		switch {
		case kind == dto.ChangeUntracked:
			result.Untracked++
		case kind == dto.ChangeConflicted:
			result.Conflicted++
		default:
			if staged {
				result.Staged++
			}
			if change.worktree != ' ' && change.worktree != '?' {
				result.Unstaged++
			}
		}
		kept = append(kept, dto.WorkspaceChangeEntry{
			Path:     path,
			OldPath:  oldPath,
			Kind:     kind,
			Status:   string([]byte{change.index, change.worktree}),
			Index:    string(change.index),
			Worktree: string(change.worktree),
			Staged:   staged,
		})
	}
	result.Total = keptCount
	if len(kept) > limit {
		kept = kept[:limit]
		result.Truncated = true
	}
	if parsed.truncated {
		result.Truncated = true
	}
	result.Entries = kept
	return result, nil
}

// parsedChange 是一条解析出来的原始改动（尚未做路径归一化与敏感过滤）。
type parsedChange struct {
	path     string
	oldPath  string
	index    byte // X：暂存侧
	worktree byte // Y：工作区侧
}

// parsedGitStatus 是一次解析结果（纯数据，便于单测直接喂固定样例）。
type parsedGitStatus struct {
	branch    string
	entries   []parsedChange
	truncated bool
}

// parseGitStatusOutput 把 `git status --porcelain=v1 -b -z` 的原始输出解析成
// 改动行（保序）。纯函数（无 IO），供单元测试直接喂入固定样例。
//
// 记录形态（NUL 分隔，-z 下路径不做引号转义）：
//
//	"## <branch>"          分支头（-b；-z 下同样是首条 NUL 记录）
//	"XY<space><path>"      一条改动；X/Y 为状态字符，空格表示该侧无改动
//	"<oldpath>"            仅当上一条 X 为 R/C：紧跟一条原路径记录
//
// 畸形记录（长度不足、第 3 字符不是空格、未知状态字符）跳过，不产生半条改动；
// 达到 maxRecords 即停止并置 truncated（防御病态输出）。
func parseGitStatusOutput(output []byte, maxRecords int) parsedGitStatus {
	parsed := parsedGitStatus{}
	if maxRecords <= 0 {
		maxRecords = gitChangesScanBudget
	}
	records := bytes.Split(output, []byte{0})
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) == 0 {
			continue
		}
		if bytes.HasPrefix(record, []byte("## ")) {
			parsed.branch = parseBranchHeader(string(record))
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			continue
		}
		change := parsedChange{
			index:    record[0],
			worktree: record[1],
			path:     string(record[3:]),
		}
		if change.index == 'R' || change.index == 'C' {
			// 重命名/复制：下一条记录就是原路径。git 一定成对输出，因此这里
			// 原样消费下一条记录（不拿"看起来像不像改动行"去猜——-z 下原路径
			// 是裸路径，第 3 个字符可能正好是空格）；只有缺失/空记录才视为
			// 畸形丢弃，避免造出半条改动。
			if index+1 >= len(records) || len(records[index+1]) == 0 {
				continue
			}
			index++
			change.oldPath = string(records[index])
		}
		if len(parsed.entries) >= maxRecords {
			parsed.truncated = true
			break
		}
		parsed.entries = append(parsed.entries, change)
	}
	return parsed
}

// parseBranchHeader 解析 -b 的分支头（porcelain v1）。见过的形态：
//
//	"## main"
//	"## main...origin/main [ahead 1, behind 2]"
//	"## No commits yet on main"        （空仓库）
//	"## HEAD (no branch)"              （分离头指针）
//
// 上游与 ahead/behind 尾注不下发（面板只说"在哪条分支上"，不装作知道推送状态）。
func parseBranchHeader(line string) string {
	body := strings.TrimPrefix(line, "## ")
	if cut := strings.Index(body, " ["); cut >= 0 {
		body = body[:cut]
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "No commits yet on ") {
		body = strings.TrimSpace(strings.TrimPrefix(body, "No commits yet on "))
	}
	if strings.HasPrefix(body, "HEAD (no branch)") {
		return "HEAD"
	}
	if name, _, found := strings.Cut(body, "..."); found {
		return strings.TrimSpace(name)
	}
	return body
}

// classifyChange 把 porcelain 的 XY 归一化成展示分类；未知/无改动组合返回空串
// （调用方跳过）。暂存侧优先：一处改动同时有暂存与未暂存变化时（"MM"）按暂存
// 侧分类，Staged/分侧字符仍如实下发，前端可分别标注。
func classifyChange(index, worktree byte) string {
	if index == '?' && worktree == '?' {
		return dto.ChangeUntracked
	}
	if isConflictXY(index, worktree) {
		return dto.ChangeConflicted
	}
	if kind := statusKind(index); kind != "" {
		return kind
	}
	return statusKind(worktree)
}

// isConflictXY 判断 XY 是否为未解决冲突（porcelain v1 的冲突组合：任一侧为 U，
// 或 both added "AA" / both deleted "DD"）。
func isConflictXY(index, worktree byte) bool {
	if index == 'U' || worktree == 'U' {
		return true
	}
	return (index == 'A' && worktree == 'A') || (index == 'D' && worktree == 'D')
}

// statusKind 把单个状态字符映射为展示分类；空格（该侧无改动）与未知字符返回空串。
func statusKind(code byte) string {
	switch code {
	case 'M':
		return dto.ChangeModified
	case 'T':
		return dto.ChangeTypeChanged
	case 'A':
		return dto.ChangeAdded
	case 'D':
		return dto.ChangeDeleted
	case 'R':
		return dto.ChangeRenamed
	case 'C':
		return dto.ChangeCopied
	}
	return ""
}

// isSensitiveChangePath 与 ListTree/ReadFile 同一可见性边界：路径任一环节命中
// 敏感文件名即不展示（工作区之外的兄弟路径在路径归一化阶段已丢弃）。
func isSensitiveChangePath(rel string) bool {
	for _, segment := range strings.Split(rel, "/") {
		if segment == "" {
			continue
		}
		if isSensitiveName(segment) {
			return true
		}
	}
	return false
}

// gitTopLevel 返回 root 所属仓库的顶层目录；非 git 仓库或 git 不可用时返回空串
// （调用方按"root 就是基准"处理，真正的错误在 status 调用处上报）。
func gitTopLevel(root string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitChangesTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--show-toplevel")
	winhide.Apply(cmd)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// workspacePathPrefix 计算仓库根 → 工作区根的前缀（"sub/" 形式，/ 分隔）；
// 工作区根即仓库根（或不是 git 仓库）时返回空串。
func workspacePathPrefix(topLevel, rootAbs string) string {
	if strings.TrimSpace(topLevel) == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(topLevel), rootAbs)
	if err != nil || rel == "." || rel == "" {
		return ""
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		// 工作区根不在该仓库内（例如绑定了另一个仓库的目录）：不剥前缀，
		// 交给 relativeToWorkspace 的越界判定逐条丢弃。
		return ""
	}
	return filepath.ToSlash(rel) + "/"
}

// relativeToWorkspace 把仓库根基准的路径转成工作区根基准。返回 ok=false 表示
// 该路径落在工作区之外（例如同一仓库里的兄弟目录改动），调用方丢弃并计数。
// Windows 上大小写不敏感（git 打印的路径大小写与 root 的输入大小写可能不同）。
func relativeToWorkspace(path, prefix string) (string, bool) {
	path = strings.TrimPrefix(path, "./")
	if path == "" || strings.HasPrefix(path, "/") {
		return "", false
	}
	if prefix == "" {
		if escapesRoot(path) {
			return "", false
		}
		return path, true
	}
	if len(path) <= len(prefix) || !strings.EqualFold(path[:len(prefix)], prefix) {
		return "", false
	}
	rel := path[len(prefix):]
	if escapesRoot(rel) {
		return "", false
	}
	return rel, true
}

// escapesRoot 判断相对路径是否逃出根（".." 开头；git 正常不会给出，防御性）。
func escapesRoot(path string) bool {
	return path == ".." || strings.HasPrefix(path, "../") || strings.HasPrefix(path, `..\`)
}
