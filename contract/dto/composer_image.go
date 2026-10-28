package dto

// composer_image.go —— 输入框图片附件（拖拽 / 粘贴）的跨层形状。
//
// 为什么要有这份 DTO：这条链上有三方消费者——渲染层（Bridge 的 JSON 参数与返回值）、
// 媒体域（落会话媒体分区 + 挂进下一次请求随图）、宿主（用系统默认查看器打开原始
// 文件）。形状只在这里定义一次；媒体域和桥面都不许再各写一份"图片附件"结构，
// 否则"什么算一条附件"迟早有两套口径（判据见 AGENTS.md §8）。
//
// 二进制为什么是 []byte 而不是 base64：base64 是**渲染层到桥面**这一段 wire 编码
// （JS 传不了裸字节），在桥面解码后就不再往下传。领域只认字节。

// ComposerImageRequest 是一次「把用户附上的图片挂进会话」的请求。
type ComposerImageRequest struct {
	// SessionID 是收下这张图的会话。图片属于**会话**而不是视图：前端显式给，
	// 后端不读"当前视图会话"（提交路由已经因为一次 TOCTOU 踩过同一个坑）。
	SessionID string `json:"session_id"`
	// Name 是原始文件名（如 screenshot-2026.png）：只用于展示与落盘名。它不是
	// 路径——落盘前由存储侧净化，重名/保留字/分隔符都不会逃出媒体分区。
	Name string `json:"name"`
	// MimeType 必须是 image/*；非图片在这里就被判死，不做"尽量塞给模型"的降级
	// （文档通道在端点上不存在，见 backend/tool/io/attachment/README.md）。
	MimeType string `json:"mime_type"`
	// Width/Height 是像素尺寸；0 = 未知（不阻塞落盘，只少一条元数据）。
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// Data 是图片原文；空 = 拒绝。
	Data []byte `json:"-"`
}

// ComposerImage 是一条**已落盘**的会话图片，也是前端手里的凭据：
//   - Ref 用于去重与"点开原始文件"（前端把 ref 交回后端，文件位置不回渲染层）；
//   - Name/Bytes/尺寸用于缩略图下方的说明文字。
type ComposerImage struct {
	Ref      string `json:"ref"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Bytes    int    `json:"bytes"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

// ComposerImageData 是一张已落盘会话图片的**字节载荷**（渲染层缩略图用）。
//
// 为什么要有这一格：对话区把用户消息随行的图片画出来时，手上只有 ref——
// 原图在会话媒体分区里，渲染层拿不到路径（路径不回渲染层，见 MediaFilePath）。
// 于是缩略图走这条只读通道：按 (session, ref) 读回字节，编码成 base64 由渲染层
// 拼成 data URL。它是**读**，不是新的存储通道：落盘、配额、归属仍只有
// AttachUserImage 那一份实现。
type ComposerImageData struct {
	Ref      string `json:"ref"`
	MimeType string `json:"mime_type"`
	Bytes    int    `json:"bytes"`
	// DataBase64 是裸 base64（不含 `data:` 前缀）：前缀由渲染层按 MimeType 拼，
	// 后端不猜它要在哪个宿主里显示。
	DataBase64 string `json:"data_base64"`
}
