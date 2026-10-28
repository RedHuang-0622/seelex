package dto

import "encoding/json"

// EventLogEntry is one append-only execution-fact log row.
type EventLogEntry struct {
	Seq     uint64          `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}
