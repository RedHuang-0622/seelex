package store

// project_known.go — 项目摘要渲染件（`project-known.md`）的**确定性渲染**与项目级写/读面。
//
// 口径（写在这里防误读，契约见 docs/arch/seelex-project-board.md §3 / §6.1 / §7）：
//
//  1. **它是 `ProjectRecord` 的人读渲染（读侧视图），不是输入源**：渲染不改判定、
//     不写事实；领域判定照旧读 task/teamwork 事实（I1/I3）。产物若被当成"第二份
//     项目知识"就是第二份实现。
//  2. **不新增第二条项目知识链**：谁能产出 `ProjectRecord` 仍只有
//     `ProjectKnowledgeBuilder`（来源合并链：元数据→文档目录→手写说明）与
//     `RefreshProjectKnowledge`（「复用 / 重建 / 回退」三分支）。本文件**只加一步
//     渲染**（`ProjectKnownMarkdown`）与一步落盘（`WriteProjectKnown`）。
//  3. **被动（R6）**：无显式调用 → 不产文件。不在装配期构建、不自动刷新；只有
//     `project_refresh` 工具被调用时才渲染 + 落盘（main.go 的 handler）。
//  4. **可提交件三铁律**：渲染件不含 `BuiltAt`、不含绝对路径、不含 `ws-<nano>`；
//     只写仓库相对路径（绝对路径的 `Path` 丢弃，`Docs` 同理）。同一 `ProjectRecord`
//     两次渲染**字节完全相同**（模块按 `Name` 稳定排序）。
//  5. 落点 = `<项目根>/.seelex/board/project-known.md`（**项目目录**，与两块投影同一个
//     目录）；目录派生**复用** `ProjectBoardDir`（project_board.go），发布**复用**
//     `storeroot.WriteAtomic`（store/root/atomic.go），锁**复用** `projectBoardLock`
//     （project_board.go，按文件路径分片的独立锁，不新增 storageModule 枚举）。
//
// 复用清单（**禁止第二份实现**，交付时同表复核）：
//
//	需求              | 复用（file:line）                                  | 不新写
//	------------------|----------------------------------------------------|------------------
//	目录派生           | `ProjectBoardDir` project_board.go                 | 第二套目录命名
//	原子发布           | `storeroot.WriteAtomic` store/root/atomic.go | 第二份 rename 实现
//	按路径独立锁       | `projectBoardLock` project_board.go                | 新 storageModule 枚举
//	项目知识链         | `ProjectKnowledgeBuilder` / `RefreshProjectKnowledge` project_record.go | 第二份项目知识实现
//	根校验             | `validateProjectRoot` project_board.go             | 第二份空根校验

import (
	"bytes"
	"fmt"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ProjectBoardKnownFile 是项目级摘要渲染件的文件名。
//
// 它与 ProjectBoardWorkFile / ProjectBoardTeamworkFile **不同层**：那两个在会话级
// 投影目录（`session-<hash>/`）下、按行追加；本文件在**项目级**目录下、整份原子发布
// （渲染件没有追加语义）。故它不进 projectBoardFile 的白名单（那三个是 jsonl 行文件）。
const ProjectBoardKnownFile = "project-known.md"

// projectKnownHeader 是渲染件固定首行（无 record 依赖 ⇒ 字节稳定）。
const projectKnownHeader = "# 项目摘要（Project Known）\n\n"

// projectKnownWorkspaceToken 匹配机器本地的 `ws-<nano>` 工作区 ID。
//
// 为什么要在渲染层剔除（而不是指望来源干净）：`ProjectRecord` 的行内 scope 允许带
// `ws-…`（它是不提交的本机派生物），但**渲染件是可能要提交的**——见契约 §3
// "可提交件的三条硬纪律"。
var projectKnownWorkspaceToken = regexp.MustCompile(`ws-[0-9]{6,}`)

// projectKnownAbsolutePathToken 匹配 Windows 盘符开头的绝对路径（prose 里的兜底清洗）。
var projectKnownAbsolutePathToken = regexp.MustCompile(`[A-Za-z]:[\\/][^\s，。；、）)"'` + "`" + `]*`)

// ProjectKnownMarkdown 把 `ProjectRecord` 渲染成人读 markdown。
//
// **纯函数**：不读盘、不取时间、不改入参（内部拷贝后排序），同一 record 两次渲染
// 字节完全相同。剔除 `BuiltAt`（会让每次刷新都产生无意义 diff）、绝对路径、`ws-<nano>`。
func ProjectKnownMarkdown(record ProjectRecord) []byte {
	modules := make([]ModuleSemantics, len(record.Modules))
	copy(modules, record.Modules)
	sort.SliceStable(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })

	var builder strings.Builder
	builder.WriteString(projectKnownHeader)
	if version := sanitizeKnownText(record.Version); version != "" {
		// Version 是来源 hash 的内容版本（确定性），不是抖动时间戳 ⇒ 可以写。
		builder.WriteString("版本：`" + version + "`\n\n")
	}
	for _, module := range modules {
		name := sanitizeKnownText(module.Name)
		if name == "" {
			continue // 没有名字的模块渲染出来只是一层空标题，不产噪声
		}
		builder.WriteString("## " + name + "\n\n")
		if summary := sanitizeKnownText(module.Summary); summary != "" {
			builder.WriteString(summary + "\n\n")
		}
		if path, ok := repoRelativePath(module.Path); ok {
			builder.WriteString("- 路径：`" + path + "`\n")
		}
		docs := make([]string, 0, len(module.Docs))
		for _, doc := range module.Docs {
			relative, ok := repoRelativePath(doc)
			if !ok {
				continue // 绝对路径的 Docs 同样丢弃
			}
			docs = append(docs, relative)
		}
		sort.Strings(docs)
		for index, doc := range docs {
			if index > 0 && doc == docs[index-1] {
				continue // 去重：来源合并可能带来同一份文档
			}
			builder.WriteString("- 文档：`" + doc + "`\n")
		}
		builder.WriteString("\n")
	}
	return []byte(builder.String())
}

// sanitizeKnownText 清洗人读文本：剔除 `ws-<nano>` 与盘符绝对路径（渲染件要能提交）。
func sanitizeKnownText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = projectKnownWorkspaceToken.ReplaceAllString(value, "ws-<workspace>")
	value = projectKnownAbsolutePathToken.ReplaceAllString(value, "<abs-path>")
	return strings.TrimSpace(value)
}

// repoRelativePath 判定并归一化**仓库相对路径**；绝对路径一律丢弃（ok=false）。
//
// 之所以判得这么啰嗦：`ProjectKnowledgeBuilder` 的 `Path` / `Docs` 都是从绝对扫描根
// 拼出来的（project_record.go 的 scanModules / scanDocDirModules），渲染层必须把它们
// 挡在产物之外，否则渲染件带着本机用户名与盘符，永远提交不出去。
func repoRelativePath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if projectKnownWorkspaceToken.MatchString(value) {
		return "", false
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return "", false // 前导分隔符 = 绝对（POSIX）或 UNC 根
	}
	if filepath.IsAbs(value) {
		return "", false
	}
	if len(normalized) >= 2 && normalized[1] == ':' {
		return "", false // 盘符（跨平台判据，不依赖运行时 OS）
	}
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "" || normalized == "." {
		return "", false
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", false // 相对路径不得向外逃逸
		}
	}
	return normalized, true
}

// ---------------------------------------------------------------------------
// 落盘 / 读回（Router 面：走既有 withJSONRepositoryAt 口径）
// ---------------------------------------------------------------------------

// projectKnownPath 返回项目摘要渲染件的物理路径：
// `<项目根>/.seelex/board/project-known.md`。
//
// 目录派生复用 `ProjectBoardDir`——**不另写一份目录派生**（契约 §8）：摘要件与
// 两块投影同一个目录、同一条生命周期（可删、可重建、不进上下文）。
func projectKnownPath(projectRoot string) string {
	return filepath.Join(ProjectBoardDir(projectRoot), ProjectBoardKnownFile)
}

// writeProjectKnown 落一份渲染件（空内容不落盘；同路径独立锁 + 原子发布）。
func writeProjectKnown(projectRoot string, data []byte) error {
	if err := validateProjectRoot(projectRoot); err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("session storage: %s 内容为空，拒绝落盘（空文件不是一份渲染件）", ProjectBoardKnownFile)
	}
	path := projectKnownPath(projectRoot)
	lock := projectBoardLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := storeroot.WriteAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("session storage: 发布 %s: %w", ProjectBoardKnownFile, err)
	}
	return nil
}

// readProjectKnown 读回渲染件（不存在 → `fs.ErrNotExist`；空文件 → 显式报错）。
func readProjectKnown(projectRoot string) ([]byte, error) {
	if err := validateProjectRoot(projectRoot); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(projectKnownPath(projectRoot))
	if err != nil {
		return nil, err // *fs.PathError，errors.Is(err, fs.ErrNotExist) 口径不变
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("session storage: %s 内容为空（空文件不是一份渲染件）", ProjectBoardKnownFile)
	}
	return data, nil
}

// WriteProjectKnown 原子写一份项目摘要渲染件（projectRoot = 项目根目录）。
//
// 空根 / 空内容**显式报错**，不产空文件；落点是 `<项目根>/.seelex/board/`（与投影
// 两块同一个目录：可提交件放一起，外部 agent 一眼看全）。
func (router *Router) WriteProjectKnown(projectRoot string, data []byte) error {
	if err := validateProjectRoot(projectRoot); err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("session storage: %s 内容为空，拒绝落盘（空文件不是一份渲染件）", ProjectBoardKnownFile)
	}
	return writeProjectKnown(projectRoot, data)
}

// ReadProjectKnown 读回项目摘要渲染件；从未渲染过 ⇒ `fs.ErrNotExist`；空根显式报错。
func (router *Router) ReadProjectKnown(projectRoot string) ([]byte, error) {
	if err := validateProjectRoot(projectRoot); err != nil {
		return nil, err
	}
	return readProjectKnown(projectRoot)
}
