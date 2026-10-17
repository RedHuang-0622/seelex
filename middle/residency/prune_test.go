package residency

import (
	"reflect"
	"testing"
)

// 本文件钉 `PruneOrder` 的收敛口径（M5-R6-residency-order 的收尾：pruneContentOrderLocked 的
// 收养/丢弃判据最后一处副本也搬进本包）。

func TestPruneOrderDropsUnloadedDedupesAndAdopts(t *testing.T) {
	loaded := map[string]bool{"a": true, "b": false, "c": true, "d": true}
	order := Order{"a", "b", "a"} // b 已不持有正文；a 重复
	got := PruneOrder(order, func(id string) bool { return loaded[id] }, []string{"c", "a", "d"})
	if want := (Order{"a", "c", "d"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("收敛后 = %v, want %v（保留序内仍持有者、去重、按单元表顺序收养）", got, want)
	}
}

func TestPruneOrderEmptyInputs(t *testing.T) {
	if got := PruneOrder(nil, func(string) bool { return true }, nil); len(got) != 0 {
		t.Fatalf("空输入应收敛成空序：%v", got)
	}
	// 单元表里的会话若未持有正文，不收养。
	if got := PruneOrder(nil, func(string) bool { return false }, []string{"x"}); len(got) != 0 {
		t.Fatalf("未持有正文的会话不该被收养：%v", got)
	}
	// 已在序里且仍持有的会话不会被重复收养。
	got := PruneOrder(Order{"x"}, func(string) bool { return true }, []string{"x"})
	if want := (Order{"x"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("不该重复收养：%v, want %v", got, want)
	}
}
