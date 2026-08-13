package seelebridge

import (
	"testing"
)

const planLoadSmokeInput = `{
  "entry": "search",
  "nodes": {
    "search": {"input": "find files"},
    "summarize": {"input": "summarize the file list"}
  },
  "edges": {
    "search": ["summarize"]
  }
}`
const planLoadAdapterInput = `{
  "entry": "inspect",
  "nodes": [
    {"id": "inspect", "input": "inspect module boundaries"},
    {"key": "verify", "input": "verify with tests"},
    {"id": "report", "input": "write a report"}
  ],
  "edges": [
    {"from": "inspect", "to": "verify"},
    {"source": "verify", "target": "report"}
  ]
}`

// TestRuntimeShutdownIdempotentAndRegistered 验证生命周期链：NewRuntime 已登记
// 资源持有者（逆序关停），重复 Shutdown 幂等不 panic。
func TestRuntimeShutdownIdempotentAndRegistered(t *testing.T) {
	runtime := newTestRuntime(t)
	if len(runtime.lifecycle) == 0 {
		t.Fatal("lifecycle must be registered by NewRuntime")
	}
	runtime.Shutdown()
	runtime.Shutdown()
}
