package store

// unit_record_writer_test.go — 写侧责任链的形状（用户口径，2026-10-06）：
// 每一环只**增补**自己这一层知道的事实、越外层的环越先写、派生新链不动本链。
//
// 场景对应产品里的两层：subagent 那条链是"内层只填空缺"（身份已经有了就不动），teammate
// 那一层是"在外面再包一环"（先写下自己的身份）——于是 teammate 记录 = subagent 记录 + 身份，
// 不必另立第二条写路径。

import (
	"strings"
	"testing"

	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

func newUnitRecordWriterFixture(t *testing.T) (*storeworkunit.SessionStore, *storeworkunit.Writer) {
	t.Helper()
	router, err := NewRouter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	store := NewWorkUnitSessionStore(router)
	// 内层（子代理那一层）：**只填空缺**——身份已经有了就不动它。
	inner := func(record storeworkunit.Record) storeworkunit.Record {
		if strings.TrimSpace(record.Unit.Kind) == "" {
			record.Unit.Kind = "subagent"
		}
		return record
	}
	return store, storeworkunit.NewWriter(store).With(inner)
}

func TestUnitRecordWriterChainFillsWithoutClobbering(t *testing.T) {
	store, base := newUnitRecordWriterFixture(t)
	// 外层（teammate 那一层）：先写下自己这一层的身份。
	teammate := base.With(func(record storeworkunit.Record) storeworkunit.Record {
		record.Unit.Kind = "teammate"
		record.Unit.Role = "exec"
		return record
	})

	// ① 外层先写：teammate 写下的身份不许被内层"只填空"的那一环覆盖。
	if err := teammate.Save("p", "sess-1", storeworkunit.Record{NodeID: "n1", SessionID: "s1"}); err != nil {
		t.Fatalf("teammate 链 Save: %v", err)
	}
	// ② 派生新链不动本链：同一条 base 写出来的仍是子代理那一层。
	if err := base.Save("p", "sess-1", storeworkunit.Record{NodeID: "n2", SessionID: "s2"}); err != nil {
		t.Fatalf("base 链 Save: %v", err)
	}

	records, err := store.List("p", "sess-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]storeworkunit.Record{}
	for _, record := range records {
		byID[record.NodeID] = record
	}
	if got := byID["n1"].Unit; got.Kind != "teammate" || got.Role != "exec" {
		t.Errorf("外层那一环写的身份被覆盖了：%+v", got)
	}
	if got := byID["n2"].Unit; got.Kind != "subagent" || got.Role != "" {
		t.Errorf("base 链应只写子代理那一层：%+v", got)
	}
}

// TestUnitRecordWriterWithoutStoreFailsLoud —— 链没装末端（未装配存储）时**显式报错**：
// 记录是恢复用的证据，静默丢掉就是"重启失忆"被当成正常。
func TestUnitRecordWriterWithoutStoreFailsLoud(t *testing.T) {
	if err := storeworkunit.NewWriter(nil).Save("p", "sess-1", storeworkunit.Record{NodeID: "n", SessionID: "s"}); err == nil {
		t.Fatal("链没装末端时必须报错，不许静默丢记录")
	}
	var missing *storeworkunit.Writer
	if err := missing.Save("p", "sess-1", storeworkunit.Record{NodeID: "n", SessionID: "s"}); err == nil {
		t.Fatal("nil 链必须报错")
	}
	if missing.With(func(record storeworkunit.Record) storeworkunit.Record { return record }) != nil {
		t.Fatal("对 nil 链派生应当仍是 nil（不制造半个链）")
	}
}
