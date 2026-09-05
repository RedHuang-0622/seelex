package workspace

// 工作树文件预览读取：ReadFile。
// 与 ListTree 同一安全策略（containment、敏感文件与忽略目录过滤、符号链接
// 拒绝、预算上限），只按需读取受控字节并如实报告截断；字节以 base64 带回，
// 文本/二进制同一通道，前端按扩展名分派渲染。

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

const (
	// filePreviewDefaultLimit 是 limit ≤ 0 时使用的默认上限（字节）。
	filePreviewDefaultLimit = 4 << 20 // 4 MiB
	// filePreviewHardLimit 是单次文件预览读取的硬上限（字节；超限钳制并
	// 报告，防止一次调用把超大文件整份拖进 renderer）。
	filePreviewHardLimit = 64 << 20 // 64 MiB
)

// ReadFile 读取根内 relPath 文件的前 limit 字节（GUI 文件预览数据源）。
// 安全规则与 ListTree 一致：只允许相对路径且不逃逸根；路径任一环节是
// 忽略目录（.git/node_modules/.seelex 等）或敏感文件名（accounts.yaml、
// *.local.yaml）一律拒绝；符号链接直接拒绝（防跟随逃逸）；超上限截断并
// 标记。TextLike 是二进制探测提示（含 NUL 或控制字节占比高 → false），
// 探测只作用于本次读取的字节，不做全文扫描。
func (r *Repo) ReadFile(root, relPath string, limit int64) (dto.FileContent, error) {
	rootAbs, fileAbs, rel, err := resolveFileTarget(root, relPath)
	if err != nil {
		return dto.FileContent{}, err
	}
	_ = rootAbs
	if limit <= 0 {
		limit = filePreviewDefaultLimit
	}
	if limit > filePreviewHardLimit {
		limit = filePreviewHardLimit
	}

	info, err := os.Lstat(fileAbs)
	if err != nil {
		return dto.FileContent{}, fmt.Errorf("workspace file: inspect %q: %w", relPath, err)
	}
	if !info.Mode().IsRegular() {
		return dto.FileContent{}, fmt.Errorf("workspace file: %q is not a regular file", relPath)
	}

	file, err := os.Open(fileAbs)
	if err != nil {
		return dto.FileContent{}, fmt.Errorf("workspace file: open %q: %w", relPath, err)
	}
	defer file.Close()

	truncated := info.Size() > limit
	reader := io.LimitReader(file, limit)
	chunk, err := io.ReadAll(reader)
	if err != nil {
		return dto.FileContent{}, fmt.Errorf("workspace file: read %q: %w", relPath, err)
	}

	return dto.FileContent{
		Name:      filepath.Base(rel),
		Path:      filepath.ToSlash(rel),
		Size:      info.Size(),
		Base64:    base64.StdEncoding.EncodeToString(chunk),
		Limit:     limit,
		Truncated: truncated,
		TextLike:  textLikeBytes(chunk),
	}, nil
}

// resolveFileTarget 校验 root/relPath 并把 relPath 解析为根内文件绝对路径。
// 拒绝绝对路径、逃逸路径、目录与符号链接；路径任一环节命中忽略目录或
// 敏感文件名时拒绝（预览数据源与工作树元数据同一可见性边界）。
func resolveFileTarget(root, relPath string) (rootAbs, fileAbs, rel string, err error) {
	rel = strings.TrimSpace(relPath)
	if rel == "" {
		return "", "", "", fmt.Errorf("workspace file: path required")
	}
	if filepath.IsAbs(rel) {
		return "", "", "", fmt.Errorf("workspace file: absolute path not allowed")
	}
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", fmt.Errorf("workspace file: path escapes workspace root")
	}

	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", "", fmt.Errorf("workspace file: root required")
	}
	rootAbs, err = filepath.Abs(root)
	if err != nil {
		return "", "", "", fmt.Errorf("workspace file: resolve root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	fileAbs = filepath.Clean(filepath.Join(rootAbs, rel))
	if !treeWithinRoot(rootAbs, fileAbs) {
		return "", "", "", fmt.Errorf("workspace file: path escapes workspace root")
	}

	for segment := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if segment == "" {
			continue
		}
		if _, ignored := ignoreDirNames[segment]; ignored {
			return "", "", "", fmt.Errorf("workspace file: %q is not readable (ignored directory)", segment)
		}
		if isSensitiveName(segment) {
			return "", "", "", fmt.Errorf("workspace file: %q is not readable (sensitive name)", segment)
		}
	}

	info, err := os.Lstat(fileAbs)
	if err != nil {
		return "", "", "", fmt.Errorf("workspace file: inspect %q: %w", rel, err)
	}
	if info.IsDir() {
		return "", "", "", fmt.Errorf("workspace file: %q is a directory", rel)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", "", fmt.Errorf("workspace file: %q is a symbolic link (not supported)", rel)
	}
	return rootAbs, fileAbs, rel, nil
}

// textLikeBytes 做轻量二进制探测：含 NUL 字节即视为二进制；控制字节
// （除 \t \n \r \f）占比超过 0.5% 也视为二进制。UTF-8 中文文本正常返回
// true（探测只查原始字节分布，不做编码判定）。
func textLikeBytes(chunk []byte) bool {
	if len(chunk) == 0 {
		return true
	}
	controls := 0
	for _, b := range chunk {
		if b == 0 {
			return false
		}
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != '\f' {
			controls++
		}
		if b == 0x7F {
			controls++
		}
	}
	return float64(controls)/float64(len(chunk)) <= 0.005
}
