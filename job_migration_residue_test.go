package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRetiredJobToolNamesLeaveNoResidue 是打点 L-2/L-4/L-7 的落地判据（TC-L2-3 /
// TC-L7-1）：旧名字 `async_output` / `async_kill` 必须在**代码与配置**里零残留。
//
// 为什么值得一条仓库级用例：工具下线最容易死在"注册删了、权限组还留着"或"注释里
// 还写着旧名字"——前者是一条活着的注册路径，后者会让下一个人照着写回来。允许出现的
// 只有**明确标注为迁移说明**的行（已下线 / 旧名 / 替代 / 残留），以及 docs/ 与
// CHANGELOG.md 这类历史记录。
func TestRetiredJobToolNamesLeaveNoResidue(t *testing.T) {
	retired := []string{"async_output", "async_kill"}
	markers := []string{"已下线", "下线", "已作废", "旧名", "旧入参", "旧 ", "旧`", "替代", "迁移", "残留", "retired"}
	skipDirs := map[string]bool{
		".git": true, "vendor": true, "dist": true, "node_modules": true,
		"_tmp": true, "_logs": true, "tmp": true, "local": true, ".seelex": true,
		".venv": true, "bin": true, "docs": true, ".claude": true, ".qoder": true,
		".gocache": true, ".githooks": true, ".github": true,
	}
	scanned := 0
	var hits []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		isGo := strings.HasSuffix(name, ".go")
		isConfig := strings.HasSuffix(name, ".yaml") && strings.HasPrefix(filepath.ToSlash(path), "config/")
		if !isGo && !isConfig {
			return nil
		}
		if name == "CHANGELOG.md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		scanned++
		for index, line := range strings.Split(string(data), "\n") {
			for _, token := range retired {
				if !strings.Contains(line, token) {
					continue
				}
				allowed := false
				for _, marker := range markers {
					if strings.Contains(line, marker) {
						allowed = true
						break
					}
				}
				if !allowed {
					hits = append(hits, filepath.ToSlash(path)+":"+strconv.Itoa(index+1)+": "+strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("没有扫到任何 .go / config 文件：用例本身失效，比断言失败更糟")
	}
	if len(hits) > 0 {
		t.Fatalf("旧作业工具名仍有残留（%d 处）——名字下线必须是「代码与配置零残留」：\n%s",
			len(hits), strings.Join(hits, "\n"))
	}
}
