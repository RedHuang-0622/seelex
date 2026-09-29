// Package bootseed 负责**启动期默认数据落盘**：按责任链（候选链）找到应用要读的
// 配置或资源，命中就以它为准（存在即读，绝不改写用户已有的东西）；候选链全都不
// 存在，才用内嵌二进制里的默认数据初始化，再按同一个入口读回来。
//
// 为什么要有这个包（现场）：包内配置/脚本缺失时，应用此前只会**静默降级**——
// 读不到 limits 就悄悄走代码默认值、找不到白名单脚本就跳过登记。现场看起来是
// "开关明明打开了却不生效"（`docs/devlog/2026-09-29-dev-package-config-drift.md`）
// 或"新建定时任务的命令下拉是空的、命令类任务根本发布不了"，排查成本极高。
//
// 口径三条：
//
//  1. **责任链**：候选按优先级排，第一个"存在"的胜出；链路本身就是"文件放哪"
//     的规范，落盘也一样按链路决定位置。
//  2. **存在即读**：命中候选时本包不写任何字节；Materialize 也逐文件跳过已存在
//     的目标——用户的配置永远比默认数据优先。
//  3. **默认数据是编码过的**：默认数据以内嵌资源形式随二进制走（`EncodingRaw`
//     逐字节，适合文本；`EncodingBase64` 适合非 UTF-8/GBK、二进制或换行不定的
//     字节），初始化时按其编码还原成**逐字节相同**的一份写出去。
//
// 本包只做"决定读哪个 + 必要时初始化"，不解析任何配置内容，也不引入业务语义。
package bootseed

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Encoding 是默认数据在内嵌资源里的编码形式。
type Encoding string

const (
	// EncodingRaw 逐字节内嵌：文本类默认数据（可读、可 diff）。
	EncodingRaw Encoding = ""
	// EncodingBase64 编码内嵌：非 UTF-8（如 GBK）、二进制或字节敏感的数据。
	EncodingBase64 Encoding = "base64"
)

// File 是默认数据包里的一个文件。
type File struct {
	Rel      string      // 相对落盘根的目标路径（用 / 分隔，落盘时按平台转换）
	Source   string      // 内嵌资源路径（相对 Assets() 根）
	Encoding Encoding    // 编码形式（零值 = 逐字节）
	Mode     os.FileMode // 0 → 0o644（目录一律 0o755）
}

// Pack 是一份默认数据包：资源全都不存在时用来初始化的那份。
type Pack struct {
	Name  string // 包名（日志用）
	Files []File
}

// Spec 是一条资源的责任链。
type Spec struct {
	Name       string   // 日志名（如 "config/seelex.yaml"）
	Candidates []string // 存在判据：按优先级排列的候选**文件**路径（相对路径按 CWD 解析）
	SeedRoot   string   // 候选链全缺时允许初始化的目录；"" = 不落盘，只报告缺失
	Entry      string   // 落盘后的入口（相对 SeedRoot，用 / 分隔）；"" = SeedRoot 本身
	Pack       Pack     // 默认数据（目标路径相对 SeedRoot）
}

// Kind 是责任链的结论类型。
type Kind string

const (
	// KindHit 候选链上命中：存在即读，本次未写任何字节。
	KindHit Kind = "hit"
	// KindSeeded 候选链全缺：已按默认数据初始化（入口可能仍缺，见 Path）。
	KindSeeded Kind = "seeded"
	// KindMissing 候选链全缺且无处落盘：调用方应回退代码默认值。
	KindMissing Kind = "missing"
)

// Result 是责任链的结论。
type Result struct {
	Kind     Kind
	Path     string   // 该读哪个路径（KindHit = 命中的候选；KindSeeded = 落盘后的入口，可能为空）
	Root     string   // 命中候选所在目录 / 落盘根
	Written  []string // 本次写出的文件（绝对路径，供日志）
	Existing []string // 已存在而跳过的目标（绝不覆盖）
	Pack     string   // 默认数据包名
}

// Resolve 对**包内默认数据**执行责任链，等价于 ResolveFS(spec, Assets())。
func Resolve(spec Spec) (Result, error) {
	return ResolveFS(spec, Assets())
}

// ResolveFS 在给定资源集上执行责任链：第一个存在的候选胜出；全都不存在 → 在
// SeedRoot 用默认数据初始化，再返回落盘后的入口。SeedRoot 为空时返回
// KindMissing（调用方回退代码默认值，不视为错误）。
//
// 落盘失败（目录不可写等）返回 KindMissing + error：调用方要么回退默认值，要么
// 向上报——本包不替调用方决定启动是否失败。
func ResolveFS(spec Spec, packFS fs.FS) (Result, error) {
	for _, candidate := range spec.Candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
			return Result{Kind: KindHit, Path: abs, Root: filepath.Dir(abs), Pack: spec.Pack.Name}, nil
		}
	}
	if strings.TrimSpace(spec.SeedRoot) == "" {
		return Result{Kind: KindMissing, Pack: spec.Pack.Name}, nil
	}
	written, existing, err := Materialize(spec.SeedRoot, spec.Pack, packFS)
	if err != nil {
		return Result{Kind: KindMissing, Written: written, Existing: existing, Pack: spec.Pack.Name}, err
	}
	root, absErr := filepath.Abs(spec.SeedRoot)
	if absErr != nil {
		return Result{Kind: KindMissing, Written: written, Existing: existing, Pack: spec.Pack.Name}, absErr
	}
	entry := root
	if strings.TrimSpace(spec.Entry) != "" {
		entry = filepath.Join(root, filepath.FromSlash(spec.Entry))
	}
	if info, statErr := os.Stat(entry); statErr != nil || info.IsDir() {
		// 默认数据里没有入口文件（例如脚本要用户自己放）：如实报告入口仍缺，
		// 让调用方按"资源不可用"处理，而不是拿着一个不存在的路径往下走。
		entry = ""
	}
	return Result{Kind: KindSeeded, Path: entry, Root: root, Written: written, Existing: existing, Pack: spec.Pack.Name}, nil
}

// Materialize 把默认数据包落到 root：逐文件写出，**已存在的目标一律跳过**
// （存在即读，用户那份优先）；目录按需创建（0o755），文件默认 0o644。
// 返回本次真正写出的文件与因已存在而跳过的文件。
func Materialize(root string, pack Pack, packFS fs.FS) (written, existing []string, err error) {
	root = filepath.Clean(root)
	for _, file := range pack.Files {
		rel := filepath.FromSlash(strings.TrimSpace(file.Rel))
		if rel == "" || strings.Contains(rel, "..") {
			return written, existing, fmt.Errorf("bootseed: 默认数据包 %q 的目标路径非法: %q", pack.Name, file.Rel)
		}
		target := filepath.Join(root, rel)
		if _, statErr := os.Stat(target); statErr == nil {
			existing = append(existing, target)
			continue
		}
		data, decodeErr := Decode(packFS, file)
		if decodeErr != nil {
			return written, existing, decodeErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
			return written, existing, fmt.Errorf("bootseed: 创建目录 %s: %w", filepath.Dir(target), mkErr)
		}
		mode := file.Mode
		if mode == 0 {
			mode = 0o644
		}
		if writeErr := os.WriteFile(target, data, mode); writeErr != nil {
			return written, existing, fmt.Errorf("bootseed: 写出默认数据 %s: %w", target, writeErr)
		}
		written = append(written, target)
	}
	return written, existing, nil
}

// Decode 按 File.Encoding 还原一份默认数据的原始字节。
func Decode(packFS fs.FS, file File) ([]byte, error) {
	raw, err := fs.ReadFile(packFS, file.Source)
	if err != nil {
		return nil, fmt.Errorf("bootseed: 默认数据 %q: %w", file.Source, err)
	}
	if file.Encoding != EncodingBase64 {
		return raw, nil
	}
	// 编码过的载荷允许折行/缩进：空白先去掉再解码，避免"文件长得好看"与
	// "能不能解"绑在一起。
	compact := strings.Join(strings.Fields(string(raw)), "")
	data, decodeErr := base64.StdEncoding.DecodeString(compact)
	if decodeErr != nil {
		return nil, fmt.Errorf("bootseed: 默认数据 %q base64: %w", file.Source, decodeErr)
	}
	return data, nil
}

// EncodeBase64 把任意字节编码成可放进内嵌资源的形式（与 Decode 对称：换行与缩进
// 会被忽略）。生成默认数据时用它，测试用它钉住编码往返。
func EncodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
