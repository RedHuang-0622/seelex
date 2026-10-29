package prefixprobe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// KeyInput 是构造 prefix_key 的原始素材（arch 文档 §4 的六格）。
//
// 每个字段都必须是**世界状态**的函数，不能掺入"本轮"的任何东西（query、时间、
// cwd、打开的文件）——否则键会随轮变化，前缀永远失效。这条纪律有外部佐证：
// Aider 的 repo-map 缓存曾把"本轮提到的文件名"放进 cache key，导致每次请求全量
// 重扫（PR #5556 修掉）。
type KeyInput struct {
	SystemPrompt   string   // 完整 system prompt 文本（会取 digest）
	ProjectVersion string   // 项目内容 hash（storeproject.Version）
	MemoryEpoch    uint64   // = compact_seq；绝不含 query
	CompactSeq     uint64   // 压缩栈顶序号
	ToolsEpoch     uint64   // 工具集纪元（catalog 变化才 bump）
	ToolSignatures []string // 工具签名（名 + 描述/schema），顺序无关，内部排序
	RetainedFrom   int      // transcript 绝对下标（left pointer）
}

// BuildKey 把素材压成可比较的前缀键。
//
// 工具签名的顺序被内部排序后取摘要：发布顺序不确定（map 迭代）时键仍稳定，
// 这也顺手把"工具定义顺序抖动"从键的语义里去掉（顺序抖动应进 tools_digest 的
// 稳定性修复，而不是让键跟着抖）。
func BuildKey(in KeyInput) Key {
	signatures := append([]string(nil), in.ToolSignatures...)
	sort.Strings(signatures)
	toolsJoined := ""
	for _, signature := range signatures {
		toolsJoined += signature + "\x00"
	}
	return Key{
		SystemDigest:   Digest(in.SystemPrompt),
		ProjectVersion: in.ProjectVersion,
		MemoryEpoch:    in.MemoryEpoch,
		CompactSeq:     in.CompactSeq,
		ToolsEpoch:     in.ToolsEpoch,
		ToolsDigest:    Digest(toolsJoined),
		RetainedFrom:   in.RetainedFrom,
	}
}

// Digest 返回内容短摘要（sha256 前 16 个十六进制字符）。
//
// 空串返回空串（而不是固定 hash）：让"未提供"与"提供了空内容"在键上可区分。
func Digest(content string) string {
	if content == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

// ToolSignature 生成一条工具的稳定签名（名 + 描述），供 BuildKey 使用。
//
// 只取名字与描述：参数 schema 的序列化格式若随版本变化会让键无故失效，而描述
// 已足够覆盖"这个工具对模型意味着什么"。若将来需要包含 schema，请同时钉住其
// 序列化顺序，否则会把抖动引回键里。
func ToolSignature(name, description string) string {
	return fmt.Sprintf("%s|%s", name, description)
}
