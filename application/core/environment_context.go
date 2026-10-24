package core

// environment_context.go 负责把项目环境表投影成**请求尾部**的动态只读块。
//
// 为什么不进 system 前缀：环境是用户随时会改的项目配置，名字、地址和执行面都可能
// 高频变化。把它放进稳定前缀会让每次环境编辑都污染缓存；放在请求尾部的标记块里，
// 只影响当前这一轮，且不会写入历史或参与压缩。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

const (
	environmentTraceMarkerOpen  = "<!-- seelex:environments:v1 -->"
	environmentTraceMarkerClose = "<!-- /seelex:environments:v1 -->"
	environmentTraceMaxEntries  = 20
)

// environmentTraceBlockFor 返回指定会话当前项目的工作环境块。
//
// 没有绑定项目、环境表未装配或表为空时返回空串；读取失败也不打断本轮请求。
func (service *Service) environmentTraceBlockFor(sessionID string) string {
	if service == nil {
		return ""
	}
	environments, defaultEnv, err := service.projectEnvironments(sessionID)
	if err != nil || len(environments) == 0 {
		return ""
	}
	sessionState, _ := service.SessionSSHState(sessionID)
	projectRoot := ""
	if service.Deps.Workspace != nil {
		if workspace, ok := service.Deps.Workspace.SessionWorkspace(sessionID); ok {
			projectRoot = workspace.RootPath
		}
	}
	return renderEnvironmentTraceBlock(projectRoot, defaultEnv, sessionState.CurrentEnv, environments)
}

// renderEnvironmentTraceBlock 是纯渲染面，供单元测试直接钉住顺序、地址与标记。
func renderEnvironmentTraceBlock(projectRoot, defaultEnv, currentEnv string, environments []model.WorkspaceEnvironmentInfo) string {
	if len(environments) == 0 {
		return ""
	}
	items := append([]model.WorkspaceEnvironmentInfo(nil), environments...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	var builder strings.Builder
	builder.WriteString(environmentTraceMarkerOpen + "\n")
	builder.WriteString("# 工作环境（会话固定，只读）\n")
	current := strings.TrimSpace(currentEnv)
	if current == "" {
		builder.WriteString("本会话未绑定命名环境：环境感知工具在本地项目根执行。环境只能由用户在会话顶部固定，agent 不得自行切换。\n")
	} else {
		builder.WriteString("本会话固定环境：" + singleLine(current) + "。read_file / write_file / edit_file / bash / bash_bg / bg_manage 自动使用该环境，subagent / teammate 继承同一绑定；agent 不得通过工具参数切换环境或传输。\n")
	}
	builder.WriteString("无地址的环境在本地项目根执行；有地址的环境走 SSH，并仍受当前 agent role 的 ssh 权限约束。\n")
	for index, item := range items {
		if index >= environmentTraceMaxEntries {
			builder.WriteString(fmt.Sprintf("- …另有 %d 个环境未展开（项目设置里查看完整清单）\n", len(items)-environmentTraceMaxEntries))
			break
		}
		line := fmt.Sprintf("- %s · %s", singleLine(item.Name), environmentAddress(item, projectRoot))
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
	builder.WriteString(environmentTraceMarkerClose)
	return builder.String()
}

func environmentAddress(item model.WorkspaceEnvironmentInfo, projectRoot string) string {
	host, user := strings.TrimSpace(item.Host), strings.TrimSpace(item.User)
	if host == "" && user == "" {
		if root := strings.TrimSpace(projectRoot); root != "" {
			return "local · " + singleLine(root)
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
		address += " · remote_root=" + singleLine(root)
	}
	return address
}

func singleLine(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ")
	trimmed := strings.TrimSpace(replacer.Replace(value))
	if len([]rune(trimmed)) <= 120 {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:117]) + "..."
}
