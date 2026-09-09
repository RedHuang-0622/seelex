// v8 操作尝试缓存（运行期、非持久）。
//
// 事实模型（my_design §4/§5.3.3）：进程内内存，不进 metadata、不落盘、
// 重启即清空；wire 只装配同一操作最近 K 条尝试（K = wire_recent_errors，
// 默认 3）；max_items/max_chars 先到先淘汰最旧。
package sessionstore

import (
	"sort"
	"sync"
	"time"
)

// attempt 是同一操作的一次尝试说明。
type attempt struct {
	// AnchorSeq 是尝试挂靠的事件行（该操作发生位置的 message seq）。
	AnchorSeq    uint64    `json:"anchor_seq"`
	OperationKey string    `json:"operation_key"`
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// attemptCache 是会话运行期的非持久尝试缓存（进程内单例，可跨会话共享
// 实例；条目按 anchor + operation_key 归组）。
type attemptCache struct {
	mu       sync.Mutex
	items    []attempt
	maxItems int
	maxChars int
}

// NewAttemptCache 构造尝试缓存；maxItems/maxChars <= 0 时取设计默认值
// （64 条 / 8 MB）。
func NewAttemptCache(maxItems, maxChars int) *attemptCache {
	if maxItems <= 0 {
		maxItems = 64
	}
	if maxChars <= 0 {
		maxChars = 8 << 20
	}
	return &attemptCache{maxItems: maxItems, maxChars: maxChars}
}

// Add 追加一次尝试；超出 max_items/max_chars 时淘汰最旧条目。
func (cache *attemptCache) Add(anchorSeq uint64, operationKey, role, content, status string) {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if operationKey == "" {
		operationKey = "default"
	}
	cache.items = append(cache.items, attempt{
		AnchorSeq:    anchorSeq,
		OperationKey: operationKey,
		Role:         role,
		Content:      content,
		Status:       status,
		CreatedAt:    time.Now(),
	})
	for len(cache.items) > cache.maxItems {
		cache.items = cache.items[1:]
	}
	chars := 0
	for _, item := range cache.items {
		chars += len(item.Content) + len(item.Role) + len(item.Status) + len(item.OperationKey)
	}
	for chars > cache.maxChars && len(cache.items) > 0 {
		chars -= len(cache.items[0].Content) + len(cache.items[0].Role) + len(cache.items[0].Status) + len(cache.items[0].OperationKey)
		cache.items = cache.items[1:]
	}
}

// RecentFor 返回指定锚点 + 操作的最近 K 条尝试（时间升序）。
func (cache *attemptCache) RecentFor(anchorSeq uint64, operationKey string, k int) []attempt {
	if cache == nil {
		return nil
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	var matched []attempt
	for _, item := range cache.items {
		if item.AnchorSeq == anchorSeq && item.OperationKey == operationKey {
			matched = append(matched, item)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CreatedAt.Before(matched[j].CreatedAt) })
	if k <= 0 || len(matched) <= k {
		return matched
	}
	return matched[len(matched)-k:]
}

// Clear 清空缓存（重启/测试用）。
func (cache *attemptCache) Clear() {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	cache.items = nil
	cache.mu.Unlock()
}
