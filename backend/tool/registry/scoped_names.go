package registry

// scoped_names.go 是**项目作用域工具**的名单（唯一一份）。
//
// 为什么值得单列：这批名字有两处消费面——`Router.Register` 里**注册**它们，装配层的
// `RegisterTool` 又要**拒绝**在项目作用域表就绪之后再注册同名工具（"后注册的会覆盖路由"）。
// 两处各写一份名单，任何一次增删都会让两边漂移：新加的项目作用域工具会被晚注册顶掉，
// 或被拒得莫名其妙。因此名字与判定同处一份（P21-31 归位；装配层只留同身份别名）。

const (
	// ReadFileToolName 读项目内文件。
	ReadFileToolName = "read_file"
	// GrepSearchToolName 在项目内搜内容。
	GrepSearchToolName = "grep_search"
	// GlobToolName 在项目内找文件。
	GlobToolName = "glob"
	// WriteFileToolName 写项目内文件。
	WriteFileToolName = "write_file"
	// EditFileToolName 改项目内文件。
	EditFileToolName = "edit_file"
	// BashToolName 在项目绑定根内跑命令（同步、写类）。
	BashToolName = "bash"
)

// projectScopedToolNames 是**项目作用域**工具集合（装配层据此判断"该名字是否已被项目作用域占住"）。
var projectScopedToolNames = map[string]struct{}{
	ReadFileToolName:   {},
	GrepSearchToolName: {},
	GlobToolName:       {},
	WriteFileToolName:  {},
	EditFileToolName:   {},
	BashToolName:       {},
}

// ProjectScopedToolName 报告该工具名是不是项目作用域工具（名单见上；大小写敏感——工具名是契约面）。
func ProjectScopedToolName(name string) bool {
	_, ok := projectScopedToolNames[name]
	return ok
}
