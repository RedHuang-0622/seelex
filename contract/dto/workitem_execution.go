package dto

// WorkItemProgressView is the read projection of one WorkItem execution unit.
// It carries the existing workunit progress facts without creating a second
// progress store.
type WorkItemProgressView struct {
	Kind      string      `json:"kind,omitempty"`
	NodeID    string      `json:"node_id,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Goal      string      `json:"goal,omitempty"`
	Status    string      `json:"status,omitempty"`
	InFlight  bool        `json:"in_flight,omitempty"`
	Summary   string      `json:"summary,omitempty"`
	Error     string      `json:"error,omitempty"`
	Stages    []StageView `json:"stages,omitempty"`
	Worktree  string      `json:"worktree,omitempty"`
	UpdatedAt string      `json:"updated_at,omitempty"`
}

// StageView is one bounded execution stage marker.
type StageView struct {
	Stage   string `json:"stage"`
	Preview string `json:"preview,omitempty"`
}

// WorkItemExecutionView is the single read payload shared by WorkItem details
// and the teammate WorkItem progress page.
type WorkItemExecutionView struct {
	WorkItem TeamworkWorkItemView `json:"work_item"`
	Progress WorkItemProgressView `json:"progress"`
	Session  SessionRecordPage    `json:"session"`
}

// TeammateInspectionView is the safe, read-only teammate detail payload.
type TeammateInspectionView struct {
	TeamID                string                 `json:"team_id,omitempty"`
	Role                  string                 `json:"role,omitempty"`
	RoleSessionID         string                 `json:"role_session_id,omitempty"`
	CurrentWorkItem       *TeamworkWorkItemView  `json:"current_work_item,omitempty"`
	BaseSystemPrompt      string                 `json:"base_system_prompt,omitempty"`
	EffectiveSystemPrompt string                 `json:"effective_system_prompt,omitempty"`
	Assembly              *PluginAssemblyView    `json:"assembly,omitempty"`
	Plugins               []PluginCapabilityView `json:"plugins,omitempty"`
	Limits                TeammateLimitsView     `json:"limits"`
	Permissions           PermissionVectorView   `json:"permissions"`
	CurrentExecution      *WorkItemExecutionView `json:"current_execution,omitempty"`
}

// TeammateLimitsView contains configured/effective limits visible to users.
type TeammateLimitsView struct {
	MaxJobs             int `json:"max_jobs"`
	MaxTurnsConfigured  int `json:"max_turns_configured"`
	MaxTurnsEffective   int `json:"max_turns_effective"`
	MaxPluginsPerMember int `json:"max_plugins_per_member"`
}

// PluginCapabilityView is a redacted plugin capability projection. It must not
// contain prompt bodies, absolute paths, MCP command/env/args, or URLs.
type PluginCapabilityView struct {
	Name         string                `json:"name"`
	Description  string                `json:"description,omitempty"`
	Mode         string                `json:"mode,omitempty"`
	Include      []string              `json:"include,omitempty"`
	Exclude      []string              `json:"exclude,omitempty"`
	Tools        []string              `json:"tools,omitempty"`
	Skills       []PluginSkillView     `json:"skills,omitempty"`
	MCPServers   []PluginMCPServerView `json:"mcp_servers,omitempty"`
	Source       string                `json:"source,omitempty"`
	Fault        string                `json:"fault,omitempty"`
	Yellow       bool                  `json:"yellow,omitempty"`
	YellowReason string                `json:"yellow_reason,omitempty"`
}

// PluginSkillView exposes only skill identity and short description.
type PluginSkillView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PluginMCPServerView exposes safe identity only.
type PluginMCPServerView struct {
	Name      string `json:"name"`
	Transport string `json:"transport,omitempty"`
}

// PermissionVectorView separates raw permission bits from decoded abilities.
type PermissionVectorView struct {
	ToolsPolicy    string                `json:"tools_policy,omitempty"`
	Groups         map[string]uint8      `json:"groups,omitempty"`
	Grants         []PermissionGrantView `json:"grants,omitempty"`
	EffectiveTools []string              `json:"effective_tools,omitempty"`
}

// PermissionGrantView decodes r/w/x abilities for one routed permission group.
type PermissionGrantView struct {
	Group     string   `json:"group"`
	Bits      uint8    `json:"bits"`
	Abilities []string `json:"abilities"`
	Explicit  bool     `json:"explicit"`
}
