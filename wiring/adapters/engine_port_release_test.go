package adapters

import "testing"

// TestEnginePortReleaseSessionBundleForwardsToBridge：端口只把释放面转给桥
// （seelebridge.Runtime.ReleaseSessionBundle 注入的闭包）；未装配时返回 false
// ——legacy 引擎的语义是"释放面退化成只丢引擎引用"，调用方据 false 保持驻留。
func TestEnginePortReleaseSessionBundleForwardsToBridge(t *testing.T) {
	port := NewEnginePort(nil, nil, nil)
	var released []string
	port.ApplyDeps(EnginePortDeps{
		ReleaseSessionBundle: func(sessionID string) bool {
			released = append(released, sessionID)
			return sessionID == "sess-a"
		},
	})
	if !port.ReleaseSessionBundle("sess-a") {
		t.Fatal("装配后应转发并把桥的结论原样回报")
	}
	if port.ReleaseSessionBundle("sess-b") {
		t.Fatal("桥拒绝（在飞/活跃别名）时应返回 false，调用方保持驻留")
	}
	if len(released) != 2 || released[0] != "sess-a" || released[1] != "sess-b" {
		t.Fatalf("转发记录 = %#v", released)
	}
	if port.ReleaseSessionBundle("   ") {
		t.Fatal("空会话 ID 不得转发")
	}

	bare := NewEnginePort(nil, nil, nil)
	if bare.ReleaseSessionBundle("sess-a") {
		t.Fatal("未装配释放面时必须返回 false")
	}
}

// TestUnloadSessionClearsHandoffLedger：卸载后端口对该会话不留任何状态——
// handoffLatest（历史交接序号）随 engines / engineCalls / sessionMaxLoops 一起
// 离场，别的会话的登记不得被误删。
func TestUnloadSessionClearsHandoffLedger(t *testing.T) {
	port := NewEnginePort(nil, nil, nil)
	port.handoffLatest = map[string]uint64{"sess-a": 7, "sess-b": 1}

	if err := port.UnloadSession("sess-a"); err != nil {
		t.Fatalf("unload sess-a: %v", err)
	}
	if _, ok := port.handoffLatest["sess-a"]; ok {
		t.Fatal("handoffLatest 未随卸载离场（端口对已卸载会话留了状态）")
	}
	if _, ok := port.handoffLatest["sess-b"]; !ok {
		t.Fatal("别的会话的交接登记被误删")
	}
}
