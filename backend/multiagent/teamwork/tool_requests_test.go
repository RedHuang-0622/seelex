package teamwork

// tool_requests_test.go 钉住工具入参的**解码与默认值**（P21-37：形状从 handler 搬进本域后，
// schema 与解析结构同处一域）。

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
)

// TestDecodePlanRequestDefaultsVersion：`team_plan` 的 version 缺省 1；其余字段原样。
func TestDecodePlanRequestDefaultsVersion(t *testing.T) {
	plan, err := DecodePlanRequest(`{"team_id":"team-1","members":[{"role":"exec"}],"milestones":[{"id":"m1"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TeamID != "team-1" || plan.Version != 1 || len(plan.Members) != 1 || len(plan.Milestones) != 1 {
		t.Fatalf("解码结果 = %+v", plan)
	}
	explicit, err := DecodePlanRequest(`{"team_id":"t","version":3}`)
	if err != nil || explicit.Version != 3 {
		t.Fatalf("显式版本应当保留：%+v %v", explicit, err)
	}
	if _, err := DecodePlanRequest("not json"); err == nil {
		t.Fatal("非 JSON 必须报错")
	}
}

// TestDecodeDispatchRequestTrimsIDsOnly：`team_dispatch` 只 trim role/item；goal 是这一轮正文，原样。
func TestDecodeDispatchRequestTrimsIDsOnly(t *testing.T) {
	raw, err := DecodeDispatchRequest(`{"role":" exec ","item":" wi-1 ","goal":"  正文  "}`)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Role != "exec" || raw.Item != "wi-1" || raw.Goal != "  正文  " {
		t.Fatalf("解码结果 = %+v", raw)
	}
}

// TestDecodeWorkRequestBuildsSpecs：里程碑 trim；工作项按字段逐项搬成规格（顺序保持）。
func TestDecodeWorkRequestBuildsSpecs(t *testing.T) {
	milestone, specs, err := DecodeWorkRequest(`{"milestone":" m1 ","items":[` +
		`{"id":"wi-1","role":"exec","name":"实现","description":"d","goal":"g","depends_on":["wi-0"]},` +
		`{"id":"wi-2","role":"pm","name":"需求"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if milestone != "m1" || len(specs) != 2 {
		t.Fatalf("解码结果 = %q %+v", milestone, specs)
	}
	if specs[0].ID != "wi-1" || specs[0].Role != "exec" || specs[0].Name != "实现" ||
		specs[0].Description != "d" || specs[0].Goal != "g" || len(specs[0].DependsOn) != 1 {
		t.Fatalf("第一项规格 = %+v", specs[0])
	}
	if specs[1].ID != "wi-2" {
		t.Fatalf("顺序必须保持：%+v", specs)
	}
}

// TestDecodeItemAndNoteRequests：`team_item` 的 id trim + 规格；验收/判负共用 id+note（note 原样）。
func TestDecodeItemAndNoteRequests(t *testing.T) {
	itemID, spec, err := DecodeItemRequest(`{"id":" wi-1 ","role":"exec","depends_on":["a"]}`)
	if err != nil || itemID != "wi-1" || spec.Role != "exec" || len(spec.DependsOn) != 1 {
		t.Fatalf("team_item 解码 = %q %+v %v", itemID, spec, err)
	}
	acceptID, note, err := DecodeItemNoteRequest(`{"id":" wi-2 ","note":"  证据齐全  "}`)
	if err != nil || acceptID != "wi-2" || note != "  证据齐全  " {
		t.Fatalf("验收解码 = %q %q %v", acceptID, note, err)
	}
}

// TestDecodeJoinRequestNormalizes：句柄 trim 并丢弃空白项；预算缺省（<=0）取 5s、给了就用给的。
func TestDecodeJoinRequestNormalizes(t *testing.T) {
	handles, budget, err := DecodeJoinRequest(`{"handles":[" bteam-1 ",""," bteam-2 "],"budget_ms":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(handles) != 2 || handles[0] != jobs.Handle("bteam-1") || handles[1] != jobs.Handle("bteam-2") {
		t.Fatalf("句柄归一 = %+v", handles)
	}
	if budget != JoinDefaultBudget {
		t.Fatalf("缺省预算 = %v, want %v", budget, JoinDefaultBudget)
	}
	if _, budget, _ = DecodeJoinRequest(`{"handles":[],"budget_ms":250}`); budget != 250*time.Millisecond {
		t.Fatalf("显式预算应当保留：%v", budget)
	}
}

// TestDecodeMilestoneRequestTrimsID：里程碑 id trim、content 原样。
func TestDecodeMilestoneRequestTrimsID(t *testing.T) {
	id, content, err := DecodeMilestoneRequest(`{"id":" m1 ","content":"  交付说明  "}`)
	if err != nil || id != "m1" || content != "  交付说明  " {
		t.Fatalf("里程碑解码 = %q %q %v", id, content, err)
	}
}
