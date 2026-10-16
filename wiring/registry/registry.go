// Package registry 注册 Seelex 自己生产的那一族工具与能力。
//
// 它是组合根的"工具面装配表"：账号/会话/看板/作业/技能/插件的工具都由它登记，登记
// 顺序与工具表内容是对外行为（工具名、schema、可见性门控都从这里出去）。它只做
// **登记**，不持有状态、不启动任何后台循环：handler 一律路由到 application.Service
// 与 assemble.Runtime 的既有方法面。
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/core"
	seelebridge "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	mcpconfig "github.com/RedHuang-0622/seelex/backend/engine/mcp/mcpstack/config"
	"github.com/RedHuang-0622/seelex/backend/engine/plugin"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	coretask "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/backend/tool/websearch"
	"github.com/RedHuang-0622/seelex/internal/bootseed"
	"github.com/RedHuang-0622/seelex/middle/view/board"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
	"github.com/RedHuang-0622/seelex/store"
	"github.com/RedHuang-0622/seelex/wiring/adapters"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

// pluginPromptEngine 是「能被推 system prompt 的引擎」这一格能力面（RegisterProductTools
// 与 ApplyPluginPrompt 只用到它）：接口放在消费方，具体 Session 由组合根注入。
type pluginPromptEngine interface {
	SetSystemPrompt(string)
}

func RegisterContextReadTools(runtime *seelebridge.Runtime, app *application.Service) {
	readResultSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"result_ref": map[string]interface{}{"type": "string"},
			"offset":     map[string]interface{}{"type": "integer", "minimum": 0},
			"limit":      map[string]interface{}{"type": "integer", "minimum": 1, "maximum": core.Limits().MaxReferencePageSize},
			"contains":   map[string]interface{}{"type": "string"},
		},
		"required": []string{"result_ref"},
	}
	readPlanSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"plan_ref": map[string]interface{}{"type": "string"},
			"node_ids": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
	}
	runtime.RegisterTool("read_tool_result", "Read an immutable stored tool result by reference with bounded pagination or line filtering.", readResultSchema, app.ReadToolResultHandler)
	runtime.RegisterTool("read_plan", "Read selected nodes from the durable canonical Plan without changing Plan state.", readPlanSchema, app.ReadPlanHandler)
	readCompressedTurnSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"segment_id": map[string]interface{}{"type": "string"},
			"offset":     map[string]interface{}{"type": "integer", "minimum": 0},
			"limit":      map[string]interface{}{"type": "integer", "minimum": 1, "maximum": core.Limits().MaxReferencePageSize},
			"contains":   map[string]interface{}{"type": "string"},
		},
		"required": []string{"segment_id"},
	}
	runtime.RegisterTool("read_compressed_turn", "Read the original messages of a compressed turn segment by segment_id (from a compact frame Summary/Evidence) with bounded pagination or line filtering. Use it when the compressed summary lacks detail — the original content is durably stored and loss is reversible.", readCompressedTurnSchema, app.ReadCompressedTurnHandler)
	// search_history：压缩栈帧是语义索引，检索在其范围内查真实聊天记录
	// （seelexctx/search：memory.Select 选相关帧 → 帧 [From..To] 单元范围
	// 从事件库读回记录 → token 预算内内联返回）。
	searchHistorySchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "检索关键词（与压缩段摘要词法匹配，可中英文）"},
			"limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": seelexctxsearch.MaxLimit},
		},
		"required": []string{"query"},
	}
	runtime.RegisterTool("search_history", "Search the session's long-term history: select relevant compressed segments (compact stack index) and read back the real chat records in their unit ranges, bounded by a token budget. Use it when the current context lacks relevant history the user mentioned earlier (past decisions, requirements, tool outputs).", searchHistorySchema, app.SearchHistoryHandler)
	// compact_context：把 /compact 这条手动压缩入口同样交给模型——它可以在
	// 上下文逼近上限、或开始一段长任务前主动收拢上下文。落点与引擎自动压缩、
	// 命令 /compact 完全同一条（context_runtime.CompactContextNow），原始轮次
	// 仍完整留在会话存储，细节按引用回读。
	compactContextSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{"type": "string", "description": "为什么现在压缩（写入压缩记录，供审计与回看）"},
		},
	}
	runtime.RegisterTool("compact_context", "Compact this session's context now: fold the variable transcript into a bounded checkpoint frame (stable prefix + task evidence summary + plan + current request) while every original turn stays readable by reference. Call it before a long multi-step task or when the context is close to the provider limit; it is not a substitute for finishing the current step.", compactContextSchema, app.CompactContextHandler)
}

// RegisterProjectRefreshTool 注册 project_refresh 产品工具：扫描模块文档目录 +
// 模块元数据（module_dotting.json）+ 可选手工说明 seelex.project.md，构建
// 项目级模块语义知识（ProjectKnowledge，plan.md §3.7.1）。来源 hash 未变时
// 直接复用（内容版本化）；重建失败保留上一版本（可回退）。
//
// M5：成功刷新后**追加一步渲染**——把 `result.Record` 确定性渲染成
// `project-known.md`（`store.ProjectKnownMarkdown` + `WriteProjectKnown`），
// 返回体里带 known_written / known_bytes。渲染**不改判定**：谁能产出
// ProjectRecord 仍只有 RefreshProjectKnowledge 这条链。
//
// 仍然是**被动**能力（契约 §6.1 / R6）：不新增工具、不在装配期构建、不自动刷新；
// 只有这个工具被显式调用时才渲染落盘（无显式调用 → 不产文件）。
// RegisterProjectBoardReadTool 注册项目看板的**只读按需读取面**（M4）。
//
// 为什么必须有这把工具（理由在 M7 落点搬进项目目录后换过，结论不变）：投影落在
// `<项目根>/.seelex/board/`，而 Seelex 的工作区读面把 **`.seelex` 整个列为忽略目录**
// （默认数据根恰好也叫 `.seelex/`、可能就在工程根下，整体放开会把 `lock.owner`、会话正文、
// 媒体一起放给模型）——`read_file` / `grep_search` / `glob` 读不到它。既不放宽忽略名单，
// 就读法另给一把：只读、有界、按会话/项目聚合（取数一律走存储面的折叠读，不在工具里重算
// 任何语义）。外部 agent（codex / claude code）不走这条路，它们直接读盘上的 `BOARD.md`。
//
// **注册门在调用点**（看 run 里的 `if runtimeLimits.ProjectBoard.Enabled`）：能力关 ⇒
// 这把工具根本不存在（零读取面），而不是"注册了但返回错误"。
func RegisterProjectBoardReadTool(runtime *seelebridge.Runtime, st *store.Router, app *application.Service) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session": map[string]interface{}{
				"type":        "string",
				"description": "会话 ID；缺省 = 当前会话；all = 本项目内跨会话聚合（有界）",
			},
			"file": map[string]interface{}{
				"type":        "string",
				"enum":        []string{seelebridge.ProjectBoardReadFileWorktable, seelebridge.ProjectBoardReadFileTeamwork},
				"description": "读哪一份投影：worktable（会话的任务行）/ teamwork（里程碑与工作项）；缺省 = 两份都读",
			},
			"project_root": map[string]interface{}{
				"type":        "string",
				"description": "项目根目录（看板落点 = <project_root>/.seelex/board）；缺省 = 当前会话所属工作区的根",
			},
			"limit": map[string]interface{}{
				"type": "integer", "minimum": 1, "maximum": seelebridge.ProjectBoardReadMaxLimit,
				"description": fmt.Sprintf("返回行数上限（缺省 %d，硬上限 %d；超限显式报错）",
					seelebridge.ProjectBoardReadDefaultLimit, seelebridge.ProjectBoardReadMaxLimit),
			},
		},
	}
	handler := func(ctx context.Context, argsJSON string) (string, error) {
		var request seelebridge.ProjectBoardReadRequest
		if strings.TrimSpace(argsJSON) != "" {
			if err := json.Unmarshal([]byte(argsJSON), &request); err != nil {
				return "", fmt.Errorf("project_board_read: %w", err)
			}
		}
		// 会话位取"完成段当场"的会话（后台会话的工具调用要落在它自己身上），
		// ctx 里没有才退到当前视图会话。
		sessionID := strings.TrimSpace(coretask.SessionIDFromContext(ctx))
		if sessionID == "" {
			sessionID = app.Snapshot().Session.ID
		}
		result, err := seelebridge.ReadProjectBoard(st, request, app.ProjectRootFor(sessionID), sessionID)
		if err != nil {
			return "", fmt.Errorf("project_board_read: %w", err)
		}
		encoded, err := json.Marshal(map[string]interface{}{
			"project_root": result.ProjectRoot,
			"session":      result.Session,
			"files":        result.Files,
			"count":        len(result.Rows),
			"truncated":    result.Truncated,
			"rows":         projectBoardReadRows(result.Rows),
		})
		if err != nil {
			return "", fmt.Errorf("project_board_read: %w", err)
		}
		return string(encoded), nil
	}
	runtime.RegisterTool(
		"project_board_read",
		"Read the derived project board projection on demand: worktable.jsonl (todo/task rows) and "+
			"teamwork.jsonl (milestones and their work items) under "+
			"<project_root>/.seelex/board/<session id>/ (one directory per session); the aggregated "+
			"human-readable board is <project_root>/.seelex/board/BOARD.md. "+
			"Read-only and bounded, filterable by session. Use it when the user asks what a session or the "+
			"project is doing, or when you pick up another session's tasks; the workspace read tools refuse "+
			"`.seelex` (it can be the data root), so this is the bounded reader for that directory. "+
			"The projection is derived data (rebuildable), never a fact source: decisions are made from the "+
			"live session/team state.",
		schema, handler)
}

func RegisterProjectRefreshTool(runtime *seelebridge.Runtime, st *store.Router, app *application.Service) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"project_root": map[string]interface{}{"type": "string", "description": "项目根目录（默认当前工作目录）"},
			"force":        map[string]interface{}{"type": "boolean", "description": "强制重建，忽略来源 hash 复用"},
		},
	}
	handler := func(ctx context.Context, argsJSON string) (string, error) {
		var input struct {
			ProjectRoot string `json:"project_root"`
			Force       bool   `json:"force"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("project_refresh: %w", err)
		}
		builder := store.NewProjectKnowledgeBuilder(store.ProjectKnowledgeSources{Root: input.ProjectRoot})
		// 项目级记录的**键是工作区 ID**（`ws-<nano>`），不是扫描根路径：读面
		// （下方 RuntimeDeps.ProjectKnowledge 的 store.LoadProjectRecord(store.Workspace())）
		// 用的就是这个键。写键≠读键会让记录永远读不到（G5 断点②），
		// 所以这里显式取一次工作区 ID 传进去，而不是让存储层去猜。
		result, err := store.RefreshProjectKnowledge(ctx, st, st.Workspace(), builder, input.Force)
		if err != nil {
			// 首建失败（无来源 / 落盘失败）：显式报错，**不产空渲染件**。
			return "", fmt.Errorf("project_refresh: %w", err)
		}
		// M5：责任链之后**只加一步渲染**（复用上面的 RefreshProjectKnowledge 三分支，
		// 不新写第二条项目知识链；被动：只有本工具被调用时才走到这里）。
		//
		// 回退（重建失败 ⇒ 保留上一版本）时**不重写渲染件**：上一版的
		// project-known.md 就是"上一版本"的渲染，渲染不改变既有回退语义。
		knownWritten := false
		knownBytes := 0
		knownNote := ""
		if result.Fallback {
			// 不在回退分支重写：盘上的上一版渲染件就是"上一版本"。
			knownNote = "fallback：保留上一版本渲染件（未重写）"
		} else {
			markdown := store.ProjectKnownMarkdown(result.Record)
			// 渲染件与看板同一个目录：<项目根>/.seelex/board/。项目根优先取**会话绑定**
			// （与看板落点同源）；未绑定时退回本次扫描根（显式给了 project_root 的用法）。
			sessionID := strings.TrimSpace(coretask.SessionIDFromContext(ctx))
			if sessionID == "" {
				sessionID = app.Snapshot().Session.ID
			}
			renderRoot := app.ProjectRootFor(sessionID)
			if renderRoot == "" {
				renderRoot = builder.Root()
			}
			if writeErr := st.WriteProjectKnown(renderRoot, markdown); writeErr != nil {
				return "", fmt.Errorf("project_refresh: %w", writeErr)
			}
			knownWritten = true
			knownBytes = len(markdown)
		}
		payload := map[string]interface{}{
			"version":       result.Record.Version,
			"modules":       len(result.Record.Modules),
			"reused":        result.Reused,
			"fallback":      result.Fallback,
			"note":          result.Note,
			"built_at":      result.Record.BuiltAt.Format(time.RFC3339),
			"known_written": knownWritten,
			"known_bytes":   knownBytes,
			"known_file":    store.ProjectBoardKnownFile,
			"known_note":    knownNote,
		}
		encoded, err := json.Marshal(payload)
		return string(encoded), err
	}
	runtime.RegisterTool("project_refresh", "扫描项目模块文档与元数据，重建项目级模块语义知识；来源未变化时直接复用", schema, handler)
}

// ScheduledPromptExecutor 是定时提示词任务的执行器。落点与装配都从任务定义读
// （唯一一份判据在调度器契约里，本函数只实现它）：
//   - SessionID 非空 → 投递到那个既有会话（显式绑定）；
//   - SessionID 空（默认）→ **新建会话**发起，WorkspaceID 非空时把新会话装配到
//     该工作区（项目根 + 会话绑定）；
//   - PermissionTier / Plugins → 触发那次会话的权限档位与这一轮的能力包装配
//     （走 application.AssembleScheduledRun，两条路径共用）。
//
// 新建路径落到 application.StartScheduledSession：新会话在后台跑，不切用户的视图
// 指针；它自己的会话记录照常按会话存储纪律落盘。返回的落点会话号会写进任务快照
// 的 last_session_id——面板上看得到"这次跑到哪个会话去了"。
//
// 单独成函数而不是就地写成闭包：测试基座（newFullChainHarness）装的就是这一份，
// 冒烟测的才是产品的那条链。
func ScheduledPromptExecutor(app *application.Service) seelebridge.ScheduledPromptExecutor {
	return func(ctx context.Context, task seelebridge.ScheduledTaskSpec) (seelebridge.ScheduledPromptOutcome, error) {
		if bound := strings.TrimSpace(task.SessionID); bound != "" {
			// 显式绑定会话：装配同样落到那个会话（任务声明什么，就用什么跑）。
			assembled, err := app.AssembleScheduledRun(ctx, bound, task)
			if err != nil {
				return seelebridge.ScheduledPromptOutcome{}, err
			}
			if err := app.SubmitToSession(assembled, bound, task.Prompt); err != nil {
				return seelebridge.ScheduledPromptOutcome{}, fmt.Errorf("定时任务投递到绑定会话 %s 失败: %w", bound, err)
			}
			return seelebridge.ScheduledPromptOutcome{
				Message: fmt.Sprintf("已提交到绑定会话 %s（异步输出见会话记录）", bound), SessionID: bound,
			}, nil
		}
		newSessionID, err := app.StartScheduledSession(ctx, task)
		if err != nil {
			return seelebridge.ScheduledPromptOutcome{SessionID: newSessionID}, err
		}
		return seelebridge.ScheduledPromptOutcome{
			Message: fmt.Sprintf("已新建会话 %s 发起（异步输出见会话记录）", newSessionID), SessionID: newSessionID,
		}, nil
	}
}

// RegisterScheduledTaskCapability 装配定时周期任务白名单命令：
// 登记 auto_get_jobs（脚本目录或 python 不可用时跳过并告警）。prompt
// 任务执行器与状态 observer 由主流程经 Runtime.ApplyDeps 一次注入
// （对应 RuntimeDeps.ScheduledPromptExecutor / SchedulerObserver）。
func RegisterScheduledTaskCapability(runtime *seelebridge.Runtime) {
	if scriptDir, ok := config.ResolveAutoGetJobsDir(); ok {
		if python := config.ResolvePythonCommand(); python != "" {
			if err := runtime.RegisterScheduledCommand(seelebridge.ScheduledCommand{
				Key:         "auto_get_jobs",
				Label:       "BOSS直聘自动投简历",
				Description: "周期抓取招聘职位并自动筛选投递（local/tools/auto_get_jobs/main.py；需先按脚本 README 配置 .env 与 user_requirements.txt）",
				WorkingDir:  scriptDir,
				Argv:        []string{python, "main.py"},
				TimeoutSec:  30 * 60,
			}); err != nil {
				log.Printf("scheduled tasks: register auto_get_jobs: %v", err)
			}
		}
	}
}

// RegisterTaskTerminalTools 把 task_complete/task_failed/task_needs_user_decision
// 注册进 tools.Registry（taskTerminalProvider，见 backend/tool/task/terminal.go）；
// handler 内调用 TaskService.VerifyAndApply（投影 flush + 终态校验）。
func RegisterTaskTerminalTools(runtime *seelebridge.Runtime, app *application.Service) {
	runtime.RegisterTaskTerminalTools(app.TaskTerminalHandler)
}

func RegisterProductTools(runtime *seelebridge.Runtime, plugins *plugin.Manager, eng pluginPromptEngine, approval *application.ApprovalBroker, skills *skill.Registry) {
	RegisterTimeTool(runtime)
	websearch.Register(runtime, config.EnsureConfigFile(bootseed.SearchEngineConfigName, bootseed.SearchEngineConfigPack()))
	RegisterMCPServers(runtime, config.EnsureConfigFile(bootseed.MCPConfigName, bootseed.MCPConfigPack())) // mcpstack/config 加载 + Runtime 冷启动登记
	RegisterMCPLoadTool(runtime)
	RegisterPluginSwitchTools(runtime, plugins, eng)
	RegisterPluginSelfTools(runtime, plugins)
	RegisterAskApprove(runtime, approval)
}

// RegisterReviewFilesTool 注册 show_files：agent 请求 GUI 在侧边资源管理器打开
// 待审阅文件（**一次调用 = 一次批处理召唤**，单次上限 dto.MaxReviewFilesPerRequest
// = 10 份；超限显式报错，不静默截断）。
//
// 分工：工具形状与硬门在 seelebridge（RegisterReviewFilesTool），落点端口在
// application（会话作用域的可重放请求 + workspace 域统一裁决沙盒/敏感/二进制/上限）。
// 与 RegisterAskApprove 同一分法——组合根只做装配，不在这里写第二份语义。
func RegisterReviewFilesTool(runtime *seelebridge.Runtime, app *application.Service) {
	seelebridge.RegisterReviewFilesTool(runtime, app)
}

// RegisterMCPServers 将 mcp.yaml 配置的 MCP 服务器全部登记到 Runtime
// （冷启动：只存配置不连接，启动路径零 MCP 进程）。配置加载在 mcpstack/config。
// 首次需要时经内置 mcp_load 工具按名加载（spawn + initialize + tools/list），
// 加载后的 MCP 工具自动通过 mcpstack 中间件记录调用 trace。
func RegisterMCPServers(runtime *seelebridge.Runtime, mcpPath string) {
	servers := mcpconfig.Load(mcpPath)
	if len(servers) == 0 {
		return
	}

	for _, s := range servers {
		transport := s.Transport
		if transport == "" {
			if s.Command != "" {
				transport = "stdio"
			} else if s.URL != "" {
				transport = "sse"
			} else {
				fmt.Fprintf(os.Stderr, "⚠ MCP 服务器 %q：transport 未知（command 和 URL 均为空），跳过\n", s.Name)
				continue
			}
		}

		cfg := seelebridge.MCPServer{
			Name: s.Name, Transport: transport, Command: s.Command,
			Args: s.Args, Env: s.Env, URL: s.URL, ToolNotes: s.ToolNotes,
		}
		if err := runtime.RegisterLazyMCP(s.Name, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ MCP 服务器 %q 配置无效: %v\n", s.Name, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "✓ MCP 服务器 %q 已登记（冷启动；需要时调用 mcp_load 连接）\n", s.Name)
	}
}

// RegisterMCPLoadTool 注册按需加载工具：连接已登记但未连接的 MCP 服务器
// （冷启动加载点），加载后其工具立即可用（下一轮调用）。
func RegisterMCPLoadTool(runtime *seelebridge.Runtime) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"server_name": map[string]interface{}{
				"type": "string", "description": "要加载的 MCP 服务器名（mcp.yaml 的 mcp_servers 列表）",
			},
		},
		"required": []string{"server_name"},
	}
	runtime.RegisterTool(
		"mcp_load",
		"Load a registered but disconnected MCP server (cold start): connect, initialize and register its tools. Call this once before using tools from that server; loaded servers stay connected for the session. Loaded tool names become available from the next turn.",
		schema,
		func(ctx context.Context, argsJSON string) (string, error) {
			var args struct {
				ServerName string `json:"server_name"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				return "", fmt.Errorf("mcp_load: invalid arguments: %w", err)
			}
			// 冷启动握手有固定开销（spawn + initialize + tools/list），带超时保护。
			loadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			tools, err := runtime.LoadMCP(loadCtx, strings.TrimSpace(args.ServerName))
			if err != nil {
				return "", fmt.Errorf("mcp_load: %w", err)
			}
			return fmt.Sprintf("MCP 服务器 %q 已加载，工具 %d 个。可用 MCP 服务器：%v。请重新发起需要这些工具的任务。",
				args.ServerName, tools, runtime.MCPServerNames()), nil
		},
	)
}

func RegisterTimeTool(runtime *seelebridge.Runtime) {
	runtime.RegisterTool(
		"get_time",
		"获取当前日期和时间",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(context.Context, string) (string, error) {
			return fmt.Sprintf(`"%s"`, time.Now().Format("2006-01-02 15:04:05")), nil
		},
	)
}

func RegisterPluginSwitchTools(
	runtime *seelebridge.Runtime,
	plugins *plugin.Manager,
	eng pluginPromptEngine,
) {
	names := make([]interface{}, 0, len(plugins.All())+1)
	for _, p := range plugins.All() {
		names = append(names, p.Name)
	}
	names = append(names, "off")
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"plugin": map[string]interface{}{
				"type": "string", "enum": names, "description": "目标插件",
			},
		},
		"required": []string{"plugin"},
	}
	handler := func(ctx context.Context, argsJSON string) (string, error) {
		var input struct {
			Plugin string `json:"plugin"`
			Mode   string `json:"mode"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("switch_plugin: %w", err)
		}
		name := strings.ToLower(strings.TrimSpace(input.Plugin))
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(input.Mode))
		}
		if name == "off" || name == "none" || name == "" {
			if err := plugins.Deactivate(ctx); err != nil {
				return "", err
			}
		} else if err := plugins.Activate(ctx, name); err != nil {
			return "", err
		}
		ApplyPluginPrompt(eng, plugins)
		result := map[string]interface{}{
			"plugin":        runtime.ActivePlugin(),
			"visible_tools": len(runtime.VisibleTools(ctx)),
			"total_tools":   len(runtime.AllTools()),
		}
		encoded, err := json.Marshal(result)
		return string(encoded), err
	}
	runtime.RegisterTool("switch_plugin", "切换 Seelex Plugin 及其工具、Skill 和 MCP", schema, handler)

	legacySchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode": map[string]interface{}{
				"type": "string", "enum": names, "description": "目标插件（兼容 mode 名称）",
			},
		},
		"required": []string{"mode"},
	}
	runtime.RegisterTool("switch_mode", "兼容工具：等价于 switch_plugin", legacySchema, handler)
}

// RegisterPluginSelfTools 注册插件/技能/MCP 的自迭代工具族：
// 创建（写盘）→ 加载（reload）→ 列表查看，闭环让 main agent 可以自己扩展
// 运行时能力。plugin 与 skill 的写盘都**不改变运行时状态**，由 plugins_reload
// 事务式应用；MCP 走冷启动登记 + mcp_load 连接。
func RegisterPluginSelfTools(runtime *seelebridge.Runtime, plugins *plugin.Manager) {
	// ── plugin_create ──────────────────────────────────────────────
	runtime.RegisterTool(
		"plugin_create",
		"在 plugins 目录脚手架一个插件（plugin.md manifest + README + 可选 skills），写入磁盘但不立即生效；调用后请执行 plugins_reload 加载。插件名须匹配 ^[a-z0-9][a-z0-9_-]*$ 且目录不存在。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":        map[string]interface{}{"type": "string", "description": "插件名（小写字母/数字/_-）"},
				"description": map[string]interface{}{"type": "string", "description": "插件职责一句话"},
				"include":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "允许暴露的工具前缀"},
				"exclude":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "排除的工具前缀"},
				"prompt":      map[string]interface{}{"type": "string", "description": "插件系统提示词正文"},
				"skills": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":        map[string]interface{}{"type": "string"},
							"description": map[string]interface{}{"type": "string"},
							"prompt":      map[string]interface{}{"type": "string", "description": "skill 指令正文"},
						},
						"required": []string{"name"},
					},
				},
			},
			"required": []string{"name"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input plugin.CreateSpec
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("plugin_create: %w", err)
			}
			paths, err := plugins.Create(input)
			if err != nil {
				return "", fmt.Errorf("plugin_create: %w", err)
			}
			encoded, _ := json.Marshal(map[string]interface{}{
				"status": "created", "plugin": input.Name, "files": paths,
				"next": "调用 plugins_reload 使插件生效",
			})
			return string(encoded), nil
		},
	)

	// ── plugins_reload ─────────────────────────────────────────────
	runtime.RegisterTool(
		"plugins_reload",
		"重新扫描 plugins 目录并事务式应用差异（新增/删除/修改）：新增插件立即可见但不激活，修改中的当前激活插件先停用再按新定义重新激活；任一步失败回滚到上一个可用状态。用于 plugin_create / skill_create 之后的加载。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, _ string) (string, error) {
			report, err := plugins.Reload(ctx)
			if err != nil {
				return "", fmt.Errorf("plugins_reload: %w", err)
			}
			// 发现 → 读回 → 落进 yaml：刚写盘的插件（plugin_create / skill_create / 别的
			// 工具放进去的）在这一步被登记进精选目录——"创建 → 加载 → 目录跟上"是同一个闭环，
			// 别让使用者手抄插件清单。登记失败不改判定：回执里留一条读数，启动期还会重试。
			registered, rerr := plugins.RegisterDiscoveredPlugins()
			if rerr != nil {
				log.Printf("plugin: 精选目录登记失败（目录保持原样）: %v", rerr)
			}
			report.CatalogRegistered = registered
			encoded, _ := json.Marshal(report)
			return string(encoded), nil
		},
	)

	// ── plugins_list ───────────────────────────────────────────────
	runtime.RegisterTool(
		"plugins_list",
		"列出全部已加载插件及其描述、激活态、include/exclude、skills 与 MCP server 清单。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, _ string) (string, error) {
			type pluginView struct {
				Name        string   `json:"name"`
				Description string   `json:"description,omitempty"`
				Active      bool     `json:"active"`
				Include     []string `json:"include,omitempty"`
				Exclude     []string `json:"exclude,omitempty"`
				Skills      []string `json:"skills,omitempty"`
				MCPServers  []string `json:"mcp_servers,omitempty"`
			}
			active, _ := plugins.Current()
			views := make([]pluginView, 0)
			for _, p := range plugins.All() {
				view := pluginView{
					Name: p.Name, Description: p.Description, Active: active.Name == p.Name,
					Include: p.Include, Exclude: p.Exclude,
				}
				for _, s := range p.Skills {
					view.Skills = append(view.Skills, s.Name)
				}
				for _, server := range p.MCPServers {
					view.MCPServers = append(view.MCPServers, server.Name)
				}
				views = append(views, view)
			}
			encoded, err := json.Marshal(views)
			return string(encoded), err
		},
	)

	// ── skill_create ───────────────────────────────────────────────
	runtime.RegisterTool(
		"skill_create",
		"在指定插件目录下脚手架一个 skill（<plugin>/<skill>/SKILL.md，含 name/description frontmatter 与指令正文），写入磁盘但不立即生效；调用后请执行 plugins_reload 加载。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"plugin":      map[string]interface{}{"type": "string", "description": "目标插件名（须已存在）"},
				"name":        map[string]interface{}{"type": "string", "description": "skill 名"},
				"description": map[string]interface{}{"type": "string"},
				"prompt":      map[string]interface{}{"type": "string", "description": "skill 指令正文"},
			},
			"required": []string{"plugin", "name"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input plugin.SkillSpec
			var meta struct {
				Plugin string `json:"plugin"`
				plugin.SkillSpec
			}
			if err := json.Unmarshal([]byte(argsJSON), &meta); err != nil {
				return "", fmt.Errorf("skill_create: %w", err)
			}
			input = meta.SkillSpec
			path, err := plugins.CreateSkill(meta.Plugin, input)
			if err != nil {
				return "", fmt.Errorf("skill_create: %w", err)
			}
			encoded, _ := json.Marshal(map[string]interface{}{
				"status": "created", "plugin": meta.Plugin, "skill": input.Name, "file": path,
				"next": "调用 plugins_reload 使 skill 生效",
			})
			return string(encoded), nil
		},
	)

	// ── skills_list ────────────────────────────────────────────────
	runtime.RegisterTool(
		"skills_list",
		"列出全部已加载插件内的 skill（无论是否激活）及其描述与来源路径。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, _ string) (string, error) {
			type skillView struct {
				Name        string `json:"name"`
				Description string `json:"description,omitempty"`
				Plugin      string `json:"plugin,omitempty"`
				Path        string `json:"path,omitempty"`
			}
			views := make([]skillView, 0)
			for _, p := range plugins.All() {
				for _, s := range p.Skills {
					views = append(views, skillView{
						Name: s.Name, Description: s.Description, Plugin: p.Name, Path: s.FilePath,
					})
				}
			}
			encoded, err := json.Marshal(views)
			return string(encoded), err
		},
	)

	// ── mcp_create ─────────────────────────────────────────────────
	runtime.RegisterTool(
		"mcp_create",
		"冷启动登记一个 MCP server（内存态，本次会话有效）：登记后调用 mcp_load 连接并注册其工具。注意：不写入 mcp.yaml，重启后需重新登记。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":      map[string]interface{}{"type": "string", "description": "MCP server 名"},
				"transport": map[string]interface{}{"type": "string", "enum": []string{"stdio", "sse"}, "description": "stdio（本地命令）或 sse（远程 URL）"},
				"command":   map[string]interface{}{"type": "string", "description": "stdio 启动命令"},
				"args":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"env":       map[string]interface{}{"type": "object", "description": "环境变量（不含机密）"},
				"url":       map[string]interface{}{"type": "string", "description": "sse 端点 URL"},
			},
			"required": []string{"name", "transport"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input struct {
				Name      string            `json:"name"`
				Transport string            `json:"transport"`
				Command   string            `json:"command,omitempty"`
				Args      []string          `json:"args,omitempty"`
				Env       map[string]string `json:"env,omitempty"`
				URL       string            `json:"url,omitempty"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("mcp_create: %w", err)
			}
			env := make([]string, 0, len(input.Env))
			for key, value := range input.Env {
				env = append(env, key+"="+value)
			}
			cfg := seelebridge.MCPServer{
				Name: input.Name, Transport: input.Transport, Command: input.Command,
				Args: input.Args, Env: env, URL: input.URL,
			}
			if err := runtime.RegisterLazyMCP(input.Name, cfg); err != nil {
				return "", fmt.Errorf("mcp_create: %w", err)
			}
			encoded, _ := json.Marshal(map[string]interface{}{
				"status": "registered", "server": input.Name,
				"next": "调用 mcp_load 连接并注册工具",
			})
			return string(encoded), nil
		},
	)

	// ── mcp_list ───────────────────────────────────────────────────
	runtime.RegisterTool(
		"mcp_list",
		"列出已登记的 MCP server（冷启动未连接 + 已连接）及其存活状态与工具数。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, _ string) (string, error) {
			type mcpView struct {
				Name   string `json:"name"`
				Loaded bool   `json:"loaded"`
				Alive  bool   `json:"alive,omitempty"`
				Tools  int    `json:"tools,omitempty"`
			}
			names := append([]string(nil), runtime.LazyMCPServerNames()...)
			seen := make(map[string]bool, len(names))
			views := make([]mcpView, 0, len(names))
			for _, name := range names {
				seen[name] = true
				alive, tools, _ := runtime.MCPServerStatus(name)
				views = append(views, mcpView{Name: name, Loaded: alive, Alive: alive, Tools: tools})
			}
			for _, name := range runtime.MCPServerNames() {
				if seen[name] {
					continue
				}
				alive, tools, _ := runtime.MCPServerStatus(name)
				views = append(views, mcpView{Name: name, Loaded: alive, Alive: alive, Tools: tools})
			}
			encoded, err := json.Marshal(views)
			return string(encoded), err
		},
	)
}

func ApplyPluginPrompt(eng pluginPromptEngine, plugins *plugin.Manager) {
	if eng == nil {
		return
	}
	current, ok := plugins.Current()
	if !ok {
		eng.SetSystemPrompt("")
		return
	}
	eng.SetSystemPrompt(strings.TrimSpace(current.Prompt))
}

func RegisterAskApprove(runtime *seelebridge.Runtime, approval *application.ApprovalBroker) {
	runtime.RegisterTool(
		"ask_approve",
		"向用户请求操作确认。当需要执行高风险操作时调用此工具。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{"type": "string"},
				"choices": map[string]interface{}{
					"type": "array", "items": map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"question"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input struct {
				Question string   `json:"question"`
				Choices  []string `json:"choices,omitempty"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("ask_approve: %w", err)
			}
			choices := input.Choices
			if len(choices) == 0 {
				choices = []string{"Yes", "No"}
			}
			options := make([]application.InteractionOption, len(choices))
			for i, choice := range choices {
				options[i] = adapters.ApprovalOption(choice)
			}
			decision, err := approval.Request(ctx, application.ApprovalRequest{
				ID: fmt.Sprintf("ask_%d", time.Now().UnixNano()), Question: input.Question,
				// 波 4 approval 会话级归属：ask_approve 随工具 ctx 归属到
				// 发起会话（不再以进程级空归属进入视图单格）。
				SessionID: coretask.SessionIDFromContext(ctx),
				Options:   options, Risk: "low", ToolName: "ask_approve",
			})
			if err != nil || !adapters.ApprovalAccepted(decision.OptionID) {
				return `{"approved":false,"reason":"cancelled"}`, nil
			}
			encoded, err := json.Marshal(map[string]interface{}{"approved": true, "choice": decision.OptionID})
			return string(encoded), err
		},
	)
}

// guardMultiProcess 是启动期多进程闸门：数据根是单进程写者
// （store/data_root_lock.go 的 lock.owner）。默认单实例——另一个实例正在写
// 同一数据根时，这里给出**可读**的拒绝（对齐 ErrDataRootLocked 的口径，但更早、
// 更可操作）。只有 limits.runtime.allow_multi_process: true 才放行；代价见
// config/seelex.yaml 的 runtime 块（放行后失去跨进程写者串行化，一致性由使用者负责）。
//
// 这是**早期**闸门（发生在抢锁之前）：真正的不变量仍由 initStore → store
// 的数据根锁兜底，所以这里只做「能早就早」的可读拒绝，不替代锁本身。
// ProjectBoardSinkFor 是项目看板的**装配门**：按配置决定挂不挂订阅口、挂哪一档触发面。
//
// 关就是关（契约 §5.3）：Enabled=false ⇒ 返回 (nil, 空收尾, nil)——工具完成段判空后
// 什么都不做，一个文件都不写，读取面也不注册。开启时先把 hook 取值按**唯一词表**
// （board.ProjectBoardHookModes）校验：拼错的名字**显式启动失败**，不静默降级成 off
// ——后者会把"开了但没生效"变成一桩只能靠读代码才能解的悬案。
//
// 取数依赖一律用 application 侧的只读口（WorkTableRowsFor / ProjectBoardTeamPlanFor /
// ProjectBoardTeamStateFor / ProjectRootFor）：同步器不重算任何状态（I3）。
func ProjectBoardSinkFor(
	limits limit.ProjectBoardLimits,
	st board.ProjectBoardStore,
	workTableRows func(sessionID string) []core.WorkItem,
	teamPlanFor func(sessionID string) *store.TeamworkPlan,
	teamStateFor func(sessionID string) *store.TeamworkState,
	projectRootFor func(sessionID string) string,
) (board.ProjectBoardSink, func(), error) {
	if !limits.Enabled {
		return nil, func() {}, nil
	}
	hook, err := projectBoardHookMode(limits.Hook)
	if err != nil {
		return nil, func() {}, err
	}
	syncer := board.NewProjectBoardSyncer(board.ProjectBoardDeps{
		Store:          st,
		WorkTableRows:  workTableRows,
		TeamPlanFor:    teamPlanFor,
		TeamStateFor:   teamStateFor,
		ProjectRootFor: projectRootFor,
		Triggers:       board.TriggerFor(hook),
		FlushIdle:      time.Duration(limits.FlushIdleMS) * time.Millisecond,
	})
	// 收尾就是同步器自己的 Close（幂等）：终止唯一 flush goroutine，退出前补一次落盘。
	return syncer, syncer.Close, nil
}

func PluginByName(plugins []plugin.Plugin, name string) (plugin.Plugin, error) {
	for _, p := range plugins {
		if p.Name == name {
			return p, nil
		}
	}
	return plugin.Plugin{}, fmt.Errorf("plugin %q not found", name)
}

func RegisterGoalTools(runtime *seelebridge.Runtime, app *application.Service) {
	beginSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":        map[string]interface{}{"type": "string", "description": "goal 标题"},
			"statement":    map[string]interface{}{"type": "string", "description": "目标正文（可选）"},
			"acceptance":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "完成条件（可选）"},
			"out_of_scope": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "非目标范围（可选）"},
		},
		"required": []string{"title"},
	}
	updateSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"progress_kind":    map[string]interface{}{"type": "string", "description": "milestone|finding|decision|risk"},
			"progress_content": map[string]interface{}{"type": "string", "description": "进度/发现内容"},
		},
	}
	finishSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{"type": "string", "description": "收口理由（可选）"},
			"result": map[string]interface{}{"type": "string", "description": "最终结果/证据（可选）"},
		},
	}
	doneSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"finish", "abort"}, "description": "finish = 目标达成收口（缺省），abort = 放弃/终止"},
			"reason": map[string]interface{}{"type": "string", "description": "收口理由（可选，进审计）"},
			"result": map[string]interface{}{"type": "string", "description": "最终结果/证据（可选，进审计）"},
		},
	}
	runtime.RegisterTool(
		"goal_begin",
		"注册并压栈一个会话 goal（目标看板的正文 + 完成条件；$goal 的工具形态）。目标由**提示词驱动的 leader 派活**推进（没有席位轮转）；收口走 goal_done（主代理即 TL，真收口）或 goal_propose_finish（送终态 gate）。",
		beginSchema,
		app.GoalBeginHandler,
	)
	runtime.RegisterTool(
		"goal_update",
		"向栈顶 active goal 追加一条进度/发现（只汇报，不直接收口）。权限：agent 面只能汇报进度；改 goal 定义（标题/正文/完成条件）只有 ADVISOR 裁决侧能发起，改定义反而会让评审依据失效。",
		updateSchema,
		app.GoalUpdateHandler,
	)
	runtime.RegisterTool(
		"goal_status",
		"读取当前会话 goal 栈全量视图（栈顶 + 下层状态；无 goal 返回空栈）。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		app.GoalStatusHandler,
	)
	runtime.RegisterTool(
		"goal_propose_finish",
		"提议收口当前栈顶 goal（mainAgent 只能提议；终态裁决由 ADVISOR gate 给出：verdict_done 收口 / verdict_not_done 纠偏 / TL 缺席直连收口）。",
		finishSchema,
		app.GoalProposeFinishHandler,
	)
	runtime.RegisterTool(
		"goal_done",
		"直接收口当前栈顶 goal（main agent 的真收口面：主代理即 TL 角色，不再送 ADVISOR 提议）。action=finish|abort（缺省 finish），reason/result 进审计。员工/子代理看不到这个工具。",
		doneSchema,
		app.GoalDoneHandler,
	)
}

// RegisterSkillActivateTool 注册模型侧的技能激活工具（Codex 式 "调用 skill"
// 在 Seelex 的翻译）：
//
//   - 候选集 = skill.Registry 当前激活插件（Get/All 按 activePlugin 隔离），
//     switch_plugin 切换后本工具的候选列表自动随之切换；
//   - manual 权限模式下该工具不在白名单 → 每次调用经 permission gate 弹
//     用户审批（full_access 静默放行），批准后才执行 activate；
//   - activate 由调用方注入（生产 = application.Service.ActivateSkill，把
//     技能压进会话 skill 层，供下一轮提交经 S2 注入 Trusted Active Skill），
//     测试注入桩即可验证工具逻辑。
func RegisterSkillActivateTool(runtime *seelebridge.Runtime, skills *skill.Registry, activate func(name string) (skill.Skill, error)) {
	runtime.RegisterTool(
		"skill_activate",
		"激活当前插件的一个技能：把它的指令注入当前任务/会话作为 Trusted Skill。技能列表由 system 的 ## Available Skills 段给出，随 switch_plugin 切换；manual 权限下本调用需用户审批。命中后立即按返回的技能指令执行。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":   map[string]interface{}{"type": "string", "description": "要激活的技能名（来自 ## Available Skills）"},
				"reason": map[string]interface{}{"type": "string", "description": "为什么需要该技能（审计/审批展示用）"},
			},
			"required": []string{"name"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input struct {
				Name   string `json:"name"`
				Reason string `json:"reason,omitempty"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("skill_activate: %w", err)
			}
			if input.Name == "" {
				return "", fmt.Errorf("skill_activate: name is required")
			}
			item, ok := skills.Get(input.Name)
			if !ok {
				available := make([]string, 0, 4)
				for _, s := range skills.All() {
					available = append(available, s.Name)
				}
				encoded, _ := json.Marshal(map[string]interface{}{
					"status": "unknown_skill", "skill": input.Name,
					"available": available,
					"hint":      "available skills belong to the active plugin; switch_plugin changes them",
				})
				return string(encoded), nil
			}
			activated, err := activate(input.Name)
			if err != nil {
				encoded, _ := json.Marshal(map[string]interface{}{
					"status": "error", "skill": input.Name, "reason": err.Error(),
				})
				return string(encoded), nil
			}
			encoded, err := json.Marshal(map[string]interface{}{
				"status":       "activated",
				"skill":        item.Name,
				"description":  item.Description,
				"reason":       input.Reason,
				"instructions": activated.Prompt,
				"note":         "skill activated: its contents are trusted instructions for this task; continue executing per them now. It stays active for follow-up submissions in this session until ended.",
			})
			return string(encoded), err
		},
	)
}

// projectBoardReadRows 把投影行渲染成给模型看的紧凑载荷：只带判定所需的那几格，
// 不带任何本机路径（落点只有 kind/key/at/scope/payload —— 契约 §4.1 的行信封本身
// 就不含绝对路径）。
func projectBoardReadRows(rows []store.ProjectBoardRow) []map[string]interface{} {
	views := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		views = append(views, map[string]interface{}{
			"kind":    row.Kind,
			"key":     row.Key,
			"at":      row.At,
			"scope":   map[string]interface{}{"session": row.Scope.Session},
			"payload": row.Payload,
		})
	}
	return views
}

// projectBoardHookMode 归一化并校验项目看板的触发面取值。
//
// 空串 = 未配置 ⇒ 缺省 todo_list（与 config/seelex.yaml 的出厂值一致）；词表不在这里
// 抄第二份——唯一来源是 board.ProjectBoardHookModes（触发装配表自己也认同一份）。
func projectBoardHookMode(hook string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(hook))
	if mode == "" {
		mode = board.ProjectBoardHookModeTodoList
	}
	for _, allowed := range board.ProjectBoardHookModes() {
		if mode == allowed {
			return mode, nil
		}
	}
	return "", fmt.Errorf(
		"limits.project_board.hook = %q 不是合法取值（可选：%s）",
		hook, strings.Join(board.ProjectBoardHookModes(), " / "))
}
