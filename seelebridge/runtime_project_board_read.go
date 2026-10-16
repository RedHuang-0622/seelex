package seelebridge

// runtime_project_board_read.go — 项目看板的**按需读取面**（M4）。
//
// 落点（2026-10-15 起）：`<项目根>/.seelex/board/<会话 ID>/{worktable,teamwork}.jsonl`
// **按会话分目录**（一个会话一份文件 ⇒ 不存在两个会话往同一个文件里交错追加；
// 汇总人读件 `BOARD.md` 与项目摘要 `project-known.md` 在看板根下，不分会话）。
// 投影就在**绑定工程之内**，所以这把工具的理由不再是"数据根在工程之外读不到"，而是
// 两条**仍然成立**的理由：
//
//  1. Seelex 自己的文件读面（`workspace.ReadFile` / `ListTree`）把 `.seelex` 整个列为
//     忽略目录——那是有意的：默认数据根恰好也叫 `.seelex/`（可能就在工程根下），
//     整体放开会把 `lock.owner`、会话正文、媒体一起放给模型。既不放宽忽略名单，
//     就得有一条**专用**的读法；
//  2. 这条读法要**有界 + 可归属**：按会话取文件、跨会话汇总时按会话目录数/行数截断并
//     显式标注（`.seelex/board/` 里的 jsonl 是追加日志，直接 read_file 会整份拖进上下文）。
//
// 外部 agent（codex / claude code）不走这条路：它们直接读盘上的
// `<项目根>/.seelex/board/BOARD.md`（汇总人读件）与各会话目录下的 jsonl——那正是
// "搬到项目目录 + 按会话分目录"的用意。
//
// 三条口径（与契约 §6 一致）：
//
//   - **只读**：本文件不写任何东西，也没有写入口；
//   - **有界**（I5）：行数有缺省与硬上限，越上限**显式报错**（不静默截断）；真到上限
//     就截断并**显式标注** truncated（"读到的比在册的少"必须让人看见）；
//   - **不进上下文**（I7）：它只在被调用时返回文本，不参与任何前缀装配。

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/store"
)

// ProjectBoardReader 是看板投影的只读面（`*store.Router` 天然满足它）。
type ProjectBoardReader interface {
	// ReadProjectBoard 是一个项目内**全部会话**的聚合读（bool = 是否截断）。
	ReadProjectBoard(projectRoot string, options store.ProjectBoardReadOptions) ([]store.ProjectBoardRow, bool, error)
	// ReadProjectBoardRows 是单会话单文件的**折叠读**（文件不存在 ⇒ 空投影）。
	ReadProjectBoardRows(projectRoot, sessionID, file string) ([]store.ProjectBoardRow, error)
}

const (
	// ProjectBoardReadDefaultLimit 是单次读取的缺省行数上限（一屏能读完的量）。
	ProjectBoardReadDefaultLimit = 100
	// ProjectBoardReadMaxLimit 是行数**硬上限**：要更多就显式报错（I5：有界，不静默截断）。
	ProjectBoardReadMaxLimit = 500
	// ProjectBoardReadSessionAll 是"本项目内跨会话聚合"的会话位取值。
	ProjectBoardReadSessionAll = "all"
	// ProjectBoardReadFileWorktable / Teamwork 是 file 入参的两个简名。
	ProjectBoardReadFileWorktable = "worktable"
	ProjectBoardReadFileTeamwork  = "teamwork"
)

// ProjectBoardReadRequest 是 `project_board_read` 的入参（都可缺省）。
type ProjectBoardReadRequest struct {
	// Session：缺省 = 当前会话（读它自己的目录）；`all` = 本项目内跨会话聚合。
	Session string `json:"session"`
	// File：缺省 = 两份都读；worktable | teamwork。
	File string `json:"file"`
	// ProjectRoot：缺省 = 当前会话所属工作区的项目根
	// （落点 `<project_root>/.seelex/board/<会话 ID>/`）。
	ProjectRoot string `json:"project_root"`
	// Limit：缺省 100；超过硬上限显式报错。
	Limit int `json:"limit"`
}

// ProjectBoardReadResult 是读取面的取数结果（渲染由调用方负责）。
type ProjectBoardReadResult struct {
	// ProjectRoot 是生效的项目根（看板落点 = `<ProjectRoot>/.seelex/board/<会话 ID>/`）。
	ProjectRoot string
	// Session 是生效的会话位（"all" 或某会话 id）。
	Session string
	// Files 是本次真正读过的白名单文件名（按请求顺序）。
	Files []string
	// Rows 是按 (Kind, Key, Scope.Session) 稳定排序、已折叠的投影行。
	Rows []store.ProjectBoardRow
	// Truncated 表示"读到的比在册的少"（行数触顶）。
	Truncated bool
}

// ReadProjectBoard 按请求取数：**只取数与校验边界**，不渲染、不判定任何语义。
//
// defaultProjectRoot / defaultSession 由调用方（工具 handler）给出"当前会话"这一上下文：
// 这里不猜——猜出来的落点会把别的项目的行读给模型。
func ReadProjectBoard(
	reader ProjectBoardReader,
	request ProjectBoardReadRequest,
	defaultProjectRoot, defaultSession string,
) (ProjectBoardReadResult, error) {
	if reader == nil {
		return ProjectBoardReadResult{}, errors.New("project board: 读取面未装配")
	}
	projectRoot := strings.TrimSpace(request.ProjectRoot)
	if projectRoot == "" {
		projectRoot = strings.TrimSpace(defaultProjectRoot)
	}
	if projectRoot == "" {
		return ProjectBoardReadResult{}, errors.New("project board: 该会话没有工作区绑定（没有看板落点），请显式给出 project_root")
	}
	sessionID := strings.TrimSpace(request.Session)
	if sessionID == "" {
		sessionID = strings.TrimSpace(defaultSession)
	}
	limit := request.Limit
	if limit <= 0 {
		limit = ProjectBoardReadDefaultLimit
	}
	if limit > ProjectBoardReadMaxLimit {
		return ProjectBoardReadResult{}, fmt.Errorf(
			"project board: limit 上限 %d 行（收到 %d）：请调小，或分次按会话读", ProjectBoardReadMaxLimit, limit)
	}
	files, err := projectBoardReadFiles(request.File)
	if err != nil {
		return ProjectBoardReadResult{}, err
	}

	result := ProjectBoardReadResult{ProjectRoot: projectRoot, Session: sessionID, Files: files}
	if sessionID == ProjectBoardReadSessionAll {
		for _, file := range files {
			rows, truncated, readErr := reader.ReadProjectBoard(projectRoot, store.ProjectBoardReadOptions{
				File:    file,
				MaxRows: limit,
			})
			if readErr != nil {
				return ProjectBoardReadResult{}, readErr
			}
			result.Rows = append(result.Rows, rows...)
			result.Truncated = result.Truncated || truncated
		}
	} else {
		if sessionID == "" {
			return ProjectBoardReadResult{}, errors.New("project board: 没有可用会话（既没给 session，也没有当前会话）")
		}
		for _, file := range files {
			rows, readErr := reader.ReadProjectBoardRows(projectRoot, sessionID, file)
			if readErr != nil {
				return ProjectBoardReadResult{}, readErr
			}
			result.Rows = append(result.Rows, rows...)
		}
	}

	sortProjectBoardRows(result.Rows)
	if len(result.Rows) > limit {
		result.Rows = result.Rows[:limit]
		result.Truncated = true
	}
	return result, nil
}

// projectBoardReadFiles 把 file 入参归一化成白名单文件名清单（空 = 两份都读）。
func projectBoardReadFiles(file string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(file)) {
	case "":
		return []string{store.ProjectBoardWorkFile, store.ProjectBoardTeamworkFile}, nil
	case ProjectBoardReadFileWorktable, store.ProjectBoardWorkFile:
		return []string{store.ProjectBoardWorkFile}, nil
	case ProjectBoardReadFileTeamwork, store.ProjectBoardTeamworkFile:
		return []string{store.ProjectBoardTeamworkFile}, nil
	default:
		return nil, fmt.Errorf(
			"project board: file = %q 不在白名单（%s / %s，留空 = 两份都读）",
			file, ProjectBoardReadFileWorktable, ProjectBoardReadFileTeamwork)
	}
}

// sortProjectBoardRows 按 (Kind, Key, Scope.Session) 稳定排序：
// 同一份输入必得同一份输出（读面给人的东西不该每次换顺序）。
func sortProjectBoardRows(rows []store.ProjectBoardRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		if rows[i].Key != rows[j].Key {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Scope.Session < rows[j].Scope.Session
	})
}
