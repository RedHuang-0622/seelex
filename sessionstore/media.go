// media 会话媒体分区（my_design §10 的落地实现）
//
// 布局（会话粒度，subagent 复用主会话目录，与 big_tool_result 同一套引用/GC 语义）：
//
//	<sessionRoot>/meta/<sha256>/<原名>      二进制原文，永不截断
//	<sessionRoot>/meta/<sha256>/meta.json   单条索引（MediaRef）
//	<sessionRoot>/metadata/media.json       会话级索引 head（可重建派生，白名单内）
//
// 目录名用内容 sha256，文件名保留写入方给的原名：既能按内容去重，又能人眼定位
// 与就地替换。引用形如 `media:<sha256>`，与 `blob:<hash>`、`compressed:<seg>` 并列。
//
// 阈值分轴（与文本大结果刻意不同，见 mediaLimits）：
//
//   - 文本大结果：字符软限 + 字节硬限，超出可截断正文并保留 result_ref 全文；
//   - 媒体：只按「字节 + 像素长边」卡硬限，**没有软限、永不截断**。截断一张
//     PNG/JPEG 得到的是坏文件而不是「更短的图」，所以只能整体接受、调用方先
//     降采样再重试、或显式报错。
//   - 配额独立计账：媒体不占 big_tool_result 的会话配额，长截屏循环不会把文本
//     大结果挤爆导致 read_tool_result 全线失败。
package sessionstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MediaRefPrefix 是媒体引用的前缀：`media:<sha256>`。
const MediaRefPrefix = "media:"

var (
	// ErrMediaInvalid 表示写入项缺少必要字段（名称/类型/字节）。
	ErrMediaInvalid = errors.New("session storage: media item requires name, mime type and bytes")
	// ErrMediaTooLarge 表示单件超过 media.max_item_bytes（不截断，直接拒绝）。
	ErrMediaTooLarge = errors.New("session storage: media item exceeds max item bytes")
	// ErrMediaDimensions 表示像素长边超过 media.max_long_side（调用方应先降采样）。
	ErrMediaDimensions = errors.New("session storage: media long side exceeds max long side")
	// ErrMediaQuota 表示会话媒体配额不足。
	ErrMediaQuota = errors.New("session storage: media session quota exceeded")
	// ErrMediaItemLimit 表示会话媒体条目数已达上限。
	ErrMediaItemLimit = errors.New("session storage: media item count limit reached")
	// ErrMediaRefInvalid 表示 ref 不是合法的 `media:<hash>`。
	ErrMediaRefInvalid = errors.New("session storage: media ref invalid")
)

// MediaItem 是一次媒体写入请求：二进制原文 + 展示与溯源元数据。
// Data 按原样落盘，调用方不要预先 base64（那只会浪费 33% 磁盘与配额）。
type MediaItem struct {
	// Name 是原始文件名（可带目录，落盘只取 basename）。
	Name string
	// MimeType 是内容类型，例如 image/png。
	MimeType string
	// Kind 是来源标记，例如 screenshot；仅用于溯源展示。
	Kind string
	// Data 是二进制原文。
	Data []byte
	// Width/Height 是像素尺寸（0 表示未知，此时跳过长边校验）。
	Width  int
	Height int
	// Scale 是落盘前相对原尺寸的缩放比例（1 或 0 表示未缩放）。
	Scale float64
}

// MediaRef 是一条已落盘媒体资产的引用与元数据。
type MediaRef struct {
	Ref       string    `json:"ref"`
	Hash      string    `json:"hash"`
	Digest    string    `json:"digest"`
	Name      string    `json:"name"`
	Aliases   []string  `json:"aliases,omitempty"`
	MimeType  string    `json:"mime_type"`
	Kind      string    `json:"kind,omitempty"`
	Bytes     int       `json:"bytes"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	Scale     float64   `json:"scale,omitempty"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
}

// mediaHead 是 metadata/media.json 的 payload（会话级媒体索引，可重建）。
type mediaHead struct {
	SessionID  string     `json:"session_id"`
	Items      []MediaRef `json:"items"`
	TotalBytes int        `json:"total_bytes"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// MediaStore 是媒体分区的可选能力接口。
//
// 刻意不进 Repository 主契约：媒体是旁路资产，不是所有后端/测试替身都必须
// 具备的能力；调用方用 MediaStoreOf 显式取用并自行判空。
type MediaStore interface {
	WriteMedia(context.Context, Key, MediaItem) (MediaRef, error)
	ReadMedia(context.Context, Key, string) (MediaRef, []byte, error)
	ListMedia(context.Context, Key) ([]MediaRef, error)
	CollectMedia(context.Context, Key, map[string]bool, bool) ([]string, error)
}

// MediaStoreOf 从 Repository 取媒体分区能力；后端不支持时返回 false。
func MediaStoreOf(repository Repository) (MediaStore, bool) {
	store, ok := repository.(MediaStore)
	return store, ok
}

// MediaRefHash 归一化 ref 为内容 hash；非 `media:` 引用返回空串。
func MediaRefHash(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if !strings.HasPrefix(trimmed, MediaRefPrefix) {
		return ""
	}
	return strings.TrimPrefix(trimmed, MediaRefPrefix)
}

// ReferencedMediaHashes 从工具结果里收集被引用的媒体 hash（GC 的 referenced 集）。
func ReferencedMediaHashes(results []ToolResult) map[string]bool {
	referenced := map[string]bool{}
	for _, result := range results {
		for _, item := range result.Multimodal {
			if hash := MediaRefHash(item.Ref); hash != "" {
				referenced[hash] = true
			}
		}
	}
	return referenced
}

// WriteMedia 落盘一条媒体资产并返回引用；内容相同则复用已有文件（幂等）。
func (repository *jsonRepository) WriteMedia(_ context.Context, key Key, item MediaItem) (MediaRef, error) {
	if err := key.validate(); err != nil {
		return MediaRef{}, err
	}
	lock := repository.layout.mu(key, moduleMedia)
	lock.Lock()
	defer lock.Unlock()
	return repository.layout.writeMediaLocked(key, item)
}

// ReadMedia 读取媒体元数据与二进制原文。
func (repository *jsonRepository) ReadMedia(_ context.Context, key Key, ref string) (MediaRef, []byte, error) {
	if err := key.validate(); err != nil {
		return MediaRef{}, nil, err
	}
	lock := repository.layout.mu(key, moduleMedia)
	lock.Lock()
	defer lock.Unlock()
	return repository.layout.readMediaLocked(key, ref)
}

// ListMedia 列出会话内全部媒体资产（按创建时间、hash 稳定排序）。
func (repository *jsonRepository) ListMedia(_ context.Context, key Key) ([]MediaRef, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	lock := repository.layout.mu(key, moduleMedia)
	lock.Lock()
	defer lock.Unlock()
	return repository.layout.listMediaItems(key)
}

// CollectMedia 删除无引用的媒体目录（referenced 来自 ReferencedMediaHashes）；
// dryRun=true 只输出候选不删除。
func (repository *jsonRepository) CollectMedia(_ context.Context, key Key, referenced map[string]bool, dryRun bool) ([]string, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	lock := repository.layout.mu(key, moduleMedia)
	lock.Lock()
	defer lock.Unlock()
	removed, err := repository.layout.mediaGarbageCollectLocked(key, referenced, dryRun)
	if err != nil {
		return nil, err
	}
	if !dryRun && len(removed) > 0 {
		_ = repository.layout.publishMediaIndexLocked(key)
	}
	return removed, nil
}

// mediaLimits 取 §11 覆盖链解析后的媒体限额。
func (store *storeEngine) mediaLimits() (maxItemBytes, quotaBytes, maxItems, maxLongSide int) {
	settings := store.settings
	return settings.MediaMaxItemBytes, settings.MediaSessionQuotaBytes,
		settings.MediaMaxItemsPerSession, settings.MediaMaxLongSide
}

// mediaRoot 是会话媒体分区根目录（与 big_tool_result 平级、配额独立）。
func (store *storeEngine) mediaRoot(key Key) string {
	return filepath.Join(store.sessionRoot(key), "meta")
}

// mediaItemDir 是单条媒体的内容寻址目录。
func (store *storeEngine) mediaItemDir(key Key, digest string) string {
	return filepath.Join(store.mediaRoot(key), digest)
}

// mediaItemMetaPath 是单条媒体的索引文件。
func (store *storeEngine) mediaItemMetaPath(key Key, digest string) string {
	return filepath.Join(store.mediaItemDir(key, digest), "meta.json")
}

// mediaHash 是媒体内容寻址摘要：完整 sha256 十六进制（目录名兼作去重键）。
func mediaHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sanitizeMediaName 保留原文件名，仅剥离目录与文件系统非法字符；
// 结果为空或撞上 Windows 保留设备名时回退到 <hash>.bin / 前置下划线。
func sanitizeMediaName(name, digest string) string {
	base := strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case 0, '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, base)
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" || cleaned == "." {
		return digest + ".bin"
	}
	if len(cleaned) > 120 {
		ext := filepath.Ext(cleaned)
		stem := strings.TrimSuffix(cleaned, ext)
		if len(stem) > 120-len(ext) {
			stem = stem[:120-len(ext)]
		}
		cleaned = stem + ext
	}
	if isReservedWindowsName(cleaned) {
		return "_" + cleaned
	}
	return cleaned
}

// isReservedWindowsName 判断 Windows 保留设备名（CON/PRN/AUX/NUL/COM1..9/LPT1..9）。
func isReservedWindowsName(name string) bool {
	stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(stem) == 4 {
		prefix, digit := stem[:3], stem[3]
		if digit >= '1' && digit <= '9' && (prefix == "COM" || prefix == "LPT") {
			return true
		}
	}
	return false
}

// writeMediaLocked 在已持锁的前提下落盘媒体（调用方负责加锁）。
func (store *storeEngine) writeMediaLocked(key Key, item MediaItem) (MediaRef, error) {
	if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.MimeType) == "" || len(item.Data) == 0 {
		return MediaRef{}, ErrMediaInvalid
	}
	maxItemBytes, quotaBytes, maxItems, maxLongSide := store.mediaLimits()
	if maxItemBytes > 0 && len(item.Data) > maxItemBytes {
		return MediaRef{}, fmt.Errorf("%w: %d > %d", ErrMediaTooLarge, len(item.Data), maxItemBytes)
	}
	if maxLongSide > 0 && item.Width > 0 && item.Height > 0 {
		if longest := max(item.Width, item.Height); longest > maxLongSide {
			return MediaRef{}, fmt.Errorf("%w: %dx%d > %d", ErrMediaDimensions, item.Width, item.Height, maxLongSide)
		}
	}
	digest := mediaHash(item.Data)

	// 内容寻址：同一份字节只落一份；换名写入记为别名，不重复占用配额。
	if existing, err := store.readMediaMetaFile(key, digest); err == nil {
		if name := sanitizeMediaName(item.Name, digest); name != existing.Name && !containsString(existing.Aliases, name) {
			existing.Aliases = append(existing.Aliases, name)
			if err := store.writeMediaMetaFile(key, existing); err != nil {
				return MediaRef{}, err
			}
			if err := store.publishMediaIndexLocked(key); err != nil {
				return MediaRef{}, err
			}
		}
		return existing, nil
	}

	items, err := store.listMediaItems(key)
	if err != nil {
		return MediaRef{}, err
	}
	if maxItems > 0 && len(items) >= maxItems {
		return MediaRef{}, fmt.Errorf("%w: %d >= %d", ErrMediaItemLimit, len(items), maxItems)
	}
	usage, err := store.mediaUsageBytes(key)
	if err != nil {
		return MediaRef{}, err
	}
	if quotaBytes > 0 && usage+len(item.Data) > quotaBytes {
		return MediaRef{}, fmt.Errorf("%w: %d + %d > %d", ErrMediaQuota, usage, len(item.Data), quotaBytes)
	}

	ref := MediaRef{
		Ref:       MediaRefPrefix + digest,
		Hash:      digest,
		Digest:    "sha256:" + digest,
		Name:      sanitizeMediaName(item.Name, digest),
		MimeType:  item.MimeType,
		Kind:      item.Kind,
		Bytes:     len(item.Data),
		Width:     item.Width,
		Height:    item.Height,
		Scale:     item.Scale,
		SessionID: key.SessionID,
		CreatedAt: time.Now().UTC(),
	}
	dir := store.mediaItemDir(key, digest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return MediaRef{}, err
	}
	if err := writeAtomic(filepath.Join(dir, ref.Name), item.Data, 0o600); err != nil {
		return MediaRef{}, err
	}
	if err := store.writeMediaMetaFile(key, ref); err != nil {
		_ = os.RemoveAll(dir)
		return MediaRef{}, err
	}
	if err := store.publishMediaIndexLocked(key); err != nil {
		_ = os.RemoveAll(dir)
		return MediaRef{}, err
	}
	return ref, nil
}

// readMediaLocked 在已持锁的前提下读取媒体元数据与原文。
func (store *storeEngine) readMediaLocked(key Key, ref string) (MediaRef, []byte, error) {
	digest := MediaRefHash(ref)
	if digest == "" {
		return MediaRef{}, nil, fmt.Errorf("%w: %q", ErrMediaRefInvalid, ref)
	}
	item, err := store.readMediaMetaFile(key, digest)
	if err != nil {
		return MediaRef{}, nil, err
	}
	data, err := os.ReadFile(filepath.Join(store.mediaItemDir(key, digest), item.Name))
	if err != nil {
		return MediaRef{}, nil, err
	}
	if len(data) != item.Bytes {
		return MediaRef{}, nil, fmt.Errorf("session storage: media %s size mismatch: %d != %d", item.Name, len(data), item.Bytes)
	}
	return item, data, nil
}

// readMediaMetaFile 读单条媒体索引；缺失视为「未落盘」。
func (store *storeEngine) readMediaMetaFile(key Key, digest string) (MediaRef, error) {
	data, err := os.ReadFile(store.mediaItemMetaPath(key, digest))
	if err != nil {
		return MediaRef{}, err
	}
	var item MediaRef
	if err := json.Unmarshal(data, &item); err != nil {
		return MediaRef{}, err
	}
	if item.Hash != digest || item.Name == "" {
		return MediaRef{}, fmt.Errorf("session storage: media index corrupt for %s", digest)
	}
	return item, nil
}

// writeMediaMetaFile 写单条媒体索引（原子替换）。
func (store *storeEngine) writeMediaMetaFile(key Key, item MediaRef) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(store.mediaItemMetaPath(key, item.Hash), data, 0o600)
}

// listMediaItems 枚举会话媒体（目录为事实来源，索引缺失条目跳过而非报错）。
func (store *storeEngine) listMediaItems(key Key) ([]MediaRef, error) {
	entries, err := os.ReadDir(store.mediaRoot(key))
	if errors.Is(err, fs.ErrNotExist) {
		return []MediaRef{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]MediaRef, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		item, err := store.readMediaMetaFile(key, entry.Name())
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].Hash < items[j].Hash
	})
	return items, nil
}

// mediaUsageBytes 统计媒体分区实际占用（配额按真实字节计）。
// 只统计二进制载荷：每条索引 meta.json 是可重建派生元数据，不计入配额，
// 否则小图会被索引开销放大成倍成本、配额换算也不再等于“多少张图”。
func (store *storeEngine) mediaUsageBytes(key Key) (int, error) {
	total := 0
	root := store.mediaRoot(key)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "meta.json" {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += int(info.Size())
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	return total, nil
}

// publishMediaIndexLocked 发布 metadata/media.json（会话级媒体索引 head，可重建）。
func (store *storeEngine) publishMediaIndexLocked(key Key) error {
	items, err := store.listMediaItems(key)
	if err != nil {
		return err
	}
	usage, err := store.mediaUsageBytes(key)
	if err != nil {
		return err
	}
	head := mediaHead{
		SessionID:  key.SessionID,
		Items:      items,
		TotalBytes: usage,
		UpdatedAt:  time.Now().UTC(),
	}
	_, err = store.publishModuleHead(key, moduleMedia, modulePayloadCommitID(moduleMedia, head), head, head.UpdatedAt)
	return err
}

// mediaGarbageCollectLocked 删除无引用的媒体目录，返回被删除的 hash 列表。
func (store *storeEngine) mediaGarbageCollectLocked(key Key, referenced map[string]bool, dryRun bool) ([]string, error) {
	items, err := store.listMediaItems(key)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, item := range items {
		if referenced[item.Hash] {
			continue
		}
		removed = append(removed, item.Hash)
		if !dryRun {
			if err := os.RemoveAll(store.mediaItemDir(key, item.Hash)); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

// containsString 判断切片是否已含目标值。
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
