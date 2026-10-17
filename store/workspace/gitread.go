package workspace

// 只读 git 通道的公共骨架（gitread.go）。
//
// 四条只读 git 查询（提交列表 gitlog.go、提交详情与提交内文件内容
// gitcommit.go、工作区改动 gitchanges.go）共用同一套起命令与失败语义：
// 固定 argv、不经过 shell、带超时、输出走管道；失败一律转成**可展示文案**
// （非 git 仓库、坏对象、超时都走这条路）而不是 Go error——这些失败对 GUI
// 来说是正常展示状态（"这不是 git 仓库"），不是需要中断调用的程序错误。
//
// 命令怎么起、失败怎么说话，只在这一处定义；各查询自己负责的参数校验
// （hash 形状、limit 钳制、路径基准）留在各自文件里。

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/internal/winhide"
)

// runGitRead 跑一次只读 git 命令。成功返回 stdout 原始字节；失败返回
// （nil, 展示文案）：stderr 优先（git 的报错最准确），stderr 为空时退回
// 进程错误，超时用调用方给的 timeoutMessage（各查询的文案各自表达"在查
// 什么"，见调用处）。
func runGitRead(argv []string, timeout time.Duration, timeoutMessage string) ([]byte, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", argv...)
	winhide.Apply(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = runErr.Error()
		}
		if ctx.Err() != nil {
			message = timeoutMessage
		}
		return nil, message
	}
	return stdout.Bytes(), ""
}

// resolveGitRoot 校验 git 查询的 root：非空、绝对化、且确实是一个目录。
// 返回归一化后的绝对路径（git 的 -C 用地）。
func resolveGitRoot(root, label string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%s: root required", label)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%s: resolve root: %w", label, err)
	}
	rootAbs = filepath.Clean(rootAbs)
	info, statErr := os.Stat(rootAbs)
	if statErr != nil {
		return "", fmt.Errorf("%s: inspect root: %w", label, statErr)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s: root is not a directory", label)
	}
	return rootAbs, nil
}
