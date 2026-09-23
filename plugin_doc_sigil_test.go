package main

// 插件文档的输入前缀守卫。
//
// 2026-09-17 的「输入前缀一字符一含义」把 `#` 从 Skill 改成「切换 Plugin」、
// 由 `$` 接管 Skill（见 application/core/completion.go 的 sigil 表）。当时
// `plugins/default/plugin.md` 里那句「通过 `#plan` 注入默认 Plan Skill」没人跟着
// 改，于是：用户照着打 `#plan` 会去切一个不存在的插件（`plan` 是 Skill），模型
// 也会拿着这句话去教用户——一个纯文档漂移造成的服务发现故障。
//
// 这条守卫把「插件文档用 `#` 引用本插件自己的 Skill」钉成失败：插件目录名
// （SKILL.md 所在目录）就是 Skill 名，文档里出现 `#<skill>` 即红。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPluginDocsDoNotUsePluginSigilForSkills(t *testing.T) {
	pluginEntries, err := os.ReadDir("plugins")
	if err != nil {
		t.Fatalf("read plugins dir: %v", err)
	}
	checked := 0
	for _, pluginEntry := range pluginEntries {
		if !pluginEntry.IsDir() {
			continue
		}
		pluginDir := filepath.Join("plugins", pluginEntry.Name())
		skillNames := pluginSkillNames(t, pluginDir)
		if len(skillNames) == 0 {
			continue
		}
		docs := []string{filepath.Join(pluginDir, "plugin.md"), filepath.Join(pluginDir, "README.md")}
		for _, doc := range docs {
			content, err := os.ReadFile(doc)
			if err != nil {
				t.Fatalf("read %s: %v", doc, err)
			}
			text := string(content)
			for _, skill := range skillNames {
				// 只认「独立出现」的 #<skill>：前面不是词字符，后面不是词字符/-。
				pattern := regexp.MustCompile(`(^|[^\w-])#` + regexp.QuoteMeta(skill) + `([^\w-]|$)`)
				if match := pattern.FindString(text); match != "" {
					t.Errorf("%s 用 `#%s` 引用 Skill：`#` 是切换 Plugin 的前缀，Skill 前缀是 `$`（命中 %q）",
						doc, skill, strings.TrimSpace(match))
				}
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到任何插件文档：守卫失效（目录结构变了？）")
	}
}

// pluginSkillNames 返回插件目录下的 Skill 名（= 含 SKILL.md 的子目录名）。
func pluginSkillNames(t *testing.T, pluginDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		t.Fatalf("read %s: %v", pluginDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(pluginDir, entry.Name(), "SKILL.md")); err == nil {
			names = append(names, entry.Name())
		}
	}
	return names
}
