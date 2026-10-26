package prompt

import (
	"fmt"
	"sort"
	"strings"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

const (
	EnvironmentTraceMarkerOpen  = "<!-- seelex:environments:v1 -->"
	EnvironmentTraceMarkerClose = "<!-- /seelex:environments:v1 -->"
	environmentTraceMaxEntries  = 20
)

// EnvironmentTraceDeps supplies project environment facts for one session.
type EnvironmentTraceDeps struct {
	ProjectEnvironments func(sessionID string) (items []protocol.WorkspaceEnvironmentInfo, defaultEnv string, err error)
	CurrentEnvironment  func(sessionID string) (string, error)
	ProjectRoot         func(sessionID string) string
}

// EnvironmentTraceBlockFor renders one session's provider-only environment
// block. Missing project/environment facts degrade to an empty block.
func EnvironmentTraceBlockFor(deps EnvironmentTraceDeps, sessionID string) string {
	if deps.ProjectEnvironments == nil {
		return ""
	}
	environments, defaultEnv, err := deps.ProjectEnvironments(sessionID)
	if err != nil || len(environments) == 0 {
		return ""
	}
	currentEnv := ""
	if deps.CurrentEnvironment != nil {
		currentEnv, _ = deps.CurrentEnvironment(sessionID)
	}
	projectRoot := ""
	if deps.ProjectRoot != nil {
		projectRoot = deps.ProjectRoot(sessionID)
	}
	return RenderEnvironmentTraceBlock(projectRoot, defaultEnv, currentEnv, environments)
}

// RenderEnvironmentTraceBlock is the pure rendering surface.
func RenderEnvironmentTraceBlock(projectRoot, defaultEnv, currentEnv string, environments []protocol.WorkspaceEnvironmentInfo) string {
	if len(environments) == 0 {
		return ""
	}
	items := append([]protocol.WorkspaceEnvironmentInfo(nil), environments...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	var builder strings.Builder
	builder.WriteString(EnvironmentTraceMarkerOpen + "\n")
	builder.WriteString("# 工作环境（会话固定，只读）\n")
	current := strings.TrimSpace(currentEnv)
	if current == "" {
		builder.WriteString("本会话未绑定命名环境：环境感知工具在本地项目根执行。环境只能由用户在会话顶部固定，agent 不得自行切换。\n")
	} else {
		builder.WriteString("本会话固定环境：" + environmentSingleLine(current) + "。read_file / write_file / edit_file / bash / bash_bg / bg_manage 自动使用该环境，subagent / teammate 继承同一绑定；agent 不得通过工具参数切换环境或传输。\n")
	}
	builder.WriteString("无地址的环境在本地项目根执行；有地址的环境走 SSH，并仍受当前 agent role 的 ssh 权限约束。\n")
	for index, item := range items {
		if index >= environmentTraceMaxEntries {
			builder.WriteString(fmt.Sprintf("- …另有 %d 个环境未展开（项目设置里查看完整清单）\n", len(items)-environmentTraceMaxEntries))
			break
		}
		line := fmt.Sprintf("- %s · %s", environmentSingleLine(item.Name), environmentAddress(item, projectRoot))
		flags := make([]string, 0, 2)
		if item.Name == currentEnv {
			flags = append(flags, "当前固定")
		} else if current != "" {
			flags = append(flags, "本会话不可切换")
		}
		if item.Name == defaultEnv {
			flags = append(flags, "项目默认")
		}
		if len(flags) > 0 {
			line += " · " + strings.Join(flags, " / ")
		}
		builder.WriteString(line + "\n")
	}
	builder.WriteString(EnvironmentTraceMarkerClose)
	return builder.String()
}

func environmentAddress(item protocol.WorkspaceEnvironmentInfo, projectRoot string) string {
	host, user := strings.TrimSpace(item.Host), strings.TrimSpace(item.User)
	if host == "" && user == "" {
		if root := strings.TrimSpace(projectRoot); root != "" {
			return "local · " + environmentSingleLine(root)
		}
		return "local"
	}
	if host == "" || user == "" {
		return "invalid SSH endpoint"
	}
	port := item.Port
	if port <= 0 {
		port = 22
	}
	address := fmt.Sprintf("ssh · %s@%s:%d", user, host, port)
	if root := strings.TrimSpace(item.RemoteRoot); root != "" {
		address += " · remote_root=" + environmentSingleLine(root)
	}
	return address
}

func environmentSingleLine(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ")
	trimmed := strings.TrimSpace(replacer.Replace(value))
	if len([]rune(trimmed)) <= 120 {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:117]) + "..."
}
