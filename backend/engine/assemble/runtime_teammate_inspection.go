package assemble

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

var _ contract.TeammateInspectionPort = (*Runtime)(nil)

// TeammateInspection aggregates safe read-only facts for the teammate detail
// page. It never changes plan, role membership, WorkItem status, or Sessions.
func (r *Runtime) TeammateInspection(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error) {
	mainSessionID = strings.TrimSpace(mainSessionID)
	role = strings.TrimSpace(role)
	if mainSessionID == "" || role == "" {
		return dto.TeammateInspectionView{}, errors.New("teammate inspection: main session and role are required")
	}
	board := r.TeamworkBoardSnapshot(mainSessionID)
	if board == nil {
		return dto.TeammateInspectionView{}, fmt.Errorf("teammate inspection: team %q has no plan", mainSessionID)
	}
	var member *dto.TeamworkMemberView
	for index := range board.Members {
		if strings.TrimSpace(board.Members[index].Role) == role {
			member = &board.Members[index]
			break
		}
	}
	if member == nil {
		return dto.TeammateInspectionView{}, fmt.Errorf("teammate inspection: role %q is not in the team plan", role)
	}
	plan, err := r.coordinatorForSession(mainSessionID)
	if err != nil {
		return dto.TeammateInspectionView{}, err
	}
	workPlan, err := plan.Plan(ctx)
	if err != nil {
		return dto.TeammateInspectionView{}, err
	}
	teamMember, ok := teamwork.MemberOf(workPlan, role)
	if !ok {
		return dto.TeammateInspectionView{}, fmt.Errorf("teammate inspection: role %q is not in the persisted plan", role)
	}
	groups, err := teamwork.MemberPermissionGroups(teamMember)
	if err != nil {
		return dto.TeammateInspectionView{}, err
	}
	basePrompt := r.roleTurnSystemPrompt(role)
	effectivePrompt := basePrompt
	var currentItem *dto.TeamworkWorkItemView
	if itemID := strings.TrimSpace(member.CurrentWorkItem); itemID != "" {
		for index := range board.WorkItems {
			if strings.TrimSpace(board.WorkItems[index].ID) == itemID {
				item := board.WorkItems[index]
				currentItem = &item
				break
			}
		}
	}
	reader := r.teamUnitReader(mainSessionID, r.teamSceneIndex(mainSessionID))
	if currentItem != nil {
		if handle := r.roleSessionHandleFor(currentItem.SessionID); handle != nil {
			if prompt := strings.TrimSpace(handle.effectiveSystemPrompt()); prompt != "" {
				effectivePrompt = prompt
			}
		}
		record, found, readErr := reader.Record(teamwork.SceneNodeID(role, currentItem.ID))
		if readErr != nil {
			return dto.TeammateInspectionView{}, readErr
		}
		if found && strings.TrimSpace(record.Unit.SystemPrompt) != "" {
			effectivePrompt = record.Unit.SystemPrompt
		}
	}
	assembly := (*dto.PluginAssemblyView)(nil)
	assemblies := r.assemblyViews([]storeteamwork.Member{teamMember})
	if len(assemblies) > 0 {
		view := assemblies[0]
		assembly = &view
	}
	plugins := r.inspectPluginCapabilities(teamMember, role)
	permissions := r.inspectPermissionVector(role, teamMember, groups, plugins)
	limitsView := dto.TeammateLimitsView{
		MaxJobs:             r.limits.Team.MaxJobs,
		MaxTurnsConfigured:  teamMember.MaxTurns,
		MaxTurnsEffective:   teamwork.TurnLimit(teamMember.MaxTurns, roleTurnMaxLoops),
		MaxPluginsPerMember: r.maxPluginsPerTeammate(),
	}
	result := dto.TeammateInspectionView{
		TeamID: board.TeamID, Role: role, RoleSessionID: member.RoleSessionID,
		CurrentWorkItem: currentItem, BaseSystemPrompt: basePrompt,
		EffectiveSystemPrompt: effectivePrompt, Assembly: assembly,
		Plugins: plugins, Limits: limitsView, Permissions: permissions,
	}
	if currentItem != nil {
		execution, execErr := r.WorkItemExecution(ctx, mainSessionID, currentItem.ID, dto.ConversationViewRequest{})
		if execErr != nil {
			return dto.TeammateInspectionView{}, execErr
		}
		result.CurrentExecution = &execution
	}
	return result, nil
}

func (r *Runtime) roleSessionHandleFor(sessionID string) *roleSessionHandle {
	sessionID = strings.TrimSpace(sessionID)
	if r == nil || sessionID == "" {
		return nil
	}
	state := r.roleTurnState()
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.sessions[sessionID]
}

func (r *Runtime) inspectPluginCapabilities(member storeteamwork.Member, role string) []dto.PluginCapabilityView {
	names := normalizePluginNames(member.Plugins)
	if len(names) == 0 {
		return nil
	}
	provided := r.pluginCapabilityViews(append([]string(nil), names...))
	byName := make(map[string]dto.PluginCapabilityView, len(provided))
	for _, capability := range provided {
		byName[capability.Name] = capability
	}
	allTools := r.AllTools()
	views := make([]dto.PluginCapabilityView, 0, len(names))
	for _, name := range names {
		view := byName[name]
		view.Name = name
		judgement := r.pluginFaceJudgement([]string{name})
		if judgement.Faulted {
			view.Fault = judgement.FaultNote([]string{name})
		}
		if view.Description == "" {
			for _, def := range judgement.Defs {
				if def.Name == name {
					view.Description = def.Description
					view.Include = append([]string(nil), def.Include...)
					view.Exclude = append([]string(nil), def.Exclude...)
					break
				}
			}
		}
		if len(view.Tools) == 0 {
			for _, tool := range allTools {
				if judgement.Visible(tool.Name) {
					view.Tools = append(view.Tools, tool.Name)
				}
			}
		}
		sort.Strings(view.Tools)
		views = append(views, view)
	}
	return views
}

func (r *Runtime) inspectPermissionVector(role string, member storeteamwork.Member, groups map[string]uint8, plugins []dto.PluginCapabilityView) dto.PermissionVectorView {
	view := dto.PermissionVectorView{
		ToolsPolicy: member.ToolsPolicy,
		Groups:      make(map[string]uint8, len(groups)),
	}
	for group, bits := range groups {
		view.Groups[group] = bits
		view.Grants = append(view.Grants, dto.PermissionGrantView{
			Group: group, Bits: bits, Abilities: decodePermissionAbilities(bits),
			Explicit: len(member.Permission) > 0,
		})
	}
	sort.Slice(view.Grants, func(i, j int) bool { return view.Grants[i].Group < view.Grants[j].Group })
	// The final tool face is the plugin face intersected with the role's
	// permission face. The registry remains the permission decision source.
	if len(plugins) > 0 {
		seen := map[string]struct{}{}
		for _, plugin := range plugins {
			for _, tool := range plugin.Tools {
				seen[tool] = struct{}{}
			}
		}
		grantCtx := toolreg.WithEmployeeGrant(context.Background(), role, member.ToolsPolicy, groups)
		for tool := range seen {
			if r == nil || r.permission == nil || r.permission.ToolFaceForContext(grantCtx, tool) {
				view.EffectiveTools = append(view.EffectiveTools, tool)
			}
		}
		sort.Strings(view.EffectiveTools)
	}
	return view
}

func decodePermissionAbilities(bits uint8) []string {
	abilities := make([]string, 0, 3)
	if bits&dto.PermissionBitRead != 0 {
		abilities = append(abilities, "r")
	}
	if bits&dto.PermissionBitWrite != 0 {
		abilities = append(abilities, "w")
	}
	if bits&dto.PermissionBitExecute != 0 {
		abilities = append(abilities, "x")
	}
	return abilities
}
