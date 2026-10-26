package review

// show_files.go 是 **show_files 工具的形状**（P21-4「域方法随类型走」：从
// `backend/engine/assemble/review_files.go` 归位到这里；plan §2.5 的 `review_files`
// → `backend/tool/review`）。
//
// 分工（与 `Requests` 的落点分开）：**工具形状**（名字 / 描述 / JSON schema / 参数校验 /
// 返回值形状）是"模型看得见的那一层"，住在本域；**落点**（会话快照与沙盒边界）由宿主实现
// `ShowFilesPort` 注入——逐份过 workspace 域裁决 → 记为会话作用域的可重放请求 → 回契约收据。
// 装配点（`wiring/registry`）拿本文件的三件套 + handler 注册进工具面。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ShowFilesToolName 是工具在模型面上的名字。
const ShowFilesToolName = "show_files"

// ShowFilesPort 是「agent 请求 GUI 在侧边资源管理器打开待审阅文件」这一能力的**落点端口**
// （application 侧实现：逐份过 workspace 域裁决 → 记为会话作用域的可重放请求 → 回契约收据）。
type ShowFilesPort interface {
	RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error)
}

// ToolRegistrar 是 show_files 注册动作需要的最小工具面。
//
// *backend/engine/assemble.Runtime 结构满足该端口；wiring/registry 与迁移期
// assemble 兼容入口都只调用本包的 RegisterTool，避免各自重复拼工具定义。
type ToolRegistrar interface {
	RegisterTool(
		name, description string,
		inputSchema map[string]interface{},
		handler func(context.Context, string) (string, error),
	)
}

// ShowFilesReceipt 是 show_files 的工具返回值形状（模型面 JSON）。
//
// 它必须回契约：工具结果进对话、被后续回合读到。所以"请求了几份 / 哪几份打开了 /
// 哪几份被跳过、为什么"都在这里——沉默地少开几份会让模型以为都开了。
type ShowFilesReceipt struct {
	// Requested 是本次**请求**的份数（含被跳过的）。
	Requested int `json:"requested"`
	// Opened 是真正进入待审阅清单的份数（= len(Files)）。
	Opened int `json:"opened"`
	// Files 是被接受的清单（路径与工作区域归一化后的形式一致）。
	Files []dto.ReviewFileRef `json:"files,omitempty"`
	// Skipped 是被跳过的清单与原因（sandbox / sensitive / binary / missing /
	// duplicate，词表在 dto）。
	Skipped []dto.ReviewSkip `json:"skipped,omitempty"`
	// RequestID 是这一次召唤的身份（同一次调用 = 同一个 ID；前端按它判定补放）。
	RequestID string `json:"request_id,omitempty"`
}

// ShowFilesDescription 是工具描述（模型面；上限用契约常量拼进去）。
//
// 语义：**一次调用 = 一次批处理召唤**（不是一份文件一次调用）。单次上限由契约常量
// `dto.MaxReviewFilesPerRequest` 给出（10 份，用户口径 2026-10-12）；超限**显式报错**并让模型
// 自己分批，绝不静默截断——静默截断会让"叫了 12 份"看起来像"12 份都在等待审阅"。
func ShowFilesDescription() string {
	return fmt.Sprintf("Ask the GUI to open these files for the human to review, in the side explorer's file-preview drawer. One call = one batch summon, at most %d files; listing more is an explicit error (batch them yourself, never rely on truncation). Only relative workspace paths are accepted: paths escaping the workspace, absolute paths, symlinks, ignored directories and sensitive file names are skipped and reported back by reason. The tool returns a receipt (requested / opened / skipped with reasons) and never returns file bytes — the drawer loads content through the existing read-only preview channel, so the same containment and sensitivity boundary applies.", dto.MaxReviewFilesPerRequest)
}

// ShowFilesSchema 是工具入参的 JSON schema（一份文件一个对象；`files` 数组带上限）。
func ShowFilesSchema() map[string]interface{} {
	fileSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type": "string", "description": "工作区相对路径（/ 分隔；越界/绝对路径/符号链接/忽略目录/敏感文件名会被跳过并如实回报）",
			},
			"note": map[string]interface{}{
				"type": "string", "description": "为什么请人看它（写进 chip 的悬停提示，不携带文件内容）",
			},
			"start_line": map[string]interface{}{
				"type": "integer", "minimum": 1, "description": "可选关注区间起点（1 起，闭区间；只用于滚动/高亮定位）",
			},
			"end_line": map[string]interface{}{
				"type": "integer", "minimum": 1, "description": "可选关注区间终点（须 ≥ start_line，否则视为不指定区间）",
			},
		},
		"required": []string{"path"},
	}
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"files": map[string]interface{}{
				"type":     "array",
				"minItems": 1,
				"maxItems": dto.MaxReviewFilesPerRequest,
				"items":    fileSchema,
				"description": fmt.Sprintf(
					"待审阅文件清单；一次调用 = 一次批处理召唤，最多 %d 份（超限会报错，请自己分批）",
					dto.MaxReviewFilesPerRequest),
			},
		},
		"required": []string{"files"},
	}
}

// RegisterShowFilesTool 是 show_files 的唯一注册实现：把本域已有的名字、描述、schema
// 与 handler 交给装配方。它不持有状态，也不执行工具。
func RegisterShowFilesTool(registrar ToolRegistrar, port ShowFilesPort) {
	if registrar == nil || port == nil {
		return
	}
	registrar.RegisterTool(
		ShowFilesToolName,
		ShowFilesDescription(),
		ShowFilesSchema(),
		ShowFilesHandler(port),
	)
}

// ShowFilesHandler 是工具处理函数：只做两件事——形状校验（非空 / 上限）与把清单交给落点。
//
// 沙盒边界（containment / 敏感过滤 / 二进制 / 上限）一律由落点经 **workspace 域**裁决，
// 本函数不读磁盘、不超过落点另开一条通道——新增这个入口因此不可能放宽既有边界。
func ShowFilesHandler(port ShowFilesPort) func(ctx context.Context, argsJSON string) (string, error) {
	return func(ctx context.Context, argsJSON string) (string, error) {
		var args struct {
			Files []dto.ReviewFileRef `json:"files"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("show_files: invalid JSON: %w", err)
		}
		if len(args.Files) == 0 {
			return "", errors.New("show_files: files 不能为空（一次调用 = 一次批处理召唤）")
		}
		if len(args.Files) > dto.MaxReviewFilesPerRequest {
			return "", fmt.Errorf(
				"show_files: 一次最多召唤 %d 份文件，本次请求 %d 份；请分成多次调用（超限不截断）",
				dto.MaxReviewFilesPerRequest, len(args.Files))
		}
		request, err := port.RequestFileReview(ctx, args.Files)
		if err != nil {
			return "", err
		}
		receipt := ShowFilesReceipt{
			Requested: len(args.Files),
			Opened:    len(request.Files),
			Files:     request.Files,
			Skipped:   request.Skipped,
			RequestID: request.RequestID,
		}
		encoded, err := json.Marshal(receipt)
		return string(encoded), err
	}
}
