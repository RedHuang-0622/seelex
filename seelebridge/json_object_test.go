package seelebridge

import "testing"

// TestDecodeJSONObjectLenient 钉住"只修语法、不改语义"的容错边界。
//
// 重点用例是 2026-09-16 的真实事故形状：裁决 JSON 的 content 值里有未转义的内层
// 引号，解码在提前闭合的字符串处报 `invalid character 'å'`（'å' 就是紧随其后的
// 中文字首字节），随后被 gate 误判成 B4 缺席。
func TestDecodeJSONObjectLenient(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantKind string
		wantErr  bool
	}{
		{
			name:     "严格：原文本身即对象",
			raw:      `{"kind":"verdict_done","content":"ok"}`,
			wantKind: "verdict_done",
		},
		{
			name:     "围栏：markdown 代码块",
			raw:      "```json\n{\"kind\":\"verdict_not_done\",\"content\":\"x\"}\n```",
			wantKind: "verdict_not_done",
		},
		{
			name:     "夹带解释文字：取首个平衡对象",
			raw:      "我的裁决如下：\n{\"kind\":\"checkpoint_ok\",\"content\":\"x\"}\n以上。",
			wantKind: "checkpoint_ok",
		},
		{
			name:     "值里未转义的内层引号（事故形状）",
			raw:      `{"kind":"verdict_done","content":"并在结尾给出"定义—执行—验证"职责分离，可收口为 done。"}`,
			wantKind: "verdict_done",
		},
		{
			name: "内层引号后紧跟 0xE5 起首的字（复现 invalid character 'å'）",
			raw: "{\"kind\":\"verdict_done\",\"content\":\"交付物与 g-1 的验收" +
				`"` + "对" + `"` + "齐，可收口为 done。\"}",
			wantKind: "verdict_done",
		},
		{
			name:     "字符串里有字面换行",
			raw:      "{\"kind\":\"verdict_done\",\"content\":\"第一行\n第二行\"}",
			wantKind: "verdict_done",
		},
		{
			name:     "值里有右花括号，对象之外的括号不算数",
			raw:      `{"kind":"escalate_human","content":"a}b"} 说明：{这不是 JSON}`,
			wantKind: "escalate_human",
		},
		{
			name:     "路径里的反斜杠不是合法转义",
			raw:      `{"kind":"verdict_done","content":"见 C:\Users\x"}`,
			wantKind: "verdict_done",
		},
		{
			name:    "没有对象：如实失败（不猜内容）",
			raw:     "无法判定，需要人工介入",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				Kind    string `json:"kind"`
				Content string `json:"content"`
			}
			err := decodeJSONObjectLenient(tc.raw, &got)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望解码失败，实际成功：%+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解码失败：%v（原文 %q）", err, tc.raw)
			}
			if got.Kind != tc.wantKind {
				t.Fatalf("kind = %q，期望 %q", got.Kind, tc.wantKind)
			}
		})
	}
}
