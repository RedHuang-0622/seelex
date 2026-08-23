package workspace

// 工作树（Work Tree）只读元数据查询：ListTree/CountFiles。
// 只返回路径/名称/类型/大小/计数，绝不返回文件内容；忽略规则与敏感
// 文件名过滤保证不把真实配置（config/accounts.yaml、*.local.yaml）暴露给
// 展示层，也不做统计。遍历不跟随符号链接（防环、防逃逸），并有条目预算
// 上限（超大目录截断并标记）。

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

const (
	// treeMaxDepth 是 ListTree 的最大递归深度（防御性钳制）。
	treeMaxDepth = 16
	// treeMaxEntriesPerDir 是单目录列表条目上限（截断并置 Truncated）。
	treeMaxEntriesPerDir = 500
	// treeCountBudget 是单次 CountFiles/ListTree 的总读取条目预算
	// （防病态仓库拖垮 GUI；达到预算即截断）。
	treeCountBudget = 200000
)

// ignoreDirNames 是默认忽略的目录名（元数据展示与统计都不进入）。
var ignoreDirNames = map[string]struct{}{
	".git":         {},
	".seelex":      {},
	"dist":         {},
	"node_modules": {},
	".venv":        {},
	"tmp":          {},
	".idea":        {},
	"__pycache__":  {},
}

// isSensitiveName 判断条目是否属于敏感配置文件（仅元数据也不展示/统计）。
func isSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	return lower == "accounts.yaml" || strings.HasSuffix(lower, ".local.yaml")
}

// TreeEntry 直接子条目排序：目录优先，再按名称大小写不敏感排序。
func sortTreeEntries(entries []dto.TreeEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		leftDir := entries[i].Type == "dir"
		rightDir := entries[j].Type == "dir"
		if leftDir != rightDir {
			return leftDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// resolveTreeDir 把客户端相对路径解析为根内目录绝对路径，同时返回规范化
// 的根绝对路径（只允许相对路径，拒绝绝对路径与逃逸；Windows 大小写不敏感
// containment）。
func resolveTreeDir(root, relPath string) (rootAbs, dirAbs string, err error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", fmt.Errorf("workspace tree: root required")
	}
	rootAbs, err = filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("workspace tree: resolve root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	raw := strings.TrimSpace(relPath)
	if raw == "" {
		raw = "."
	}
	if filepath.IsAbs(raw) {
		return "", "", fmt.Errorf("workspace tree: absolute path not allowed")
	}
	candidate := filepath.Clean(filepath.Join(rootAbs, filepath.FromSlash(raw)))
	if !treeWithinRoot(rootAbs, candidate) {
		return "", "", fmt.Errorf("workspace tree: path escapes workspace root")
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", "", fmt.Errorf("workspace tree: inspect %q: %w", raw, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("workspace tree: %q is not a directory", raw)
	}
	return rootAbs, candidate, nil
}

func treeWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(root), filepath.Clean(candidate)) ||
			!strings.HasPrefix(strings.ToLower(rel), ".."+string(filepath.Separator))
	}
	return true
}

// treeWalker 是带预算的只读遍历器（单次调用内共享，防超大目录无限膨胀）。
type treeWalker struct {
	root      string
	budget    int
	reads     int
	truncated bool
}

// readDir 读取目录并扣除预算；超预算置 truncated 并返回 nil 表示停止。
func (walker *treeWalker) readDir(dir string) ([]os.DirEntry, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// 权限/IO 错误：跳过该目录（best-effort 展示）。
		return nil, false
	}
	walker.reads += len(entries)
	if walker.reads > walker.budget {
		walker.truncated = true
		return nil, false
	}
	return entries, true
}

// ListTree 列出根内某目录的子条目（dir-first 排序、有界；dir 条目带直接
// 文件计数）。depth ≤ 0 视为 1；返回的 Listing.Truncated 表示已达预算或
// 单目录条目上限。只含元数据，不含文件内容。
func (r *Repo) ListTree(root, relPath string, depth int) (dto.TreeListing, error) {
	if depth <= 0 {
		depth = 1
	}
	if depth > treeMaxDepth {
		depth = treeMaxDepth
	}
	rootAbs, dir, err := resolveTreeDir(root, relPath)
	if err != nil {
		return dto.TreeListing{}, err
	}
	// walker.root 是工作区根：条目 Path 一律相对根（跨目录唯一，前端展开键
	// 不会因同名子目录冲突）。
	walker := &treeWalker{root: rootAbs, budget: treeCountBudget}
	listing := dto.TreeListing{Entries: make([]dto.TreeEntry, 0, 64)}
	listTreeLevel(walker, dir, depth, &listing)
	listing.Truncated = walker.truncated
	sortTreeEntries(listing.Entries)
	return listing, nil
}

func listTreeLevel(walker *treeWalker, dir string, depth int, listing *dto.TreeListing) {
	entries, ok := walker.readDir(dir)
	if !ok {
		return
	}
	if len(entries) > treeMaxEntriesPerDir {
		entries = entries[:treeMaxEntriesPerDir]
		walker.truncated = true
	}
	for _, entry := range entries {
		if entry.IsDir() && isIgnoredDir(entry.Name()) {
			continue
		}
		if isSensitiveName(entry.Name()) {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		rel, relErr := filepath.Rel(walker.root, full)
		if relErr != nil {
			continue
		}
		item := dto.TreeEntry{
			Name: entry.Name(),
			Path: filepath.ToSlash(rel),
			Type: "file",
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			item.Size = info.Size()
		}
		if entry.IsDir() {
			item.Type = "dir"
			item.Count = directFileCount(walker, full)
		}
		listing.Entries = append(listing.Entries, item)
		if entry.IsDir() && depth > 1 {
			listTreeLevel(walker, full, depth-1, listing)
		}
		if walker.truncated {
			return
		}
	}
}

// directFileCount 统计目录的直接文件子条目数（不递归；忽略目录与敏感文件
// 不计入）。预算耗尽时返回 0（展示降级，不阻塞）。
func directFileCount(walker *treeWalker, dir string) int {
	entries, ok := walker.readDir(dir)
	if !ok {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isSensitiveName(entry.Name()) {
			continue
		}
		count++
	}
	return count
}

// CountFiles 递归统计工作区文件/目录数（忽略规则与敏感文件除外；有预算
// 上限，Truncated 标记是否截断）。
func (r *Repo) CountFiles(root string) (dto.TreeCount, error) {
	_, dir, err := resolveTreeDir(root, "")
	if err != nil {
		return dto.TreeCount{}, err
	}
	walker := &treeWalker{root: dir, budget: treeCountBudget}
	count := dto.TreeCount{}
	countTreeFiles(walker, dir, &count)
	count.Truncated = walker.truncated
	return count, nil
}

func countTreeFiles(walker *treeWalker, dir string, count *dto.TreeCount) {
	entries, ok := walker.readDir(dir)
	if !ok {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if isIgnoredDir(entry.Name()) {
				continue
			}
			count.Dirs++
			countTreeFiles(walker, filepath.Join(dir, entry.Name()), count)
			if walker.truncated {
				return
			}
			continue
		}
		if isSensitiveName(entry.Name()) {
			continue
		}
		count.Files++
	}
}

func isIgnoredDir(name string) bool {
	_, ignored := ignoreDirNames[name]
	return ignored
}
