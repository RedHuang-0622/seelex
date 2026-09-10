// R3 会话中间缓存区的接口与失效键（my_design §5.7 / 提示词 R3）。
//
// 本文件先提供进程内物化缓存（不落盘）：缓存内容 = 角色 wire 的物化形态；
// 失效键 = message head commit/revision + compact applied_seq/frame_id + floor。
// 应用层接线（AssembleWire 命中/回写、attempt cache 合并）留到后续阶段。
package sessionstore

import (
	"container/list"
	"sync"
	"time"
)

// MaterialCacheKey 是一次角色 wire 物化结果的失效键。任何字段变化都会使旧
// 缓存不命中；LastCommitID 对应 message head 最近一次已发布提交。
type MaterialCacheKey struct {
	RoleSessionID string `json:"role_session_id"`
	RoleName      string `json:"role_name"`
	AppliedSeq    uint64 `json:"applied_seq"`
	FrameID       string `json:"frame_id,omitempty"`
	LastCommitID  string `json:"last_commit_id,omitempty"`
	FloorSeq      uint64 `json:"floor_seq,omitempty"`
}

// MaterialCacheValue 是物化角色 wire 的缓存结果。Rows 是已发布的 message 行
// 派生出的 wire 形态（含 compact 摘要/尾段；应用层装配后填写）。
type MaterialCacheValue struct {
	Rows         []Event   `json:"rows"`
	NeedCompact  bool      `json:"need_compact"`
	PrefixDigest string    `json:"prefix_digest,omitempty"`
	StoredAt     time.Time `json:"stored_at"`
}

type materialCacheEntry struct {
	key   MaterialCacheKey
	value MaterialCacheValue
}

// MaterialCache 是进程内 LRU 缓存。key 用 json.Marshal 稳定序列化。
type MaterialCache struct {
	mu      sync.Mutex
	max     int
	order   *list.List
	entries map[string]*list.Element
}

// NewMaterialCache 构造缓存；maxEntries <= 0 时取 256。
func NewMaterialCache(maxEntries int) *MaterialCache {
	if maxEntries <= 0 {
		maxEntries = 256
	}
	return &MaterialCache{
		max: maxEntries, order: list.New(), entries: make(map[string]*list.Element),
	}
}

func (cache *MaterialCache) keyString(key MaterialCacheKey) string {
	// 包内类型本身没有歧义字段，稳定拼出即可。
	return key.RoleSessionID + "\x00" + key.RoleName + "\x00" +
		itoa(key.AppliedSeq) + "\x00" + key.FrameID + "\x00" + key.LastCommitID +
		"\x00" + itoa(key.FloorSeq)
}

// Get 命中返回缓存值；未命中或已失效返回 false。
func (cache *MaterialCache) Get(key MaterialCacheKey) (MaterialCacheValue, bool) {
	if cache == nil {
		return MaterialCacheValue{}, false
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	element, ok := cache.entries[cache.keyString(key)]
	if !ok {
		return MaterialCacheValue{}, false
	}
	cache.order.MoveToFront(element)
	entry := element.Value.(materialCacheEntry)
	value := entry.value
	value.Rows = append([]Event(nil), value.Rows...)
	return value, true
}

// Put 写入/更新缓存并按 LRU 淘汰最旧条目。
func (cache *MaterialCache) Put(key MaterialCacheKey, value MaterialCacheValue) {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	value.StoredAt = time.Now().UTC()
	value.Rows = append([]Event(nil), value.Rows...)
	keyString := cache.keyString(key)
	if element, ok := cache.entries[keyString]; ok {
		element.Value = materialCacheEntry{key: key, value: value}
		cache.order.MoveToFront(element)
		return
	}
	element := cache.order.PushFront(materialCacheEntry{key: key, value: value})
	cache.entries[keyString] = element
	for cache.order.Len() > cache.max {
		oldest := cache.order.Back()
		if oldest == nil {
			break
		}
		cache.order.Remove(oldest)
		delete(cache.entries, cache.keyString(oldest.Value.(materialCacheEntry).key))
	}
}

// InvalidateRole 使某个角色会话的缓存失效（message head/compact/floor 变化后
// 调用）。应用层尚未接线时，测试/巡检可用。
func (cache *MaterialCache) InvalidateRole(roleSessionID, roleName string) {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for key, element := range cache.entries {
		entry := element.Value.(materialCacheEntry)
		if entry.key.RoleSessionID == roleSessionID && entry.key.RoleName == roleName {
			cache.order.Remove(element)
			delete(cache.entries, key)
		}
	}
}

// InvalidateAll 清空缓存。
func (cache *MaterialCache) InvalidateAll() {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	cache.order.Init()
	cache.entries = make(map[string]*list.Element)
	cache.mu.Unlock()
}

func itoa(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
