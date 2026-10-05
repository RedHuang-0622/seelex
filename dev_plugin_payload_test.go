package main

// 运行树**插件载荷刷新通路**的守卫（2026-10-05 事故的直接回归）。
//
// 事故：dist/seelex-gui-dev/plugins/ 是一份**冻结的快照**——仓库在 10-05 02:23 的提交里
// 把 curated.yaml 加进 plugins/（那次提交确实跑了 post-commit hook：dist/dev 与 P2 两个
// exe 的 mtime 与提交时间只差 3~6 秒），但包内那份至今缺席，app 启动期报"精选目录读不到
// （责任链上 3 个根都没有）"。根因是差异判据写成了管道取反：
//
//	! diff -rq "$src" "$dest" 2>/dev/null | grep -qv "Only in $dest"
//
// 而 build-dev.sh 第 15 行是 `set -o pipefail`——管道退出码取**最右非零成员**，于是这句
// 取反读到的是 diff 的 1（"有差异"），恒为真 ⇒ 每次都 early return，同步**从未执行过**。
// 实测复现：同一夹具下无 pipefail = WOULD-COPY、有 pipefail = EARLY-RETURN。
//
// 这条守卫钉三件事：判据不再依赖管道退出码；两条通路（hook 的 build-dev.sh 与 flow 的
// sync-dev-plugins.ps1）都在且指向同一个目标；flow 的 Deploy 真的会调用它。

import (
	"strings"
	"testing"
)

// nonCommentLines 去掉整行注释（注释里会引用那条坏写法当反例，不该被判成回归）。
func nonCommentLines(text string) string {
	kept := make([]string, 0, 64)
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func TestDevPluginPayloadRefreshIsWired(t *testing.T) {
	t.Parallel()

	// ① 判据不得再依赖管道退出码。
	dev := readRepoFile(t, "scripts/build-dev.sh")
	if !strings.Contains(dev, "set -o pipefail") {
		t.Fatal("build-dev.sh 不再 `set -o pipefail`：这条守卫的前提变了，请重读插件同步那一段")
	}
	code := nonCommentLines(dev)
	for _, banned := range []string{`grep -qv "Only in`, "! diff -rq"} {
		if strings.Contains(code, banned) {
			t.Errorf("build-dev.sh 的插件同步判据又写成管道取反（%q）：pipefail 下它恒为真，同步会静默不执行", banned)
		}
	}
	if !strings.Contains(code, `cmp -s "$file" "$dest/$rel"`) {
		t.Error("build-dev.sh 的插件同步判据必须是逐文件内容比对（cmp -s）：时间戳不可靠（cp -r 不保、Copy-Item 保）")
	}
	for _, token := range []string{"dist/seelex-gui-dev/plugins", "只覆盖"} {
		if !strings.Contains(dev, token) {
			t.Errorf("build-dev.sh 的插件同步缺少口令 %q", token)
		}
	}

	// ② 第二条通路：PowerShell 侧脚本（路径取自 build-layout.ps1 的单一真源）。
	sync := readRepoFile(t, "scripts/sync-dev-plugins.ps1")
	for _, token := range []string{
		"build-layout.ps1", "Get-SeelexLayout", "DevBaselineDir",
		"Assert-DistRootCleanLayout", "Copy-Item", "只覆盖", "保留本机自加",
	} {
		if !strings.Contains(sync, token) {
			t.Errorf("scripts/sync-dev-plugins.ps1 缺少令牌 %q", token)
		}
	}

	// ③ 通路必须真的被接上：Deploy 会刷插件载荷，且能单独跑（-Stage SyncPlugins）。
	flow := readRepoFile(t, "scripts/seelex-flow.ps1")
	for _, token := range []string{
		`"sync-dev-plugins.ps1"`, "Invoke-SyncPlugins", `"SyncPlugins"`, "Invoke-SyncPlugins\n",
	} {
		if !strings.Contains(flow, token) {
			t.Errorf("scripts/seelex-flow.ps1 缺少令牌 %q：只换二进制会让运行树的插件载荷永久冻结", token)
		}
	}
	if !strings.Contains(flow, "Invoke-SyncPlugins\n    Save-StashCopy") {
		t.Error("Deploy 里插件载荷刷新必须发生在覆盖二进制之前（Invoke-SyncPlugins → Save-StashCopy → Copy-Item）")
	}

	// ④ 文档/入口面：分区表、脚本清单与 Makefile 都得写明这条通路，否则下一个人仍会以为
	//    "Deploy 只换二进制"。
	for _, rel := range []string{".claude/build-convention.md", "scripts/README.md"} {
		if text := readRepoFile(t, rel); !strings.Contains(text, "sync-dev-plugins") {
			t.Errorf("%s 没有写明插件载荷刷新通路（sync-dev-plugins）", rel)
		}
	}
	if makefile := readRepoFile(t, "Makefile"); !strings.Contains(makefile, "sync-dev-plugins") {
		t.Error("Makefile 缺少 sync-dev-plugins 目标")
	}
}
