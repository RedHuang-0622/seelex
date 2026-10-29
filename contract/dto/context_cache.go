package dto

// ContextCacheView is the read-only frontend projection of the L1 prefix
// reuse cache. It reports local reuse facts only; it is not provider usage.
type ContextCacheView struct {
	Available       bool                 `json:"available"`
	Status          string               `json:"status,omitempty"`
	Hits            int                  `json:"hits"`
	Misses          int                  `json:"misses"`
	Rebuilt         int                  `json:"rebuilt"`
	ReusedEvents    int                  `json:"reused_events"`
	ReusedChars     int64                `json:"reused_chars"`
	LastReusedChars int64                `json:"last_reused_chars"`
	LastChanged     []string             `json:"last_changed,omitempty"`
	PrefixKey       ContextPrefixKeyView `json:"prefix_key"`
}

// ContextPrefixKeyView is the public attribution projection of prefix_key.
type ContextPrefixKeyView struct {
	SystemDigest   string `json:"system_digest,omitempty"`
	ProjectVersion string `json:"project_version,omitempty"`
	MemoryEpoch    uint64 `json:"memory_epoch"`
	CompactSeq     uint64 `json:"compact_seq"`
	ToolsEpoch     uint64 `json:"tools_epoch"`
	ToolsDigest    string `json:"tools_digest,omitempty"`
	RetainedFrom   int    `json:"retained_from"`
}
