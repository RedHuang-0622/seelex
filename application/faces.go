package application

import "github.com/RedHuang-0622/seelex/application/core"

// faces.go 只再导出 Application 入站契约，保持前端看到的类型身份不变。
// 接口定义位于 application/core/service_api_faces.go；根 application 不新增类型身份。
var FaceTypeNames = core.FaceTypeNames

type (
	ViewFace                 = core.ViewFace
	CommandFace              = core.CommandFace
	PolicyFace               = core.PolicyFace
	QueueFace                = core.QueueFace
	ConversationAPI          = core.ConversationAPI
	SessionAPI               = core.SessionAPI
	ExecutionQueryAPI        = core.ExecutionQueryAPI
	WorkItemQueryAPI         = core.WorkItemQueryAPI
	TeammateQueryAPI         = core.TeammateQueryAPI
	TeamworkQueryAPI         = core.TeamworkQueryAPI
	SubagentQueryAPI         = core.SubagentQueryAPI
	TranscriptQueryAPI       = core.TranscriptQueryAPI
	ExecutionCommandAPI      = core.ExecutionCommandAPI
	WorkspaceAPI             = core.WorkspaceAPI
	GoalAPI                  = core.GoalAPI
	PolicyAPI                = core.PolicyAPI
	AdminAPI                 = core.AdminAPI
	RoleAPI                  = core.RoleAPI
	SubagentAPI              = core.SubagentAPI
	StateAPI                 = core.StateAPI
	QueueAPI                 = core.QueueAPI
	LifecycleAPI             = core.LifecycleAPI
	SessionQuery             = core.SessionQuery
	ConversationHistoryQuery = core.ConversationHistoryQuery
)
