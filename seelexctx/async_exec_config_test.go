package seelexctx

import (
	"path/filepath"
	"testing"
)

// 两条口径必须分开钉住（§10 J9）：
//   - 出厂配置（config/seelex.yaml）= 后台命令能力常驻开；
//   - 结构零值 / 配置里缺这段 = 关（这是"秒回滚"与旧配置文件兼容的根据）。
func TestAsyncExecShippedConfigOn(t *testing.T) {
	loaded, err := LoadLimits(filepath.Join("..", "config", "seelex.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.WithDefaults().AsyncExec.Enabled {
		t.Fatal("出厂配置必须让作业面常驻开（bash_bg / read_batch / job_manage）")
	}
}

func TestAsyncExecZeroValueStaysOff(t *testing.T) {
	if limits := (Limits{}).WithDefaults(); limits.AsyncExec.Enabled {
		t.Fatal("结构零值必须保持关闭：缺这段配置的旧文件不得被悄悄打开能力")
	}
}
