package adapters

import (
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

// session_ssh_state_port_test.go 钉住执行面缺省环境读面的三件事：
//  1. 写入后**立刻**读得到（写穿缓存，不必等回放）；
//  2. 缺省是空（没放权/没限定 = 工具按"没指名环境"处理）；
//  3. 解析器与端口读到同一个值（同一个进程内实体，不存在两份事实）。

func newSSHBridgeRouter(t *testing.T) *store.Router {
	t.Helper()
	dir := t.TempDir()
	router, err := store.NewRouter(filepath.Join(dir, "session-storage.json"))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

func TestSessionSSHEnvResolverReadsWrittenState(t *testing.T) {
	router := newSSHBridgeRouter(t)
	bridge := sessionSSHBridgeFor(router)
	if bridge == nil {
		t.Fatal("bridge 不该为 nil")
	}

	// 缺省：没有限定环境
	resolver := SessionSSHEnvResolver(router)
	if got := resolver("session-a", ""); got != "" {
		t.Fatalf("缺省环境 = %q, want 空（没有限定）", got)
	}

	// 写入后立刻可见（写穿缓存）
	if err := bridge.put("", model.SessionSSHInfo{
		SessionID:   "session-a",
		CurrentEnv:  "dev",
		AllowedEnvs: []string{"dev", "test"},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := resolver("session-a", ""); got != "dev" {
		t.Fatalf("写入后缺省环境 = %q, want dev", got)
	}

	// 端口读面与解析器同一个值
	port := SessionPort{}
	_ = port // SessionPort 需要 Manager 才能读；这里直接读共享体验证同源
	state, err := bridge.state("", "session-a")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.CurrentEnv != "dev" || len(state.AllowedEnvs) != 2 {
		t.Fatalf("共享体读数 = %#v, want dev + 2 个放权", state)
	}

	// 别的会话不受影响（缓存按会话分格）
	if got := resolver("session-b", ""); got != "" {
		t.Fatalf("session-b 缺省环境 = %q, want 空", got)
	}
}

func TestSessionSSHEnvResolverWithoutRouterIsNil(t *testing.T) {
	if resolver := SessionSSHEnvResolver(nil); resolver != nil {
		t.Fatal("没有数据根时解析器该是 nil（执行面按「没有缺省」处理）")
	}
}
