package bootstrap

import (
	"context"
	"fmt"
	"log"
	"strings"

	frameworkSession "github.com/RedHuang-0622/Seele/session"

	seelebridge "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/backend/engine/plugin"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	"github.com/RedHuang-0622/seelex/wiring/config"
	"github.com/RedHuang-0622/seelex/wiring/registry"
)

func InitSkillSystem() *skill.Registry {
	// skills are now per-plugin (plugins/<name>/<skill>/SKILL.md).
	// The registry is populated via PublishPluginSkills on plugin Load/Activate.
	return skill.NewRegistry()
}

// PluginStartup 是一次插件系统启动的读数（终端日志与 UI 面**同源**，见 wiring/config 的
// PluginRootReport）：
//
//	RootReading —— 根读数（加载了几个插件、从哪个根；进终端日志 + UI 启动通知）
//	Curated     —— 精选目录读不到时的出声（空 = 目录已读到，无声；非空 = 启动期警告）
type PluginStartup struct {
	RootReading string
	Curated     string
}

func InitPluginSystem(
	runtime *seelebridge.Runtime,
	skills *skill.Registry,
	// pluginsPaths 是 `-plugins` 的原始值（责任链的链首）；环境变量与可执行文件
	// 位置由 config.PluginRoots 自己解析。
	pluginsPaths string,
) (*plugin.Manager, PluginStartup, error) {
	// 根解析 = 责任链（-plugins > $SEELEX_PLUGINS > <exe>/plugins > <exe>/../plugins
	// > plugins(CWD)），见 config.PluginRootChain；加载器是多根 first-wins。
	roots := config.PluginRoots(pluginsPaths)
	loader := plugin.NewLoader(roots...)
	manager := plugin.NewManager(loader, runtime, runtime, skills)
	if err := manager.Load(); err != nil {
		return nil, PluginStartup{}, fmt.Errorf("加载 Plugin 失败: %w", err)
	}
	loaded := manager.All()
	if err := config.RequirePlugins(roots, loaded); err != nil {
		return nil, PluginStartup{}, err
	}
	startup := PluginStartup{RootReading: config.LogPluginRoots(roots, loaded)}
	// 精选目录（A：从"声明面"变成"装配面"）：**启动时**从已解析的插件根读一次，
	// 读到的判决函数接进运行期的装配校验（team_plan members[].plugins）——
	// pending 候选（路线图）要能点名上游来源、认不出的名字要说清两边都不在。
	//
	// 三档出声（2026-10-05 重排：使用者自己改插件不该被自己的 app 警告）：
	//   - 目录**缺席**（自建根/用户树没这份文件）⇒ 只写终端一行：合法现场，不是警告；
	//   - 目录**存在却读不动**（解析/结构失败）⇒ 终端 + UI 启动警告（这是配置缺陷）；
	//   - 目录能读、但与这台机器**已装集合有差异** ⇒ 只写终端一行（本机自装/退役插件）。
	// 无论哪一档，判决函数都会被注入：它对每个未定义名显式拒绝并说清"用哪个根找过"，
	// 所以"没有目录/目录坏了"在装配面上同样是显式失败，而不是"没有 pending"。
	// 发现 → 读回 → 落进 yaml：磁盘是事实，精选目录是事实的读数。运行树里的插件可能是
	// agent 自己长出来的（plugin_create）、也可能是手工/上一次会话放进来的——先登记，再读
	// 判决面；否则它们只会被读成"漂移"，使用者只能自己把机器该做的簿记抄进 YAML。
	if registered, err := manager.RegisterDiscoveredPlugins(); err != nil {
		log.Printf("plugin: 精选目录登记失败（目录保持原样，不影响启动）: %v", err)
	} else if len(registered) > 0 {
		log.Printf("plugin: 已发现并登记本机自建插件进 %s: %s",
			plugin.CuratedFileName, strings.Join(registered, ", "))
	}
	curated := config.ResolveCuratedRead(roots, loaded)
	// 来源读数（「这个插件从哪来」）的判定源挂到 manager 上：运行期视图经 adapters 的
	// PluginPort 读它，把 source_kind/source_url 补进每个插件——判定与文案全在
	// plugin.CuratedCatalog.ReadSource，前端不复制判定、组合根也不复制。
	// 目录缺席/损坏时传的是零值 ⇒ 来源面留空（缺失来源不编默认值）。
	manager.SetCuratedCatalog(curated.Catalog)
	runtime.SetPluginUnassembledReason(config.CuratedAssemblyJudge(curated))
	switch {
	case curated.Absent:
		log.Printf("plugin: 没有精选目录——%v（找过: %s）；自建根/用户树可以不带它，装配面对未定义名仍显式拒绝",
			curated.Err, curated.Page())
	case curated.Err != nil:
		message := fmt.Sprintf(
			"%s 读不到（%v；责任链上找过的根: %s）——装配面的名字判定会显式拒绝未定义的名字，不会静默当空目录",
			plugin.CuratedFileName, curated.Err, curated.Page())
		log.Printf("plugin: %s", message)
		startup.Curated = message
	case len(curated.Drift) > 0:
		log.Printf("plugin: %s 与本次已装集合有差异（不是错误，是回报）：%s",
			curated.Path, strings.Join(curated.Drift, "；"))
	}
	return manager, startup, nil
}

func ActivateDefaultPlugin(manager *plugin.Manager, eng *frameworkSession.Session) error {
	if _, err := registry.PluginByName(manager.All(), "default"); err != nil {
		// 同一条静默降级链的另一半：插件加载到了，但没有 default（启动基线）。
		// 这里不阻断启动（自定义根可以只带一个垂直插件），但不许无声无息。
		log.Printf("plugin: 没有 default 插件——启动基线未激活（skill 目录与 `#` 切换为空）；可用插件见 plugins/curated.yaml 的 entries")
		return nil
	}
	if err := manager.Activate(context.Background(), "default"); err != nil {
		return fmt.Errorf("激活 default Plugin 失败: %w", err)
	}
	// 系统提示词由 application.Service.buildSystemPrompt 在 InitApplication 时组装，
	// 不要在启动时直接覆盖 session 的 system prompt。
	// registry.ApplyPluginPrompt(eng, manager)
	return nil
}
