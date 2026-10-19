package workspace

// 工作区内 git 仓库发现（多仓库 git 的第一半：发现端）。
//
// 工作区根下若干目录各自是独立仓库（或根本身不是仓库、子目录才是）时，
// 「提交记录 / 工作区更改」两个面板要能按仓库切换——但前端只能拿到相对路径，
// 所以这里做一次只读的 BFS 扫描，回报有界的一份仓库清单：
//
//   - 仓库判定：目录内存在名为 `.git` 的条目，**目录或文件都算**（文件形态是
//     worktree / 子模块，`git -C` 一样可用）；
//   - 仓库内部**继续下钻**（跳过 `.git` 自身）：仓库里套独立仓库很常见；
//   - 剪枝复用 tree.go 的 ignoreDirNames + 隐藏目录（`.` 开头），不写第二份表；
//   - 三重预算：深度、进过的目录数、仓库条目数，任一触顶即 Truncated 如实上报；
//   - 读不了的目录（权限/IO）跳过并继续，不让一次扫描整体失败。
//
// 只读元数据：只返回相对路径、展示名、是否主仓库与分支名，绝不含提交、diff、
// 文件内容或绝对路径（Root 只是本次扫描的根，用于前端判断"同一份快照"）。
import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

const (
	// gitReposMaxDepth 是仓库发现的最大下钻深度（工作区根 = 0）。深过 6 层的
	// 仓库已经不算"工作区里的仓库"，再扫下去只是替别人的依赖树数目录。
	gitReposMaxDepth = 6
	// gitReposDirBudget 是单次扫描进过的目录上限（防大仓库把面板刷新拖成
	// IO 风暴）。
	gitReposDirBudget = 20000
	// gitReposDefaultLimit 是默认回报的仓库条目数（前端标签栏也放不下更多）。
	gitReposDefaultLimit = 64
	// gitReposMaxLimit 是仓库条目数的硬上限（防御性钳制）。
	gitReposMaxLimit = 256
	// gitReposToplevelTimeoutMessage 是祖先仓库探测超时的展示文案。
	gitReposToplevelTimeoutMessage = "git 仓库探测超时"
)

// clampRepoLimit 把调用方给的仓库条目上限收敛到实现口径：<=0 用默认值，
// 超过硬上限钳制（纯函数，便于单测钉住钳制边界）。
func clampRepoLimit(limit int) int {
	if limit <= 0 {
		return gitReposDefaultLimit
	}
	if limit > gitReposMaxLimit {
		return gitReposMaxLimit
	}
	return limit
}

// gitReposNode 是 BFS 队列里的一层目录。
type gitReposNode struct {
	dir   string // 绝对路径（只在实现内部用，不下发）
	depth int
}

// GitRepos 扫描 root 内的 git 仓库（BFS、有界、只读）。
//
// limit <= 0 用默认 64，超过 256 钳制。空 root / 根不可读不当作程序错误：
// 返回带 Error 文案的空结果（GUI 显示"不可用/未绑定"而不是弹错）。
func (r *Repo) GitRepos(root string, limit int) (dto.GitReposResult, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return dto.GitReposResult{
			Repos: []dto.GitRepoRef{},
			Error: "未绑定工作区，无法发现仓库",
		}, nil
	}
	rootAbs, err := filepath.Abs(trimmed)
	if err != nil {
		return dto.GitReposResult{
			Repos: []dto.GitRepoRef{},
			Error: fmt.Sprintf("仓库发现：无法解析工作区根（%v）", err),
		}, nil
	}
	rootAbs = filepath.Clean(rootAbs)
	info, statErr := os.Stat(rootAbs)
	if statErr != nil || !info.IsDir() {
		return dto.GitReposResult{
			Root:  rootAbs,
			Repos: []dto.GitRepoRef{},
			Error: "仓库发现：工作区根不可读",
		}, nil
	}

	limit = clampRepoLimit(limit)
	rootName := filepath.Base(rootAbs)
	rootIsRepo := hasGitEntry(rootAbs)

	repos := make([]dto.GitRepoRef, 0, 8)
	// 根自身就是仓库时先占一格：它永远是主仓库（Rel = "."）。
	if rootIsRepo {
		repos = append(repos, dto.GitRepoRef{
			Rel:     ".",
			Name:    rootName,
			Primary: true,
			Branch:  gitRepoBranch(rootAbs),
		})
	}

	scanned := 0
	truncated := false
	queue := []gitReposNode{{dir: rootAbs, depth: 0}}
	for len(queue) > 0 {
		if scanned >= gitReposDirBudget {
			truncated = true
			break
		}
		node := queue[0]
		queue = queue[1:]
		scanned++
		entries, readErr := os.ReadDir(node.dir)
		if readErr != nil {
			// 权限/IO：跳过该目录继续（best-effort 展示），不让整次扫描失败。
			continue
		}
		isRoot := node.dir == rootAbs
		if !isRoot && hasGitEntryIn(entries) {
			if len(repos) >= limit {
				// 条目预算触顶：后面再有仓库也不会展示，如实标记截断并收工。
				truncated = true
				break
			}
			rel, relErr := filepath.Rel(rootAbs, node.dir)
			if relErr == nil {
				repos = append(repos, dto.GitRepoRef{
					Rel:    filepath.ToSlash(rel),
					Name:   filepath.Base(node.dir),
					Branch: gitRepoBranch(node.dir),
				})
			}
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				// 符号链接（entry.Type 不是目录）不跟随：既防环也防逃逸。
				continue
			}
			name := entry.Name()
			if isIgnoredDir(name) || strings.HasPrefix(name, ".") {
				continue
			}
			if node.depth >= gitReposMaxDepth {
				truncated = true
				continue
			}
			queue = append(queue, gitReposNode{dir: filepath.Join(node.dir, name), depth: node.depth + 1})
		}
	}

	// 根不是仓库时看它是否落在某个祖先仓库里（`git -C root rev-parse
	// --show-toplevel`）：命中即沿用"以工作区根为视角"的既有语义，也记
	// Rel="."、Primary=true。都没有则没有 Primary（前端回落 Repos[0]）。
	if !rootIsRepo {
		if toplevel := gitAncestorToplevel(rootAbs); toplevel != "" {
			primary := dto.GitRepoRef{
				Rel:     ".",
				Name:    rootName,
				Primary: true,
				Branch:  gitRepoBranch(toplevel),
			}
			repos = append([]dto.GitRepoRef{primary}, repos...)
			if len(repos) > limit {
				repos = repos[:limit]
				truncated = true
			}
		}
	}

	sortGitRepos(repos)

	return dto.GitReposResult{
		Root:      rootAbs,
		Repos:     repos,
		Truncated: truncated,
		Scanned:   scanned,
	}, nil
}

// sortGitRepos 排序：Primary 优先，其余按 Rel 字典序（读数确定、标签位置稳定）。
func sortGitRepos(repos []dto.GitRepoRef) {
	sort.SliceStable(repos, func(i, j int) bool {
		if repos[i].Primary != repos[j].Primary {
			return repos[i].Primary
		}
		return repos[i].Rel < repos[j].Rel
	})
}

// hasGitEntry 报告 dir 内是否有 `.git` 条目（目录或文件形态都算）。
func hasGitEntry(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// hasGitEntryIn 在已读出的目录条目里找 `.git`（省一次 stat）。
func hasGitEntryIn(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if entry.Name() == ".git" {
			return true
		}
	}
	return false
}

// gitAncestorToplevel 探测 root 所属的仓库根（root 自己是仓库时就是它自己）。
// 用固定 argv（不经 shell）、只读、带超时；git 缺失/不是仓库/超时一律返回 ""。
func gitAncestorToplevel(root string) string {
	stdout, message := runGitRead(
		[]string{"-C", root, "rev-parse", "--show-toplevel"},
		gitLogTimeout,
		gitReposToplevelTimeoutMessage,
	)
	if message != "" {
		return ""
	}
	toplevel := strings.TrimSpace(string(stdout))
	if toplevel == "" {
		return ""
	}
	// Windows 上 git 可能回报正斜杠/大小写不同的路径：统一绝对化再比对。
	abs, err := filepath.Abs(filepath.FromSlash(toplevel))
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

// gitRepoBranch 读仓库当前分支：直接读 `.git/HEAD`（不跑子进程，避免为标签栏
// 拉起 N 个 git 进程）。`.git` 是文件时按 `gitdir: <path>` 跟随到真正的 git
// 目录（worktree / 子模块形态）。读不到、分离头指针一律留空——分支只是展示，
// 永远不阻塞列表。
func gitRepoBranch(dir string) string {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return ""
	}
	gitDir := gitPath
	if !info.IsDir() {
		target, ok := readGitDirFile(gitPath, dir)
		if !ok {
			return ""
		}
		gitDir = target
	}
	data, readErr := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if readErr != nil {
		return ""
	}
	return parseGitHeadRef(string(data))
}

// readGitDirFile 解析 `.git` 文件形态（首行 `gitdir: <path>`），相对路径以
// 该仓库目录为基准。
func readGitDirFile(gitFile, repoDir string) (string, bool) {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if target == "" {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(repoDir, filepath.FromSlash(target))
	}
	return filepath.Clean(target), true
}

// parseGitHeadRef 把 HEAD 内容解析成分支名：`ref: refs/heads/<name>` → `<name>`；
// 其余（分离头指针、损坏内容）返回 ""。
func parseGitHeadRef(raw string) string {
	line := strings.TrimSpace(strings.SplitN(raw, "\n", 2)[0])
	if !strings.HasPrefix(line, "ref:") {
		return ""
	}
	ref := strings.TrimSpace(strings.TrimPrefix(line, "ref:"))
	ref = strings.TrimPrefix(ref, "refs/heads/")
	if ref == "" || ref == "HEAD" || strings.HasPrefix(ref, "refs/") {
		return ""
	}
	return ref
}
