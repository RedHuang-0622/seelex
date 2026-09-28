package workspace

// 工作树文件写入（「资源管理器 → 文件详情」面板的编辑保存）：WriteFile。
//
// 与 ReadFile 共用同一条可见性边界（resolveFileTarget：相对路径 + containment、
// 忽略目录与敏感文件名过滤、目录与符号链接拒绝），因此**读面看不见的文件同样写
// 不动**——编辑面只可能比读面更严，不可能更松（用户能点开哪个文件，才能保存哪个）。
//
// 三条产品口径（不是可选配置）：
//
//  1. **只写文本**：目标文件已存在且二进制探测为 false 时拒绝写入。文本编辑器把
//     UTF-8 正文落到二进制上只会毁文件（前端不给编辑入口，这里是服务端兜底）。
//  2. **原子发布**：同目录临时文件 + flush + rename（与 workspace_index 同一
//     writeFileAtomic）。保存成功即"磁盘上的内容就是用户刚看到的那一份"，中途失败
//     原文件不变——这是"保存后读回同步基线"这条前端不变量成立的前提。
//  3. **上限硬钳制**：单次写入 ≤ fileWriteHardLimit，超出**拒绝**而不是截断
//     （截断会静默毁文件）。
//
// 另外只覆盖**已存在**的常规文件：本通道的语义是"保存面板里打开的那个文件"，没有
// 新建入口（目标不存在即显式失败，而不是悄悄新建一个同名文件）。

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

const (
	// fileWriteHardLimit 是单次写入的硬上限（字节）：与预览读取同一量级
	// （编辑器不可能产出这种体量的正文）。
	fileWriteHardLimit = 64 << 20 // 64 MiB
	// binaryProbeBytes 是"目标文件是否二进制"的探测窗口（字节）：只看文件头
	// 一小段，不做全文扫描。
	binaryProbeBytes = 4 << 10
)

// WriteFile 用 content 覆盖 root 内 relPath 文件的**全部内容**（GUI 文件详情
// 编辑保存数据源）。成功返回写入后的相对路径与字节数；调用方随后读回同一路径即可
// 把编辑器基线同步到实际文件。
//
// 只覆盖**已存在**的常规文本文件：新建文件不在本通道的语义内（面板的入口是"从工作
// 树打开一个文件"，没有新建入口），删掉重来也一样（目标不存在即显式失败，而不是
// 悄悄新建一个同名文件把内容写进去）。
func (r *Repo) WriteFile(root, relPath string, content []byte) (dto.FileWriteResult, error) {
	_, fileAbs, rel, err := resolveFileTarget(root, relPath)
	if err != nil {
		return dto.FileWriteResult{}, err
	}
	if int64(len(content)) > fileWriteHardLimit {
		return dto.FileWriteResult{}, fmt.Errorf(
			"workspace file: write %q: content exceeds the %d byte limit", relPath, int64(fileWriteHardLimit))
	}

	// resolveFileTarget 已保证目标是根内的常规文件（非目录、非符号链接），因此这里
	// 的 Lstat 只用来取原权限位与做二进制探测（目标在两步之间被删就显式失败）。
	info, statErr := os.Lstat(fileAbs)
	if statErr != nil {
		return dto.FileWriteResult{}, fmt.Errorf("workspace file: write %q: inspect: %w", relPath, statErr)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return dto.FileWriteResult{}, fmt.Errorf("workspace file: write %q: not a regular file", relPath)
	}
	probe, probeErr := readProbeBytes(fileAbs)
	if probeErr != nil {
		return dto.FileWriteResult{}, fmt.Errorf("workspace file: write %q: inspect content: %w", relPath, probeErr)
	}
	if !textLikeBytes(probe) {
		return dto.FileWriteResult{}, fmt.Errorf("workspace file: write %q: binary files are not editable", relPath)
	}

	if err := writeFileAtomic(fileAbs, content, info.Mode().Perm()); err != nil {
		return dto.FileWriteResult{}, fmt.Errorf("workspace file: write %q: %w", relPath, err)
	}
	return dto.FileWriteResult{
		Path:  filepath.ToSlash(rel),
		Size:  int64(len(content)),
		Limit: fileWriteHardLimit,
	}, nil
}

// readProbeBytes 读取文件头部至多 binaryProbeBytes 字节，供二进制探测。
// 空文件（首次 Read 即 EOF）返回空切片而不是错误：空内容可安全按文本编辑。
func readProbeBytes(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	probe := make([]byte, binaryProbeBytes)
	read, err := file.Read(probe)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return probe[:read], nil
}
