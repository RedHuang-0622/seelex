package ssh

// files.go —— 远端**文件面**：读 / 写 / 替换（前端工具 read_file / write_file /
// edit_file 的 ssh 一支）。
//
// 为什么是三条独立动作而不是把三个工具各写一遍：三个工具真正需要的只是"这台的这几个
// 字节"，判据（行窗口、替换次数、回执形状）留在工具层；本包只负责把字节搬对。
//
// 与 SFTP 的关系：本文件走的是**已有连接上的命令通道**（base64 承载），不是 SFTP
// 子系统（设计稿 §12 D-1 仍未决）。理由：SFTP 要新增一套协议实现与依赖，而 base64
// 在命令通道上是字节透明的、并把"远端要有什么"压到最低（只要 `base64`）；代价是一次
// 多几倍的数据量，对工具级的读写（几十 KB）可以接受。

import (
	"context"
	"strings"
)

// FileContent 是一次远端读的事实回报（Data 是原样字节，未做换行归一）。
type FileContent struct {
	Data []byte
	// Truncated 表示内容在输出上限处被截断（**不是**文件就这么大）。
	Truncated bool
	Bytes     int
}

// ReadFile 读一个远端文件的全部字节（Content 的窗口切分由调用方做）。
//
// 远端只做两件事：把路径解码出来、把文件 base64 流回本端。为什么整文件读而不是远端
// 切行：切行要再引入 sed/head 的口径（末行无换行、CRLF、二进制），而"本地切"与本地
// 面（os.ReadFile + Split("\n")）逐字同源——同一份判据，不允许两份。
func (b *Backend) ReadFile(ctx context.Context, path string) (FileContent, error) {
	script := strings.Join([]string{
		decodeVar("__p", path),
		`[ -f "$__p" ] || { echo "not a regular file" >&2; exit 91; }`,
		`base64 < "$__p" || exit 92`,
	}, "; ")
	res, err := b.runInline(ctx, script, "")
	if err != nil {
		return FileContent{}, err
	}
	if res.ExitCode != 0 {
		return FileContent{}, remoteScriptError("read_file", res)
	}
	data, truncated := decodeRemoteBase64(res.Stdout, res.Truncated)
	if len(data) == 0 && res.Stdout != "" && !truncated {
		return FileContent{}, codeErr(CodeRemoteFileIO, "read_file: 远端返回的 base64 无法解码（%d 字节）", len(res.Stdout))
	}
	return FileContent{Data: data, Truncated: truncated || res.Truncated, Bytes: len(data)}, nil
}

// WriteFile 覆盖写一个远端文件（内容走内容通道；父目录按需创建）。
//
// 原子性：先写同目录临时名再 `mv -f`——远端没有本地的 per-path actor（那是本机进程内
// 的锁，跨机器不成立），所以这里只保证"不会看到一个写了一半的文件"，不保证两个并发
// 远端写之间的顺序（如实记在 README 的并发一节）。
func (b *Backend) WriteFile(ctx context.Context, path, content string) (int, error) {
	script := strings.Join([]string{
		decodeVar("__p", path),
		`__d=$(dirname "$__p") || exit 91`,
		`mkdir -p "$__d" || exit 91`,
		`__t="$__p.seelex-tmp" || exit 91`,
		`base64 -d > "$__t" || { rm -f "$__t"; exit 92; }`,
		`mv -f "$__t" "$__p" || { rm -f "$__t"; exit 92; }`,
	}, "; ")
	res, err := b.runInline(ctx, script, encodePayload(content))
	if err != nil {
		return 0, err
	}
	if res.ExitCode != 0 {
		return 0, remoteScriptError("write_file", res)
	}
	return len(content), nil
}

// EditFile 在远端文件里做"全部出现处替换"，返回替换次数（与本地 actor 的 Edit 同语义）。
//
// 为什么是"读-替换-写"两趟而不是远端一次 sed：替换串与原文都是任意 UTF-8（可能是
// 中文、正则元字符、斜杠），交给 sed 就要处理它的一整套转义与方言；而在本端做替换，
// 判据与本地面是同一个（strings.Count / strings.ReplaceAll）。代价是两趟往返与
// 一个"读到写之间远端可能被别人改了"的窗口——如实记在 README。
func (b *Backend) EditFile(ctx context.Context, path, oldString, newString string) (int, error) {
	if oldString == "" {
		return 0, codeErr(CodeNoCommand, "edit_file: old_string 不能为空")
	}
	content, err := b.ReadFile(ctx, path)
	if err != nil {
		return 0, err
	}
	if content.Truncated {
		// 半份内容上做替换 = 用残缺文本覆盖远端文件。宁可拒绝。
		return 0, codeErr(CodeRemoteFileIO,
			"edit_file: 远端文件超过读取上限（%d 字节已读），拒绝在残缺内容上替换", content.Bytes)
	}
	text := string(content.Data)
	if !strings.Contains(text, oldString) {
		return 0, codeErr(CodeRemoteEditNoMatch, "edit_file: old_string not found in %q", path)
	}
	count := strings.Count(text, oldString)
	if _, err := b.WriteFile(ctx, path, strings.ReplaceAll(text, oldString, newString)); err != nil {
		return 0, err
	}
	return count, nil
}
