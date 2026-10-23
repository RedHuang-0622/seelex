package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// role_permission_test.go 钉住会话级角色权限登记的三件事：
//  1. 键是 (sessionID, role)，落盘一份项目级 JSON，换一个 store 实例也能读回来；
//  2. 三态纪律：空向量是"显式不给任何能力"，必须能写进去、读回来、与"没这条记录"
//     区分开（删除只走 Drop）；
//  3. 并发：不同角色同时写不丢更新（每角色一格各自持锁 + 每项目一把文件锁）。

func openRolePermissionRouter(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	router, err := NewRouter(filepath.Join(dir, "session-storage.json"))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// rolePermissionFileOnDisk 找项目级角色权限文件（项目目录名带 hash 前缀）。
func rolePermissionFileOnDisk(t *testing.T, router *Router) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(router.repository.(*jsonRepository).root, "project-*", rolePermissionFileName))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("角色权限文件 = %v, want 恰好一份", matches)
	}
	return matches[0]
}

func TestRolePermissionStoreRoundTrip(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := NewRolePermissionStore(router)

	if _, err := store.Put("proj-a", "sess-1", "tl", []string{" ro ", "rw", "ro"}); err != nil {
		t.Fatalf("Put(tl): %v", err)
	}
	if _, err := store.Put("proj-a", "sess-1", "coworker", []string{"!adm"}); err != nil {
		t.Fatalf("Put(coworker): %v", err)
	}
	if _, err := store.Put("proj-a", "sess-2", "tl", []string{"rw_session"}); err != nil {
		t.Fatalf("Put(另一个会话): %v", err)
	}

	vector, ok, err := store.Vector("proj-a", "sess-1", "tl")
	if err != nil || !ok {
		t.Fatalf("Vector(tl) = %v, %v, %v", vector, ok, err)
	}
	if want := []string{"ro", "rw"}; len(vector) != 2 || vector[0] != want[0] || vector[1] != want[1] {
		t.Fatalf("tl 向量 = %v, want %v（保序去重、去空白）", vector, want)
	}

	// 换一个 store 实例 = 缓存全空，只能从盘上读：验证落盘而不是只改了内存。
	reloaded := NewRolePermissionStore(router)
	vectors, err := reloaded.Vectors("proj-a", "sess-1")
	if err != nil {
		t.Fatalf("Vectors: %v", err)
	}
	if len(vectors) != 2 || len(vectors["tl"]) != 2 || len(vectors["coworker"]) != 1 {
		t.Fatalf("sess-1 登记 = %+v, want tl/coworker 两条", vectors)
	}
	if other, err := reloaded.Vectors("proj-a", "sess-2"); err != nil || len(other) != 1 {
		t.Fatalf("sess-2 登记 = %+v, %v（会话维度必须隔离）", other, err)
	}
	// 未知角色 = 从未登记（调用方据此走四级链路）。
	if _, ok, err := reloaded.Vector("proj-a", "sess-1", "nobody"); err != nil || ok {
		t.Fatalf("未登记角色 = %v, %v, want ok=false", ok, err)
	}
}

func TestRolePermissionExplicitEmptyVectorIsNotMissing(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := NewRolePermissionStore(router)

	record, err := store.Put("proj-a", "sess-1", "coworker", nil)
	if err != nil {
		t.Fatalf("Put(空向量): %v", err)
	}
	if record.PermissionVector == nil || len(record.PermissionVector) != 0 {
		t.Fatalf("空向量记录 = %+v, want 非 nil 空切片", record)
	}

	// 盘上必须是 `"permission_vector": []`：omitempty 会让它变成"没有这个字段"，
	// 于是"显式不给"与"没登记"就分不开了。
	payload, err := os.ReadFile(rolePermissionFileOnDisk(t, router))
	if err != nil {
		t.Fatalf("读盘: %v", err)
	}
	var decoded struct {
		Sessions map[string]map[string]map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("反序列化盘上文件: %v", err)
	}
	raw, ok := decoded.Sessions["sess-1"]["coworker"]["permission_vector"]
	if !ok {
		t.Fatalf("盘上缺 permission_vector 字段：%s", payload)
	}
	var vector []string
	if err := json.Unmarshal(raw, &vector); err != nil || len(vector) != 0 {
		t.Fatalf("盘上向量 = %s, %v, want []", raw, err)
	}

	reloaded := NewRolePermissionStore(router)
	vector, ok, err = reloaded.Vector("proj-a", "sess-1", "coworker")
	if err != nil || !ok {
		t.Fatalf("Vector = %v, %v, %v；显式空集必须 ok=true", vector, ok, err)
	}
	if vector == nil || len(vector) != 0 {
		t.Fatalf("读回向量 = %v, want 非 nil 空切片", vector)
	}
}

func TestRolePermissionDropScopes(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := NewRolePermissionStore(router)
	for _, role := range []string{"tl", "coworker", "reviewer"} {
		if _, err := store.Put("proj-a", "sess-1", role, []string{"ro"}); err != nil {
			t.Fatalf("Put(%s): %v", role, err)
		}
	}
	if err := store.Drop("proj-a", "sess-1", "tl"); err != nil {
		t.Fatalf("Drop(tl): %v", err)
	}
	vectors, err := NewRolePermissionStore(router).Vectors("proj-a", "sess-1")
	if err != nil {
		t.Fatalf("Vectors: %v", err)
	}
	if _, still := vectors["tl"]; still {
		t.Fatalf("Drop 后仍读到 tl：%+v", vectors)
	}
	if len(vectors) != 2 {
		t.Fatalf("Drop 只应影响一个角色，得到 %+v", vectors)
	}

	if err := store.DropSession("proj-a", "sess-1"); err != nil {
		t.Fatalf("DropSession: %v", err)
	}
	if vectors, err := NewRolePermissionStore(router).Vectors("proj-a", "sess-1"); err != nil || len(vectors) != 0 {
		t.Fatalf("DropSession 后 = %+v, %v", vectors, err)
	}
}

// TestRolePermissionConcurrentRoleWritesDoNotLoseUpdates 是本文件的核心并发用例：
// 8 个角色同时写同一个会话的同一份文件。每角色一格各自持锁，落盘共用一把按项目
// 分配的文件锁——缺任何一半，整份文件的读-改-写都会丢别的角色的更新。
func TestRolePermissionConcurrentRoleWritesDoNotLoseUpdates(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := NewRolePermissionStore(router)

	roles := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8"}
	var wait sync.WaitGroup
	for _, role := range roles {
		role := role
		wait.Add(3)
		go func() {
			defer wait.Done()
			if _, err := store.Put("proj-a", "sess-1", role, []string{"ro", role}); err != nil {
				t.Errorf("Put(%s): %v", role, err)
			}
		}()
		go func() {
			defer wait.Done()
			if _, _, err := store.Vector("proj-a", "sess-1", role); err != nil {
				t.Errorf("Vector(%s): %v", role, err)
			}
		}()
		go func() {
			defer wait.Done()
			if _, err := store.Vectors("proj-a", "sess-1"); err != nil {
				t.Errorf("Vectors: %v", err)
			}
		}()
	}
	wait.Wait()

	vectors, err := NewRolePermissionStore(router).Vectors("proj-a", "sess-1")
	if err != nil {
		t.Fatalf("Vectors: %v", err)
	}
	if len(vectors) != len(roles) {
		t.Fatalf("并发写后登记 %d 条, want %d：%+v", len(vectors), len(roles), vectors)
	}
	for _, role := range roles {
		vector, ok := vectors[role]
		if !ok || len(vector) != 2 || vector[1] != role {
			t.Fatalf("角色 %s 的登记 = %v, %v", role, vector, ok)
		}
	}
}

func TestRolePermissionRejectsEmptyCoordinates(t *testing.T) {
	store := NewRolePermissionStore(openRolePermissionRouter(t))
	if _, err := store.Put("proj-a", "  ", "tl", []string{"ro"}); err == nil {
		t.Fatal("缺会话号必须报错")
	}
	if _, err := store.Put("proj-a", "sess-1", " ", []string{"ro"}); err == nil {
		t.Fatal("缺角色名必须报错")
	}
	if _, _, err := store.Vector("proj-a", "", "tl"); err == nil {
		t.Fatal("缺会话号读取必须报错")
	}
	if err := store.Drop("proj-a", "sess-1", ""); err == nil {
		t.Fatal("缺角色名删除必须报错")
	}
}

// TestRolePermissionStoreWithoutRouter 钉住"没装配存储就别假装成功"。
func TestRolePermissionStoreWithoutRouter(t *testing.T) {
	store := NewRolePermissionStore(nil)
	if vectors, err := store.Vectors("proj-a", "sess-1"); err != nil || len(vectors) != 0 {
		t.Fatalf("无 router 读 = %+v, %v, want 空登记且不报错", vectors, err)
	}
	if _, err := store.Put("proj-a", "sess-1", "tl", []string{"ro"}); err == nil {
		t.Fatal("无 router 写必须显式失败")
	}
}
