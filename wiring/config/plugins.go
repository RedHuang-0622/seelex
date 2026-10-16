package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RedHuang-0622/seelex/plugin"
)

// PluginRootChain 返回 Plugin 根在责任链上的候选（按优先级、去重保序）：
//
//  1. -plugins <paths>      显式旗标（最高优先；逗号分隔可多根，先出现的根胜出）
//  2. $SEELEX_PLUGINS       环境变量（容器/CI/多份发行包并存时的注入点）
//  3. <exe>/plugins         交付树自带（发行包：二进制旁边就有一份 plugins/）
//  4. <exe>/../plugins      交付树上一级（stage 布局：exe 落在子目录里）
//  5. plugins               CWD 相对（仓库 / go run 开发场景）
//
// 语义是**顺序覆盖**而不是"择一"：加载器本身是多根 first-wins（plugin/loader.go
// 的 LoadAll：先出现的同名插件胜出、不存在的根静默跳过），所以这条链只决定"去哪里找"。
// 显式旗标排在最前，因此 `-plugins <开发目录>` 能覆盖发行包里那份同名插件；
// 而"到底哪个根真的供上了插件"由启动期的根报告（LogPluginRoots）写明，不靠猜。
func PluginRootChain(flagValue, envValue, exePath string) []string {
	roots := make([]string, 0, 6)
	roots = append(roots, SplitPaths(flagValue)...)
	roots = append(roots, SplitPaths(envValue)...)
	if exePath != "" {
		exeDir := filepath.Dir(exePath)
		roots = append(roots, filepath.Join(exeDir, "plugins"), filepath.Join(exeDir, "..", "plugins"))
	}
	roots = append(roots, "plugins")
	return DedupRoots(roots)
}

// PluginRoots 用真实环境（旗标 / SEELEX_PLUGINS / 可执行文件位置）解析责任链。
func PluginRoots(flagValue string) []string {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return PluginRootChain(flagValue, os.Getenv("SEELEX_PLUGINS"), exe)
}

// DedupRoots 去空、去重（保序）。Windows 上路径大小写不敏感，同一份目录可能以两种
// 写法出现在链上，重复项会让启动期的根报告出现"两个 0 个插件"的噪声行。
func DedupRoots(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		if root = strings.TrimSpace(root); root == "" {
			continue
		}
		cleaned := filepath.Clean(root)
		key := cleaned
		if os.PathSeparator == '\\' {
			key = strings.ToLower(cleaned)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, cleaned)
	}
	return result
}

// RequirePlugins 把"零插件"从静默降级变成一次显式失败。
//
// 没有 Plugin 的启动不是"功能少一点"：default 插件不存在 ⇒ 无 skill 目录、`#` 切换
// 为空、模型拿不到任何领域纪律，而旧口径下日志里一个字都没有（loader 跳过不存在的根
// → 空表不报错 → 找不到 default 就 `return nil`）。这条错误把"试过哪些根"和"怎么救"
// 一起说出来，让故障在启动期可见，而不是以"模型什么都不会"的形式显现。
func RequirePlugins(roots []string, loaded []plugin.Plugin) error {
	if len(loaded) > 0 {
		return nil
	}
	tried := "（责任链为空）"
	if len(roots) > 0 {
		tried = strings.Join(roots, ", ")
	}
	return fmt.Errorf(
		"零插件：责任链上 %d 个根都没有加载到任何 Plugin（试过: %s）。"+
			"用 -plugins <路径> 或 $SEELEX_PLUGINS 显式指定插件根；发行包应把 plugins/ 放在二进制旁边（<exe>/plugins）",
		len(roots), tried)
}

// PluginRootReport 生成"插件到底从哪来"的**一条读数**：责任链上每个根各供上了几个
// 插件，以及每个插件最终来自哪个根（多根 first-wins，同名插件只算链上先出现的那个）。
//
// 这条读数**两处共写**：终端日志（LogPluginRoots）与 UI 面（run() 里进启动通知）。
// 旧口径只有前者，于是同一条事实在 GUI 里根本看不到——所以"广播"那个词名不副实；
// 现在它是"启动读数"，两处同源（改一处两处一起变，不会出现两份对不上的读数）。
func PluginRootReport(roots []string, loaded []plugin.Plugin) string {
	absLoaded := make([]string, 0, len(loaded))
	for _, p := range loaded {
		if abs, err := filepath.Abs(p.RootDir); err == nil {
			absLoaded = append(absLoaded, abs)
		}
	}
	parts := make([]string, 0, len(roots))
	for _, root := range roots {
		count := 0
		if abs, err := filepath.Abs(root); err == nil {
			prefix := abs + string(os.PathSeparator)
			for _, dir := range absLoaded {
				if dir == abs || strings.HasPrefix(dir, prefix) {
					count++
				}
			}
		}
		parts = append(parts, fmt.Sprintf("%s=%d", root, count))
	}
	origins := make([]string, 0, len(loaded))
	for _, p := range loaded {
		origins = append(origins, fmt.Sprintf("%s←%s", p.Name, p.RootDir))
	}
	report := fmt.Sprintf("根 %s（共加载 %d 个插件）", strings.Join(parts, ", "), len(loaded))
	if len(origins) > 0 {
		report += "；来源 " + strings.Join(origins, ", ")
	}
	return report
}

// LogPluginRoots 把根读数写成启动期一行终端日志。**它不再是唯一出口**：同一条读数由
// run() 送进 UI 面（启动通知），见 PluginRootReport。返回这行读数，供调用方复用同一份
// 字节（两处各生成一次就会出现两份对不上的读数）。
//
// 这是"一处发现、处处可用"的可观察面——静默降级被这条读数替换掉（0 个插件的根会
// 明明白白显示为 0），排障不必再靠猜二进制旁边有没有 plugins/。
func LogPluginRoots(roots []string, loaded []plugin.Plugin) string {
	report := PluginRootReport(roots, loaded)
	log.Printf("plugin: %s", report)
	return report
}

// ResolveCuratedRead 从**已解析的插件根**（责任链，多根 first-wins）读一次精选目录。
//
// 语义三条：
//   - **first-wins**：链上第一个带 curated.yaml 的根说话（与加载器"同名插件先出现的
//     根胜出"同一姿势）；第一个存在的那份读不动就**当场报**，不悄悄退到下一个根——
//     退让会把"这份目录坏了"藏成"目录里没有这个名字"。
//   - **交叉校验用真实加载集合**：installed 取的是本次启动**真正加载出来**的插件名
//     （多根并集），不是某一个根的 LoadAll。
//   - **差异是回报、不是错误**：目录与这台机器已装集合不一致（本机自装插件、退役插件）
//     不阻断、不报警告，只留成 Drift 供日志回报——否则使用者"自己改下插件"就会永远
//     背着一条他改不动、也不该由他改的发行侧警告。
//   - **一个根都没有 ⇒ Absent**：自建根/用户树可以不带精选目录。Err 仍非 nil（装配面
//     要显式拒绝并说清"没有目录可比对"），但它不是配置缺陷、不进启动警告。
func ResolveCuratedRead(roots []string, loaded []plugin.Plugin) plugin.CuratedRead {
	installed := make([]string, 0, len(loaded))
	for _, p := range loaded {
		installed = append(installed, p.Name)
	}
	for _, root := range roots {
		path := filepath.Join(root, plugin.CuratedFileName)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		// 运行期读数：目录**自身**的问题才是错误；"与这台机器已装集合不一致"退成回报。
		catalog, drift, err := plugin.LoadCuratedForRuntime(root, installed)
		if err != nil {
			return plugin.CuratedRead{Roots: roots, Path: path, Err: err}
		}
		return plugin.CuratedRead{Catalog: catalog, Roots: roots, Path: path, Drift: drift}
	}
	return plugin.CuratedRead{Roots: roots, Absent: true, Err: fmt.Errorf(
		"责任链上 %d 个根都没有 %s（根解析见 PluginRootChain；用 -plugins 或 $SEELEX_PLUGINS 指定插件根）",
		len(roots), plugin.CuratedFileName)}
}

// CuratedAssemblyJudge 把精选目录读数包成装配面的判决函数（"未定义的名字怎么判/怎么说"）。
//
// 判定的分支与文案全在 `plugin.CuratedRead.Judge`：那里同时有目录（entries / pending /
// 实读页）与"已装名单"，是唯一能一次说清三件事的地方；本函数只负责把**运行时的事实**
// （本进程已定义插件名）递进去，不复制判定。
func CuratedAssemblyJudge(read plugin.CuratedRead) func(name string, installed []string) error {
	return func(name string, installed []string) error {
		return read.Judge(name, installed)
	}
}

func SplitPaths(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
