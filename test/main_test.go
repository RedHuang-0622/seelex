package test

import (
	"flag"
	"fmt"
	startup "github.com/RedHuang-0622/seelex/internal/startup"
	"github.com/RedHuang-0622/seelex/internal/testutil"
	"os"
	"testing"
)

// launchFlags 是本包摆现场用的启动参数句柄：与入口 `main.go` 用**同一份定义**
// （`startup.BindFlags`），所以默认值即产品默认值（`-permission manual`、
// `-frontend tui`）。用例通过改它的字段摆现场（例如把 `StorePath` 指到临时目录），
// 与搬迁前直接改 `*storePath` 的语义逐字相同。
var launchFlags = startup.BindFlags(flag.CommandLine)

// TestMain 把工作目录切到**仓库根**：本包用例按仓库相对路径读夹具
// （`gui/icons`、`scripts/`、`.claude/`、`plugins/`、`boot/`、`internal/` 的源码文本门禁），
// 而 `go test ./test/` 的默认 CWD 是 `test/`。把它切回仓库根，让搬迁前的相对路径
// 语义逐字保持（这是搬迁，不是重写用例）；用例内部仍可用 `t.Chdir` 切到临时目录。
func TestMain(m *testing.M) {
	root := testutil.RepoRoot()
	if err := os.Chdir(root); err != nil {
		fmt.Fprintf(os.Stderr, "test: 切换到仓库根 %s 失败: %v\n", root, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
