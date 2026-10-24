package dto

import (
	"reflect"
	"testing"
)

// role_settings_test.go 钉住四级优先级的**唯一**判据：逐标签取首个出现者。
//
// 用例按"最容易漂移的方向"排：先是高层否决压住低层授予（漂移即越权），再是
// 同层重复、大小写/空白、未知标签的稳定性，最后是整份设置的四级合成。

func TestResolvePermissionVectorTable(t *testing.T) {
	tests := []struct {
		name   string
		layers [][]string
		want   []string
	}{
		{
			name:   "无任何层",
			layers: nil,
			want:   []string{},
		},
		{
			name:   "单层原样保留",
			layers: [][]string{{PermissionTagRO, PermissionTagRW}},
			want:   []string{PermissionTagRO, PermissionTagRW},
		},
		{
			name: "高层授予压住低层否决",
			layers: [][]string{
				{PermissionTagRW},       // user_allow
				{"!" + PermissionTagRW}, // agent_dispatch
				{"!" + PermissionTagRW}, // session_default
			},
			want: []string{PermissionTagRW},
		},
		{
			name: "高层否决压住低层授予",
			layers: [][]string{
				{},
				{"!" + PermissionTagCTL},
				{PermissionTagCTL, PermissionTagRO},
			},
			want: []string{PermissionTagRO},
		},
		{
			name: "未提到的标签由低层决定",
			layers: [][]string{
				{PermissionTagRO},
				{PermissionTagRO, PermissionTagRWSession},
			},
			want: []string{PermissionTagRO, PermissionTagRWSession},
		},
		{
			name: "同层重复以先出现者为准",
			layers: [][]string{
				{PermissionTagADM, "!" + PermissionTagADM},
			},
			want: []string{PermissionTagADM},
		},
		{
			name: "大小写与空白归一",
			layers: [][]string{
				{"  RW  ", "!" + PermissionTagADM},
			},
			want: []string{PermissionTagRW},
		},
		{
			name: "双重否定等于肯定",
			layers: [][]string{
				{"!!" + PermissionTagRO},
			},
			want: []string{PermissionTagRO},
		},
		{
			name: "非法项被宽容跳过而不影响其它标签",
			layers: [][]string{
				{"", "  ", "!", "bad tag", PermissionTagRW},
			},
			want: []string{PermissionTagRW},
		},
		{
			name: "输出按规范标签序而非输入序",
			layers: [][]string{
				{PermissionTagADM, PermissionTagRWSession, PermissionTagRO},
			},
			want: []string{PermissionTagRO, PermissionTagRWSession, PermissionTagADM},
		},
		{
			name: "未知标签排在已知标签之后并按字典序",
			layers: [][]string{
				{"zeta", PermissionTagRW, "alpha"},
			},
			want: []string{PermissionTagRW, "alpha", "zeta"},
		},
		{
			name: "全被否决时返回空集合",
			layers: [][]string{
				{"!" + PermissionTagRO, "!" + PermissionTagRW},
			},
			want: []string{},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := ResolvePermissionVector(testCase.layers...)
			if got == nil {
				t.Fatal("ResolvePermissionVector 返回 nil：契约是非 nil 空切片（调用方不必再判空）")
			}
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("解析结果 = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestResolvePermissionVectorLayersOrder 钉住 RoleSettingsLayers.PermissionVectors
// 的层序就是优先级序（顺序写反会让"会话默认"压住"用户允许"，是越权方向）。
func TestResolvePermissionVectorLayersOrder(t *testing.T) {
	layers := RoleSettingsLayers{
		UserAllow:      RoleSettings{PermissionVector: []string{PermissionTagRO}},
		AgentDispatch:  RoleSettings{PermissionVector: []string{"!" + PermissionTagRW}},
		SessionDefault: RoleSettings{PermissionVector: []string{PermissionTagRW}},
		RoleDefault:    RoleSettings{PermissionVector: []string{"!" + PermissionTagCTL}},
		GlobalDefault:  RoleSettings{PermissionVector: []string{PermissionTagADM}},
	}
	if ordered := (RoleSettingsLayers{}).Ordered(); !reflect.DeepEqual(ordered, make([]RoleSettings, 5)) {
		t.Fatalf("空层 Ordered = %v, want 五份零值", ordered)
	}
	got := ResolvePermissionVector(layers.PermissionVectors()...)
	want := []string{PermissionTagRO, PermissionTagADM}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("层序解析 = %v, want %v（user_allow > agent_dispatch > session_default > role_default > global_default）", got, want)
	}
	if names := PermissionLayerNames(); len(names) != 5 || names[0] != PermissionLayerUserAllow ||
		names[3] != PermissionLayerRoleDefault || names[4] != PermissionLayerGlobalDefault {
		t.Fatalf("PermissionLayerNames = %v，顺序必须是高→低", names)
	}
}

// TestRoleDefaultIsIndependentOfGlobalDefault 钉住角色默认的真实位置：它可以压住
// 全局默认，但不能冒充全局层；派发/会话层仍可显式覆盖它。
func TestRoleDefaultIsIndependentOfGlobalDefault(t *testing.T) {
	layers := RoleSettingsLayers{
		SessionDefault: RoleSettings{PermissionVector: []string{PermissionTagRO}},
		RoleDefault:    RoleSettings{PermissionVector: []string{PermissionTagRW}},
		GlobalDefault:  RoleSettings{PermissionVector: []string{PermissionTagADM}},
	}
	got := ResolvePermissionVector(layers.PermissionVectors()...)
	if want := ([]string{PermissionTagRO, PermissionTagRW, PermissionTagADM}); !reflect.DeepEqual(got, want) {
		t.Fatalf("向量 = %v, want %v", got, want)
	}

	layers.SessionDefault = RoleSettings{PermissionVector: []string{"!" + PermissionTagRW}}
	got = ResolvePermissionVector(layers.PermissionVectors()...)
	if want := ([]string{PermissionTagADM}); !reflect.DeepEqual(got, want) {
		t.Fatalf("会话层覆盖后向量 = %v, want %v", got, want)
	}
}

func TestResolveRoleSettingsFirstNonZeroWins(t *testing.T) {
	layers := []RoleSettings{
		{}, // user_allow 什么都没配
		{Account: "worker-a", ContextWindow: 64000, PermissionVector: []string{"!" + PermissionTagADM}},
		{Account: "worker-b", ContextWindow: 128000, DefaultPlugin: "review", PermissionVector: []string{PermissionTagRW}},
		{Account: "fallback", DefaultPlugin: "search"},
	}
	got := ResolveRoleSettings(layers...)
	if got.Account != "worker-a" || got.ContextWindow != 64000 || got.DefaultPlugin != "review" {
		t.Fatalf("标量字段 = %+v，want account=worker-a window=64000 plugin=review（首个非零层说了算）", got)
	}
	if want := ([]string{PermissionTagRW}); !reflect.DeepEqual(got.PermissionVector, want) {
		t.Fatalf("向量 = %v, want %v", got.PermissionVector, want)
	}
	if !(RoleSettings{}).Empty() {
		t.Fatal("零值设置必须是 Empty")
	}
	if (RoleSettings{ContextWindow: 1}).Empty() {
		t.Fatal("配了 ContextWindow 的设置不是 Empty")
	}
}

func TestNormalizePermissionVector(t *testing.T) {
	normalized, err := NormalizePermissionVector([]string{" RW ", "!adm", "rw", "!!ro"})
	if err != nil {
		t.Fatalf("NormalizePermissionVector: %v", err)
	}
	want := []string{PermissionTagRW, "!" + PermissionTagADM, PermissionTagRO}
	if !reflect.DeepEqual(normalized, want) {
		t.Fatalf("规整结果 = %v, want %v（保留单个 ! 前缀、去重、保序）", normalized, want)
	}
	if empty, err := NormalizePermissionVector(nil); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("空向量 = %v, %v，want 非 nil 空切片", empty, err)
	}
	for _, invalid := range []string{"bad tag", "!", "   "} {
		if _, err := NormalizePermissionVector([]string{invalid}); err == nil {
			t.Fatalf("非法项 %q 必须显式报错（写入侧静默吞掉 = 用户以为给了能力）", invalid)
		}
	}
}
