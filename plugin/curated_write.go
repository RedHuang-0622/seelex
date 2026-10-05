package plugin

// 精选目录的**写侧**：把"已经落盘、却还没登记"的插件补进 `curated.yaml`。
//
// 为什么要有它（2026-10-05 实读）：`plugin_create` 让 main agent 能自己长插件（可以带
// skill），写侧落在 `Loader.PrimaryRoot`；但精选目录是一份**声明面**——新目录没有对应
// entry，目录里就没有"这台机器此刻有什么"的读数。使用者的选择只有两个：每次启动背一条
// 他改不动、也不该由他改的警告，或者手工把插件抄进 YAML。两个都不合理。
//
// 这一层的语义是「**发现 → 读回 → 落进 yaml**」：磁盘是事实，目录是事实的读数；既然插件
// 已经在根里（而且是 agent 自己刚写下去的），目录就该被顶上来，而不是把机器该做的簿记
// 推给人。
//
// 五条纪律：
//  1. **只追加、不重排**：已有 entries / pending / preset 逐字节保留（文本插入，不重新
//     序列化整份文件）——注册不能把别人的簿记洗掉，注释也是簿记。
//  2. **只登记本根的东西**：只补 `RootDir` 落在这个根下面的插件。多根 first-wins 下，
//     别的根供上来的插件不属于这份目录，登记进来会造出第二条谎（目录说"它在根里"，
//     而它根本不在）。
//  3. **本机自建就是本机自建**：补的条目 `source.kind = local`，url 指向它所在的根，
//     license/pinned 明说"未声明（本机自建，不入发行包）"——它不是发行载荷，不许被读成
//     "上游给的"（铁律 3：每条 entry 必有 source，出处必须诚实）。
//  4. **给装配路径**：新条目挂进一个 `local` preset。否则目录里会同时留下"已落盘但不属于
//     任何 preset"的漂移，装配面也就没有它的权限档可读。
//  5. **落盘前自检**：写回的那份必须先解得动、且过结构守卫，否则拒绝落盘——宁可不登记，
//     也不许把一份读不动的目录留在运行树上（那会把"目录坏了"变成下一次启动的现场）。
//
// 幂等：目录已经登记过的名字不会被重复追加；无事可做时一个字节都不写。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// CuratedSourceLocal 是本机自建插件的 `source.kind`：来源是本机的创建动作（agent
	// 脚手架 plugin_create、或手工放置），**不是**上游发行源。用它把"发行载荷"与
	// "这台机器自己长的"分开。
	CuratedSourceLocal = "local"
	// CuratedLocalPreset 是本机自建插件的装配路径（preset 名）。注册时它们被挂在这里；
	// 权限档跟其余 preset 一样只能由 preset 给（插件不得自带），这里取产品认识的 manual。
	CuratedLocalPreset = "local"
	// curatedReadAtLayout 与目录里 `read_at` / `source.read_at` 的日期口径一致。
	curatedReadAtLayout = "2006-01-02"
)

// topLevelKeyLine 认出顶层键行（`entries:` / `presets:` …）：插入点就靠它划块。
var topLevelKeyLine = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*:`)

// LocalEntry 依据一个**已落盘**的插件造一条本机自建条目。description 直接取 manifest 里
// 的那句话——"读回目录"读的就是磁盘上的事实，不是调用方现编的。
func LocalEntry(p Plugin, readAt string) CuratedEntry {
	description := strings.TrimSpace(p.Description)
	if description == "" {
		description = "本机自建插件（运行期发现后自动登记；manifest 未写 description）"
	}
	root := strings.TrimSpace(p.RootDir)
	if root == "" {
		root = "(未知根)"
	}
	return CuratedEntry{
		Name:        p.Name,
		Description: description,
		Installed:   true,
		Source: CuratedSource{
			Kind:    CuratedSourceLocal,
			URL:     "local:" + root,
			License: "未声明（本机自建，不入发行包）",
			Pinned:  "随本机文件（非发行载荷）",
			ReadAt:  readAt,
		},
	}
}

// RegisterDiscoveredPlugins 把 root 下**已落盘但目录里没登记**的插件补进
// `root/curated.yaml`，返回本次新登记的名字（顺序稳定 = 调用方给的顺序）。
//
// 无事可做的三种情形都返回 (nil, nil) 且不写盘：目录文件不存在（自建根/用户树本来可以
// 不带精选目录，这是合法现场）、没有本根下未登记的插件、名字已在 pending 里（路线图候选
// 不是"待登记"，它按定义不在盘上）。
func RegisterDiscoveredPlugins(root string, loaded []Plugin) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("精选目录登记: 插件根为空")
	}
	path := filepath.Join(root, CuratedFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("精选目录登记: 读 %s: %w", path, err)
	}
	catalog, err := ParseCuratedCatalog(data)
	if err != nil {
		// 读不动就不改：宁可不登记，也不许把一份读不动的目录留在运行树上。
		return nil, fmt.Errorf("精选目录登记: %s 读不动，拒绝改写: %w", path, err)
	}

	listed := make(map[string]bool, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		listed[entry.Name] = true
	}
	roadmap := make(map[string]bool)
	for _, preset := range catalog.Presets {
		for _, item := range preset.Pending {
			roadmap[item.Name] = true
		}
	}

	readAt := time.Now().Format(curatedReadAtLayout)
	missing := make([]CuratedEntry, 0, len(loaded))
	names := make([]string, 0, len(loaded))
	for _, p := range loaded {
		if listed[p.Name] || roadmap[p.Name] || !pluginLivesInRoot(p, root) {
			continue
		}
		missing = append(missing, LocalEntry(p, readAt))
		names = append(names, p.Name)
	}
	if len(missing) == 0 {
		return nil, nil
	}

	localPlugins := make([]string, 0, len(missing))
	for _, preset := range catalog.Presets {
		if preset.Name == CuratedLocalPreset {
			localPlugins = append(localPlugins, preset.Plugins...)
			break
		}
	}
	for _, name := range names {
		if !containsString(localPlugins, name) {
			localPlugins = append(localPlugins, name)
		}
	}

	next, err := insertCuratedEntries(string(data), missing)
	if err != nil {
		return nil, fmt.Errorf("精选目录登记: 渲染条目: %w", err)
	}
	next, err = upsertCuratedLocalPreset(next, localPlugins)
	if err != nil {
		return nil, fmt.Errorf("精选目录登记: 渲染 preset: %w", err)
	}
	if strings.HasSuffix(string(data), "\n") && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	if err := verifyCuratedRewrite(next); err != nil {
		return nil, fmt.Errorf("精选目录登记: %s 登记后自检失败，拒绝落盘: %w", path, err)
	}
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return nil, fmt.Errorf("精选目录登记: 写 %s: %w", path, err)
	}
	return names, nil
}

// pluginLivesInRoot 判一个插件目录是否就落在 root 下面（多根 first-wins 下的归属判定）。
func pluginLivesInRoot(p Plugin, root string) bool {
	dir := strings.TrimSpace(p.RootDir)
	if dir == "" {
		return false
	}
	parent := filepath.Dir(filepath.Clean(dir))
	return sameFilePath(parent, filepath.Clean(root))
}

// sameFilePath 比对两条路径是否同一处（Windows 上大小写不敏感——同一个根可能以两种写法
// 出现在责任链上，见 main.go 的 dedupRoots）。
func sameFilePath(a, b string) bool {
	if a == b {
		return true
	}
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return false
}

// verifyCuratedRewrite 是落盘前的自检：新文本必须解得动、且过结构守卫（文件自身成立与否，
// 与"这台机器装了什么"无关的那一半——entries ↔ 已装集合那一半由发行守卫/运行期漂移各管
// 各的，不在这里判）。
func verifyCuratedRewrite(document string) error {
	catalog, err := ParseCuratedCatalog([]byte(document))
	if err != nil {
		return fmt.Errorf("解析失败: %w", err)
	}
	if problems := validateCuratedCatalogStructure(catalog); len(problems) > 0 {
		return fmt.Errorf("结构校验失败:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// insertCuratedEntries 把新条目插进 `entries:` 序列的**末尾**，逐字节保留文件其余部分。
// 两种既有形态都认：块序列（`entries:` + `  - name:`…）与内联空表（`entries: []`）。
func insertCuratedEntries(document string, entries []CuratedEntry) (string, error) {
	block, err := curatedSequenceBlocks(entries)
	if err != nil {
		return "", err
	}
	lines := strings.Split(document, "\n")
	for index, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed != "entries:" && trimmed != "entries: []" {
			continue
		}
		if trimmed == "entries: []" {
			lines[index] = "entries:"
			return spliceLines(lines, index+1, index+1, block), nil
		}
		end := index + 1
		for end < len(lines) && !topLevelKeyLine.MatchString(lines[end]) {
			end++
		}
		// 插在块里最后一个非空行之后：保住块与下一个顶层键之间的空行（人写的版式）。
		anchor := index
		for i := index + 1; i < end; i++ {
			if strings.TrimSpace(lines[i]) != "" {
				anchor = i
			}
		}
		return spliceLines(lines, anchor+1, anchor+1, block), nil
	}
	return document, nil
}

// upsertCuratedLocalPreset 保证目录里有一个 `local` preset，其 `plugins` 恰好是 localPlugins
// （原有成员 ∪ 本次登记）。存在则整块替换（这块的版式由本文件生成，可控），不存在则追加；
// 两种形态都逐字节保留其余部分。
func upsertCuratedLocalPreset(document string, localPlugins []string) (string, error) {
	preset := CuratedPreset{
		Name:           CuratedLocalPreset,
		Description:    "本机自建插件的装配路径（运行期发现后自动登记；不随发行包）",
		PermissionTier: "manual",
		Plugins:        append([]string(nil), localPlugins...),
		Pending:        []CuratedPending{},
	}
	block, err := curatedSequenceBlocks([]CuratedPreset{preset})
	if err != nil {
		return "", err
	}
	lines := strings.Split(document, "\n")
	start := -1
	for index, line := range lines {
		if strings.TrimRight(line, " \t\r") == "  - name: "+CuratedLocalPreset {
			start = index
			break
		}
	}
	if start >= 0 {
		end := start + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "  - ") && !topLevelKeyLine.MatchString(lines[end]) {
			end++
		}
		return spliceLines(lines, start, end, block), nil
	}
	for index, line := range lines {
		if strings.TrimRight(line, " \t\r") == "presets: []" {
			lines[index] = "presets:"
			return spliceLines(lines, index+1, index+1, block), nil
		}
	}
	anchor := len(lines) - 1
	for anchor > 0 && strings.TrimSpace(lines[anchor]) == "" {
		anchor--
	}
	return spliceLines(lines, anchor+1, anchor+1, block), nil
}

// curatedSequenceBlocks 把一份 YAML 序列**渲染成 2 空格缩进的序列项**（每项一行起头
// `  - `，其余行 +4 空格）。用它把 yaml.Marshal 的结构塞进既有的块里，不必重新序列化
// 整份文件。
func curatedSequenceBlocks[T any](items []T) ([]string, error) {
	lines := make([]string, 0, len(items)*8)
	for _, item := range items {
		encoded, err := yaml.Marshal(item)
		if err != nil {
			return nil, err
		}
		body := strings.Split(strings.TrimRight(string(encoded), "\n"), "\n")
		for index, line := range body {
			if index == 0 {
				lines = append(lines, "  - "+line)
				continue
			}
			lines = append(lines, "    "+line)
		}
	}
	return lines, nil
}

// spliceLines 把 [from, to) 换成 insert（原地语义、返回新切片），供各处文本插入共用。
func spliceLines(lines []string, from, to int, insert []string) string {
	merged := make([]string, 0, len(lines)-(to-from)+len(insert))
	merged = append(merged, lines[:from]...)
	merged = append(merged, insert...)
	merged = append(merged, lines[to:]...)
	return strings.Join(merged, "\n")
}

// writeFileAtomic 同目录临时文件 + rename：写一半崩掉也不会把目录留成读不动的现场。
func writeFileAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".curated-*.yaml")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName) // 成功路径已被 rename 掉，这里是失败路径的兜底
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

// RegisterDiscoveredPlugins 是 Manager 上的写侧入口：走与读侧**同一条** first-wins 责任链
// 找那份真正在用的 curated.yaml（链上第一个带它的根），把本根下未登记的插件补进去。
// 没有任何根带目录时返回 (nil, nil)——那是合法现场，不是失败。
func (m *Manager) RegisterDiscoveredPlugins() ([]string, error) {
	if m == nil || m.loader == nil {
		return nil, nil
	}
	for _, root := range m.loader.roots {
		if _, err := os.Stat(filepath.Join(root, CuratedFileName)); err != nil {
			continue
		}
		return RegisterDiscoveredPlugins(root, m.All())
	}
	return nil, nil
}
