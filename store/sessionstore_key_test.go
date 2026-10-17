package store

// sessionstore_key_test.go 钉住 `Key.Complete`：项目与会话两个分量（trim 后）都在，
// 这个键才指向一份会话。装配层的团队作用域解析与看板域的取数判定共用这一句。

import "testing"

func TestKeyComplete(t *testing.T) {
	cases := []struct {
		name string
		key  Key
		want bool
	}{
		{"两个分量齐全", Key{ProjectID: "p-1", SessionID: "s-1"}, true},
		{"两侧留白仍算齐全", Key{ProjectID: " p-1 ", SessionID: "\ts-1\n"}, true},
		{"缺项目", Key{SessionID: "s-1"}, false},
		{"缺会话", Key{ProjectID: "p-1"}, false},
		{"只有空白", Key{ProjectID: "  ", SessionID: "\t"}, false},
		{"零值", Key{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.key.Complete(); got != c.want {
				t.Fatalf("%+v.Complete() = %v, want %v", c.key, got, c.want)
			}
		})
	}
}
