// Package stablehash 提供仓库内共享的稳定哈希原语。
//
// 这里的输出是**持久兼容面**：node 会话号、账号池定序与权限主体名都靠
// “同输入同输出”工作。换算法必须同时迁移这些持久键，不能只改实现。
package stablehash

import "hash/fnv"

// FNV1a32 返回 s 的 FNV-1a 32 位哈希。
func FNV1a32(s string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(s))
	return hash.Sum32()
}

// FNV1a64 返回 s 的 FNV-1a 64 位哈希。它服务 task key 等更长的稳定标识；
// 同样是持久兼容面，换算法必须迁移对应的键。
func FNV1a64(s string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(s))
	return hash.Sum64()
}
