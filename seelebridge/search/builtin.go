package search

import (
	"fmt"
	"sort"
	"strings"
)

// BuiltinFactory 构建一个内置厂商适配器（builtin vendor adapter）：厂商专有
// 协议的请求构造与响应解析封装在各自 Strategy 实现内部，对外仍遵循统一契约
// ——入参 (ctx, query, maxResults)、出参 SearchResponse，与标准协议策略一致。
// 消费方（工具层 / application 门面）不感知、也不需要感知厂商差异。
type BuiltinFactory func(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error)

// builtinVendors 是内置厂商注册表：strategies[].type（或旧 provider 名）→ 工厂。
// 注册发生在各适配器文件的 init()；装配期只查表，新增厂商不改装配逻辑。
var builtinVendors = map[string]BuiltinFactory{}

// registerBuiltin 注册一个内置厂商适配器（供各适配器文件 init 调用；
// 名称为空或工厂为空时忽略，保持注册表不变量）。
func registerBuiltin(vendor string, factory BuiltinFactory) {
	v := strings.ToLower(strings.TrimSpace(vendor))
	if v == "" || factory == nil {
		return
	}
	builtinVendors[v] = factory
}

// isBuiltinVendor 报告 name 是否为已注册的内置厂商名（旧 provider 兼容用）。
func isBuiltinVendor(name string) bool {
	_, ok := builtinVendors[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// builtinNames 返回已注册厂商名（字典序，用于装配错误提示）。
func builtinNames() []string {
	out := make([]string, 0, len(builtinVendors))
	for name := range builtinVendors {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// assembleStrategy 按 strategies[].type 分发策略构建：
//   - 空或 "standard"：标准 websearch 协议（Tavily 兼容，POST JSON + Bearer，
//     端点可配置，可指向自建网关或任意兼容端点）；
//   - 内置厂商名（tavily / bochaai / searxng…）：查注册表构建专有协议适配器；
//   - 未注册：装配期报错并列出可用厂商。
//
// 分发后所有策略共享同一入参/出参契约，切换厂商只改配置（type 字段）。
func assembleStrategy(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error) {
	t := strings.ToLower(strings.TrimSpace(cfg.Type))
	if t == "" || t == "standard" {
		return newStandardStrategy(cfg, opts)
	}
	factory, ok := builtinVendors[t]
	if !ok {
		return nil, fmt.Errorf("search: 不支持的 strategies[].type %q（可用内置厂商: %s；省略 type 则使用标准 Tavily 兼容协议）",
			cfg.Type, strings.Join(builtinNames(), ", "))
	}
	return factory(cfg, opts)
}
