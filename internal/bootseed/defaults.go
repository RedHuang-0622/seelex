package bootseed

import (
	"embed"
	"io/fs"
	"path/filepath"
)

// assets/ 是"缺失时就地初始化"的那份默认数据。三类来源：
//
//   - assets/config/seelex.yaml、assets/config/seele.yaml：与仓库 `config/` 下的
//     规范档**逐字节相同**（规范档是唯一事实源）。同步靠
//     `scripts/sync-bootseed-defaults.ps1`，漂移由 defaults_test.go 钉住——
//     "包内那份冻结在旧档"正是 2026-09-29 折叠厚摘要开关那个现场。
//   - assets/local/tools/<tool>/：本地工具目录的骨架（第三方脚本本体不随包分发，
//     见该目录的 README）。
//
// `all:` 前缀必须留着：go:embed 默认跳过 `_`/`.` 开头的文件，而骨架里正是靠
// `.env.example` 这类文件说话。
//
//go:embed all:assets
var assetsFS embed.FS

// Assets 返回内嵌默认数据的只读资源集。
func Assets() fs.FS { return assetsFS }

// RuntimeConfigName / PermissionConfigName 是运行参数与权限规则的文件名：调用方
// 拼候选链（`config/<name>`、`<name>`、`<exe>/config/<name>`）时共用同一份常量。
const (
	RuntimeConfigName    = "seelex.yaml"
	PermissionConfigName = "seele.yaml"
)

// RuntimeConfigPack 是运行参数配置（window / limits）的默认数据包。
func RuntimeConfigPack() Pack {
	return Pack{Name: RuntimeConfigName, Files: []File{
		{Rel: RuntimeConfigName, Source: "assets/config/" + RuntimeConfigName},
	}}
}

// PermissionConfigPack 是权限规则配置（permission.rules）的默认数据包。
func PermissionConfigPack() Pack {
	return Pack{Name: PermissionConfigName, Files: []File{
		{Rel: PermissionConfigName, Source: "assets/config/" + PermissionConfigName},
	}}
}

// ToolPack 是本地工具目录的默认数据包：只放**本应用自己写**的骨架（README、
// `.env.example` 一类），第三方脚本本体不进包（再分发是产品/许可决定）。
// files 里的 Rel/Source 都相对该工具目录。
func ToolPack(tool string, files ...File) Pack {
	pack := Pack{Name: tool}
	for _, file := range files {
		file.Rel = filepath.ToSlash(file.Rel)
		file.Source = "assets/local/tools/" + tool + "/" + filepath.ToSlash(file.Source)
		pack.Files = append(pack.Files, file)
	}
	return pack
}

// AutoGetJobsPack 是白名单命令 auto_get_jobs 的脚本目录骨架：一份给人看的
// README + 一份环境变量模板（键名取自脚本读的变量，值是空的）。
func AutoGetJobsPack() Pack {
	return ToolPack("auto_get_jobs",
		File{Rel: "README.md", Source: "README.md"},
		File{Rel: ".env.example", Source: ".env.example"},
	)
}
