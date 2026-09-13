// Package imageattach 把「会话里刚看到的画面」送到模型眼前。
//
// 为什么落在这一层：引擎（Seele）按自己的历史构造消息，它并不知道 seelex 的
// 会话媒体分区里什么时候多了一张图；而 Completer/StreamCompleter 是引擎与
// provider 之间唯一稳定的窄接口。在这一层补一条带图的 user 消息，模型就能看到
// 画面，同时不改动引擎的历史语义。
//
// 语义是「至多送一次」：工具侧 Add，请求侧 Take；Take 取出即清空，所以同一张
// 图不会在后续每一轮里重复占用 token 与配额。
package imageattach

import (
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

// Attachment 是一次「待随图」。
type Attachment struct {
	// Ref 指向会话媒体分区（sessionstore 的 media:<hash>），用于溯源与界面展示。
	Ref string
	// Label 是给人看的来源说明，例如 `screenshot 1280x720`。
	Label string
	// Image 是图片本体的投影；Ref 非空而这里为空时，由 Loader 按需加载。
	Image types.ImagePart
	// CreatedAt 用于诊断：队列长时间积压时能看出是哪一轮留下的。
	CreatedAt time.Time
}

// Registry 是会话级的待随图队列。零值不可用，用 NewRegistry 构造。
type Registry struct {
	mu      sync.Mutex
	pending map[string][]Attachment
}

// NewRegistry 构造一个空队列。
func NewRegistry() *Registry {
	return &Registry{pending: map[string][]Attachment{}}
}

// Add 把一张待随图挂到指定会话。
//
// sessionID 为空时直接丢弃：队列是「按会话」的，宁可不送，也不能串会话把别人的
// 画面发给错误的对话。
func (r *Registry) Add(sessionID string, attachment Attachment) {
	if r == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if attachment.CreatedAt.IsZero() {
		attachment.CreatedAt = time.Now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = map[string][]Attachment{}
	}
	r.pending[sessionID] = append(r.pending[sessionID], attachment)
}

// Take 取出并清空该会话的待随图；没有待随图时返回 nil。
func (r *Registry) Take(sessionID string) []Attachment {
	if r == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	attachments := r.pending[sessionID]
	if len(attachments) == 0 {
		return nil
	}
	delete(r.pending, sessionID)
	return attachments
}

// Len 返回该会话待随图数量（诊断与断言用）。
func (r *Registry) Len(sessionID string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending[strings.TrimSpace(sessionID)])
}
