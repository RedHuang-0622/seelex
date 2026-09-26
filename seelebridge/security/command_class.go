package security

import (
	"strings"
	"unicode"
)

// command_class.go — `bash_read` 的**服务端判定**（设计文档 §A.4，打点 K-4）。
//
// 生态位：模型面上有三个 bash 名字——`bash`（串行写类）、`bash_read`（只读、免打断）、
// `bash_bg`（后台受管）。`bash_read` 落在只读簇 ⇒ 默认 allow ⇒ 子代理与只读员工天然
// 拿得到、且不弹审批。**这条便利必须由服务端自己判定，不能由模型自报**：
// ClassifyCommand 只吃命令字符串，签名上就拿不到模型的任何主张（"这是只读的"这类
// 说明不构成授权依据），与 pathgate.go 的"服务端按规则判定"同源。
//
// 判定口径（保守默认）：分类失败一律按**写**处理。
//
//  1. 命令为空、或含 shell 复合/重定向/替换语法（`;` `&&` `||` `|` `>` `` ` `` `$X`
//     换行）→ 拒绝：复合命令里第一词的只读性不代表整条命令只读；
//  2. 首词必须在只读白名单里；
//  3. 两词族（`git` / `go` / `dotnet`）必须命中只读子命令，其余子命令一律按写处理；
//  4. 已知的写形态参数（`find -delete`、`git diff --output=…`）拒绝；需要"看着像只读
//     其实会写"的形态（`git config`、`git branch`、`sed -i`）**整族不进白名单**。
//
// 它**不是**沙箱：判成的"只读"不改变 cwd 门禁与凭据清理，只决定这一次调用能不能走
// `bash_read`。要写就换 `bash`（rw 簇、规则照旧），不得静默降级成执行。

// readOnlyFirstWords 是只读命令的白名单（首词）。刻意保持窄：宁可让模型改用 bash，
// 也不要把一个会写盘的工具放进只读簇。交互式命令（less/top/…）同样不收：
// 挂住的"只读"命令对模型与用户都没有价值。
var readOnlyFirstWords = map[string]bool{
	// 目录与文件读取
	"ls": true, "dir": true, "pwd": true, "cat": true, "type": true, "head": true, "tail": true,
	"file": true, "stat": true, "wc": true, "tree": true,
	// 搜索与过滤（只读形态；sed/awk 因"看着像只读、其实可写"整族不收）
	"grep": true, "rg": true, "find": true, "fd": true, "cut": true, "sort": true, "uniq": true,
	"tr": true, "column": true, "jq": true,
	// 环境与时间读取
	"echo": true, "printf": true, "date": true, "whoami": true, "which": true, "where": true,
	"printenv": true, "uname": true, "hostname": true, "id": true, "uptime": true,
	// 资源用量
	"df": true, "du": true, "ps": true, "free": true,
	// 语言/工具链的只读查询（写子命令见 readOnlySubcommands / writeSubcommands）
	"git": true, "go": true, "dotnet": true,
	// 哈希与比对
	"sha256sum": true, "md5sum": true, "shasum": true, "cmp": true, "diff": true,
}

// readOnlySubcommands 是两词族的**唯一放行面**（首词 → 子命令集合）：不在这里的子命令
// 一律按写处理，`git commit` / `git config` / `go mod tidy` 都是写。把 `git branch` /
// `git tag` / `git config` 这类"参数决定读写"的子命令整族排除，是为了让"白名单"保持
// 可枚举：分类失败按写处理的代价只是模型改用 bash，不是功能缺失。
var readOnlySubcommands = map[string]map[string]bool{
	"git": {
		"status": true, "log": true, "diff": true, "show": true, "blame": true, "shortlog": true,
		"rev-parse": true, "ls-files": true, "ls-tree": true, "cat-file": true, "describe": true,
		"for-each-ref": true, "rev-list": true, "grep": true, "whatchanged": true, "version": true,
		"stash": true, // 只有 `git stash list` 放行，见 readOnlySubcommandArgs
	},
	"go": {
		"test": true, "build": true, "vet": true, "list": true, "doc": true, "env": true,
		"version": true, "why": true,
	},
	"dotnet": {
		"build": true, "test": true, "list": true, "sdk": true, "nuget": true,
	},
}

// readOnlySubcommandArgs 是"子命令本身只读、但必须带某个参数才只读"的白名单。
// git stash 不带 list 就是创建一个 stash（写 refs），因此只放行 `list`。
var readOnlySubcommandArgs = map[string]map[string]string{
	"git": {"stash": "list"},
}

// shellSyntaxTokens 是"复合命令/重定向/替换"的语法标记：出现任何一个即拒绝。
// 为什么不逐段解析：`a | b` 里 a 只读不代表 b 只读，`$(...)` 里的命令完全不可见。
var shellSyntaxTokens = []string{
	">", "|", "&", ";", "`", "$", "\n", "\r", "\x00", "<",
}

// writeTokens 是**参数级写形态**：首词/子命令只读，但某个参数会让它写盘。
// 整串包含匹配（`--output=build/x` 也要命中）比精确相等保守，符合本文件的口径。
var writeTokens = []string{
	"--output", "--in-place", "--delete", "-delete", "-exec", "-execdir", "-ok", "-okdir",
	"-fprint", "-fprintf", "-fls", "--log-file", "--out-file", "tee",
}

// ClassifyCommand 判定一条 shell 命令能否在 `bash_read` 下执行。
// 返回 false = 按写处理（要么换 `bash`，要么这条命令本就不该免打断）。
func ClassifyCommand(command string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	for _, token := range shellSyntaxTokens {
		if strings.Contains(trimmed, token) {
			return false
		}
	}
	fields := splitCommandWords(trimmed)
	if len(fields) == 0 {
		return false
	}
	first := strings.ToLower(fields[0])
	if !readOnlyFirstWords[first] {
		return false
	}
	for _, field := range fields[1:] {
		for _, token := range writeTokens {
			if strings.Contains(strings.ToLower(field), token) {
				return false
			}
		}
	}
	allowed, twoWordFamily := readOnlySubcommands[first]
	if !twoWordFamily {
		return true
	}
	if len(fields) < 2 || strings.HasPrefix(fields[1], "-") {
		// `git -C repo status` / 裸 `git` 都不判只读：保守默认（模型可用 bash）。
		return false
	}
	sub := strings.ToLower(fields[1])
	if !allowed[sub] {
		return false
	}
	if required, ok := readOnlySubcommandArgs[first][sub]; ok {
		if len(fields) < 3 || !strings.EqualFold(fields[2], required) {
			return false
		}
	}
	return true
}

// splitCommandWords 按空白切词，但**保留引号内的空白**为同一个词，并去掉引号：
// `git log --grep="a b"` 是一个词里的两个词。切错不会造成安全漏洞（语法标记已在
// 前一步整串拒绝），但会让白名单匹配失败——保守默认下宁可不放行。
func splitCommandWords(command string) []string {
	var words []string
	var builder strings.Builder
	var quote rune
	inWord := false
	for _, symbol := range command {
		switch {
		case quote != 0:
			if symbol == quote {
				quote = 0
				continue
			}
			builder.WriteRune(symbol)
			inWord = true
		case symbol == '"' || symbol == '\'':
			quote = symbol
			inWord = true
		case unicode.IsSpace(symbol):
			if inWord {
				words = append(words, builder.String())
				builder.Reset()
				inWord = false
			}
		default:
			builder.WriteRune(symbol)
			inWord = true
		}
	}
	if inWord {
		words = append(words, builder.String())
	}
	return words
}
