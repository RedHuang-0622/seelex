package core

// workspace_gitrepos.go —— 多仓库 git 的应用用例面（发现 + 按仓库根查询）。
//
// 两件事只在这一处编排：
//
//  1. 仓库发现（WorkspaceGitRepos）：端口是 optional 的
//     contract.WorkspaceGitReposPort，由 workspace.Repo 实现。未绑定工作区或
//     后端没有这个端口时**不报错**，回一份带 Error 文案的空结果——GUI 显示
//     「不可用」而不是弹错。
//  2. 仓库参数解析（resolveWorkspaceRepo）：前端只能给**相对工作区根**的仓库
//     路径（"."/相对路径），本层把它解析成绝对仓库根再交给既有 workspace 实现
//     （端口签名与 git 查询语义都不变）。绝对路径、`..` 逃逸与软链逃逸一律是
//     调用方传错参数 → **Go error**（不是 Result.Error：那一位留给"这不是 git
//     仓库"这类可展示状态）。
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// WorkspaceGitRepos 发现当前工作区内的 git 仓库（多仓库面板的仓库标签数据源）。
//
// 未绑定工作区、后端缺 WorkspaceGitReposPort、工作区根不可读都只体现在
// Result.Error 上（Repos 为空），不向调用方抛错：面板要能显示"这一格现在没有
// 可看的仓库"，而不是整块渲染失败。
func (service *Service) WorkspaceGitRepos() (dto.GitReposResult, error) {
	port, root, err := service.components.workspace.WorkspaceRootPort()
	if err != nil {
		return dto.GitReposResult{Repos: []dto.GitRepoRef{}, Error: err.Error()}, nil
	}
	reposPort, ok := port.(contract.WorkspaceGitReposPort)
	if !ok {
		return dto.GitReposResult{
			Repos: []dto.GitRepoRef{},
			Error: "worktree: workspace backend does not support repo discovery",
		}, nil
	}
	// limit 传 0：默认值与钳制口径归实现（store/workspace 一份）。
	return reposPort.GitRepos(root, 0)
}

// workspaceRepoPort 解析「前端给的仓库相对路径 → 绝对仓库根」并连同后端端口
// 一起交出。四条 git 查询共用它，保证 containment 判据只有一份。
func (service *Service) workspaceRepoPort(repo string) (contract.WorkspacePort, string, error) {
	port, root, err := service.components.workspace.WorkspaceRootPort()
	if err != nil {
		return nil, "", err
	}
	repoRoot, err := resolveWorkspaceRepo(root, repo)
	if err != nil {
		return nil, "", err
	}
	return port, repoRoot, nil
}

// resolveWorkspaceRepo 把仓库相对路径解析成**绝对仓库根**（containment）。
//
// 口径：空串与 "." → 工作区根本身；其余必须是工作区根之内的相对路径。绝对
// 路径、`..` 逃逸、软链逃逸都是调用方传错参数 → Go error。目录不存在/不是目录
// 同样报错（同样属于调用方传错，而不是"这段 git 读不出来"）。
func resolveWorkspaceRepo(root, repo string) (string, error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return "", errors.New("worktree: no workspace bound to current session")
	}
	rootAbs, err := filepath.Abs(trimmedRoot)
	if err != nil {
		return "", fmt.Errorf("worktree: resolve workspace root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	raw := strings.TrimSpace(repo)
	if raw == "" || raw == "." {
		return rootAbs, nil
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("worktree: repo path must be relative to the workspace root: %q", raw)
	}
	candidate := filepath.Clean(filepath.Join(rootAbs, filepath.FromSlash(raw)))
	if !repoWithinRoot(rootAbs, candidate) {
		return "", fmt.Errorf("worktree: repo path escapes the workspace root: %q", raw)
	}
	info, statErr := os.Stat(candidate)
	if statErr != nil {
		return "", fmt.Errorf("worktree: inspect repo %q: %w", raw, statErr)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("worktree: repo %q is not a directory", raw)
	}
	// 软链逃逸：词法 containment 挡不住"根内链接 → 根外目录"，把两侧都解析成
	// 真实路径再判一次。
	realRoot, rootErr := filepath.EvalSymlinks(rootAbs)
	if rootErr != nil {
		realRoot = rootAbs
	}
	realCandidate, candidateErr := filepath.EvalSymlinks(candidate)
	if candidateErr != nil {
		return "", fmt.Errorf("worktree: resolve repo %q: %w", raw, candidateErr)
	}
	if !repoWithinRoot(realRoot, realCandidate) {
		return "", fmt.Errorf("worktree: repo path escapes the workspace root: %q", raw)
	}
	return candidate, nil
}

// repoWithinRoot 判断 candidate 是否落在 root 之内（含相等）。filepath.Rel 在
// Windows 上按大小写不敏感比较路径元素，所以这里只判结果形状即可（与目录树的
// containment 判据同口径）。
func repoWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if filepath.IsAbs(rel) {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
