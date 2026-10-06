package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestToolCallStatusIsTheToolEventCell 是本格的类型化判据：字段本身就是那一格
// （写错词由编译器钉住），不是"另一份字符串词表"。
func TestToolCallStatusIsTheToolEventCell(t *testing.T) {
	tool := ToolCall{ID: "c1", Name: "bash", Status: dto.ToolEventSuccess}
	if tool.Status != dto.ToolEventSuccess {
		t.Fatalf("ToolCall.Status = %v, want dto.ToolEventSuccess", tool.Status)
	}
	if tool.Status.String() != "success" {
		t.Fatalf("对外词 = %q, want %q", tool.Status.String(), "success")
	}
}

// TestToolCallStatusRecordFold 钉住落盘那一侧（`model.ToolCall` 同时是存档/事件 wire 的形状）：
// 老记录里的词读回**不炸**，认不得的词既不折成"成功"、也不报错——落到"未知"。
func TestToolCallStatusRecordFold(t *testing.T) {
	cases := []struct {
		payload string
		want    dto.ToolEventStatus
		why     string
	}{
		{`{"id":"c1","name":"bash","status":"success"}`, dto.ToolEventSuccess, "本格的词原样读回"},
		{`{"id":"c1","name":"bash","status":"running"}`, dto.ToolEventRunning, "本格的词原样读回"},
		{`{"id":"c1","name":"bash","status":"error"}`, dto.ToolEventError, "本格的词原样读回"},
		{`{"id":"c1","name":"bash"}`, dto.ToolEventUnknown, "老记录没有这一栏：不炸，也不当成成功"},
		{`{"id":"c1","name":"bash","status":""}`, dto.ToolEventUnknown, "空词同上"},
		{`{"id":"c1","name":"bash","status":"completed"}`, dto.ToolEventUnknown,
			"历史上一处漂移写点（子代理详情投影）写过的词：本格不认它，更不许拿它冒充成功"},
	}
	for _, testCase := range cases {
		var tool ToolCall
		if err := json.Unmarshal([]byte(testCase.payload), &tool); err != nil {
			t.Fatalf("落盘读回不许炸（%s / %s）：%v", testCase.payload, testCase.why, err)
		}
		if tool.Status != testCase.want {
			t.Fatalf("%s：读回 %v, want %v（%s）", testCase.payload, tool.Status, testCase.want, testCase.why)
		}
	}

	// wire 形状不变：写出去仍是本格的词（前端与老工具链不用动）。
	encoded, err := json.Marshal(ToolCall{ID: "c1", Name: "bash", Status: dto.ToolEventSuccess})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"status":"success"`) {
		t.Fatalf("wire 形状变了：%s", encoded)
	}
}

// BenchmarkToolCallRecordDecode 量一下"落盘读回多折一次"的代价量级：
// 同一个载荷，一次走本格的宽松读法（ToolCall.UnmarshalJSON → 具名转换点），
// 一次走不带方法的名字面（plain alias，等价于枚举化之前的纯结构解码）。
// 只报量级，不主张快慢。
func BenchmarkToolCallRecordDecode(b *testing.B) {
	payload := []byte(`{"id":"call-1","name":"read_file","arguments":"{\"path\":\"a.go\"}","result":"package a","status":"success","duration":12000000,"total_chars":9000}`)

	b.Run("with_fold", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			var tool ToolCall
			if err := json.Unmarshal(payload, &tool); err != nil {
				b.Fatalf("unmarshal: %v", err)
			}
		}
	})

	b.Run("plain_alias", func(b *testing.B) {
		type plain ToolCall
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			var tool plain
			if err := json.Unmarshal(payload, &tool); err != nil {
				b.Fatalf("unmarshal: %v", err)
			}
		}
	})
}
