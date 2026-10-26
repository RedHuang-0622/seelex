package core

import (
	"encoding/json"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
	"testing"
)

// FuzzBuildWorkTable 以畸形输入喂投影构建器：深层嵌套、错类型、超大文本
// 都必须产出有界行且不 panic（白盒健壮性）。
func FuzzBuildWorkTable(f *testing.F) {
	f.Add(`{"status":"running","nodes":[{"id":"n1","label":"a","status":"completed","output":"x","events":[{"status":"queued","at":"2026-01-01T00:00:00Z"}]}]}`, `[]`)
	f.Add(`{"status":"completed","nodes":[],"edges":[]}`, `[{"id":"todo:0","phase":"tasklist","task":"a","status":"doing","kind":"todo"},{"id":"subagent:s1","phase":"subagent","task":"g","status":"running","kind":"subagent"}]`)
	f.Add(`not-json`, `not-json`)
	f.Add(``, `[]`)

	f.Fuzz(func(t *testing.T, planJSON, taskJSON string) {
		var plan *model.PlanState
		if planJSON != "" {
			var candidate model.PlanState
			if err := json.Unmarshal([]byte(planJSON), &candidate); err == nil {
				plan = &candidate
			}
		}
		var tasks []dto.TaskRecord
		_ = json.Unmarshal([]byte(taskJSON), &tasks)

		rows := worktable.BuildWorkTable(plan, tasks, nil, nil, workTableBounds())
		if len(rows) > Limits().WorkTableRows {
			t.Fatalf("rows = %d exceed limit %d", len(rows), Limits().WorkTableRows)
		}
		for _, row := range rows {
			if row.ID == "" || row.Phase == "" || row.Kind == "" || row.Status == "" {
				t.Fatalf("incomplete work item: %+v", row)
			}
			for _, point := range row.Trace {
				if point.Status == "" {
					t.Fatalf("trace point without status: %+v", point)
				}
			}
		}
	})
}
