package seelebridge

// runtime_project_board_read.go — 项目看板的**按需读取面**（M4）。
//
// 为什么需要一把专用工具（而不是让模型用 read_file / grep_search / glob）：
// 投影落点在**数据根**（`<json_root>/project-board/...`），而数据根可以在绑定工程
// 之外——模型侧那几个读工具的作用域声明是 "inside the bound project"，读不到它。
// 于是这里给一把**只读、有界、按会话/项目聚合**的工具 `project_board_read`。
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

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ProjectBoardReader 是看板投影的只读面（`*sessionstore.Router` 天然满足它）。
type ProjectBoardReader interface {
	// ReadProjectBoard 是项目内**跨会话**聚合读（bool = 是否截断）。
	ReadProjectBoard(projectID string, options sessionstore.ProjectBoardReadOptions) ([]sessionstore.ProjectBoardRow, bool, error)
	// ReadProjectBoardRows 是单会话单文件的**折叠读**（文件不存在 ⇒ 空投影）。
	ReadProjectBoardRows(projectID, sessionID, file string) ([]sessionstore.ProjectBoardRow, error)
}

const (
	// ProjectBoardReadDefaultLimit 是单次读取的缺省行数上限（一屏能读完的量）。
	ProjectBoardReadDefaultLimit = 100
	// ProjectBoardReadMaxLimit 是行数**硬上限**：要更多就显式报错（I5：有界，不静默截断）。
	ProjectBoardReadMaxLimit = 500
	// ProjectBoardReadMaxSessions 是跨会话聚合时的会话数上限（复用聚合读的既有口径）。
	ProjectBoardReadMaxSessions = 20
	// ProjectBoardReadSessionAll 是"本项目内跨会话聚合"的会话位取值。
	ProjectBoardReadSessionAll = "all"
	// ProjectBoardReadFileWorktable / Teamwork 是 file 入参的两个简名。
	ProjectBoardReadFileWorktable = "worktable"
	ProjectBoardReadFileTeamwork  = "teamwork"
)

// ProjectBoardReadRequest 是 `project_board_read` 的入参（三个位都可缺省）。
type ProjectBoardReadRequest struct {
	// Session：缺省 = 当前会话；`all` = 本项目内跨会话聚合。
	Session string `json:"session"`
	// File：缺省 = 两份都读；worktable | teamwork。
	File string `json:"file"`
	// Project：缺省 = 当前会话所属工作区 ID。
	Project string `json:"project"`
	// Limit：缺省 100；超过硬上限显式报错。
	Limit int `json:"limit"`
}

// ProjectBoardReadResult 是读取面的取数结果（渲染由调用方负责）。
type ProjectBoardReadResult struct {
	Project string
	// Session 是生效的会话位（"all" 或某会话 id）。
	Session string
	// Files 是本次真正读过的白名单文件名（按请求顺序）。
	Files []string
	// Rows 是按 (Kind, Key, Scope.Session) 稳定排序、已折叠的投影行。
	Rows []sessionstore.ProjectBoardRow
	// Truncated 表示"读到的比在册的少"（会话数或行数触顶）。
	Truncated bool
}

// ReadProjectBoard 按请求取数：**只取数与校验边界**，不渲染、不判定任何语义。
//
// defaultProject / defaultSession 由调用方（工具 handler）给出"当前会话"这一上下文：
// 这里不猜——猜出来的作用域会把别的项目的行读给模型。
func ReadProjectBoard(
	reader ProjectBoardReader,
	request ProjectBoardReadRequest,
	defaultProject, defaultSession string,
) (ProjectBoardReadResult, error) {
	if reader == nil {
		return ProjectBoardReadResult{}, errors.New("project board: 读取面未装配")
	}
	projectID := strings.TrimSpace(request.Project)
	if projectID == "" {
		projectID = strings.TrimSpace(defaultProject)
	}
	if projectID == "" {
		return ProjectBoardReadResult{}, errors.New("project board: 该会话没有工作区绑定，请显式给出 project")
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

	result := ProjectBoardReadResult{Project: projectID, Session: sessionID, Files: files}
	if sessionID == ProjectBoardReadSessionAll {
		for _, file := range files {
			rows, truncated, readErr := reader.ReadProjectBoard(projectID, sessionstore.ProjectBoardReadOptions{
				File:        file,
				MaxSessions: ProjectBoardReadMaxSessions,
				MaxRows:     limit,
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
			rows, readErr := reader.ReadProjectBoardRows(projectID, sessionID, file)
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
		return []string{sessionstore.ProjectBoardWorkFile, sessionstore.ProjectBoardTeamworkFile}, nil
	case ProjectBoardReadFileWorktable, sessionstore.ProjectBoardWorkFile:
		return []string{sessionstore.ProjectBoardWorkFile}, nil
	case ProjectBoardReadFileTeamwork, sessionstore.ProjectBoardTeamworkFile:
		return []string{sessionstore.ProjectBoardTeamworkFile}, nil
	default:
		return nil, fmt.Errorf(
			"project board: file = %q 不在白名单（%s / %s，留空 = 两份都读）",
			file, ProjectBoardReadFileWorktable, ProjectBoardReadFileTeamwork)
	}
}

// sortProjectBoardRows 按 (Kind, Key, Scope.Session) 稳定排序：
// 同一份输入必得同一份输出（读面给人的东西不该每次换顺序）。
func sortProjectBoardRows(rows []sessionstore.ProjectBoardRow) {
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
