package testutil

import (
	"os"
	"path/filepath"
)

// RepoRoot 返回仓库根目录：从当前工作目录（测试运行时 = 该测试包目录）向上找
// 第一个含 `go.mod` 的目录。
//
// 为什么不写成 `filepath.Dir(cwd)`：那样把"测试包在仓库下第几层"写死了——包一搬家
// （M4 把这些测试从 `seelebridge/` 搬到 `backend/engine/assemble/`）就会去找不存在的
// 路径，而 `go build` 与 `go vet` 都看不见这类错。向上找 go.mod 与层数无关。
//
// 找不到时 panic：测试支持代码，调用方不必各自处理这个"不可能"分支。
func RepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic("testutil: getwd: " + err.Error())
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("testutil: 从 " + dir + " 向上找不到 go.mod")
		}
		dir = parent
	}
}
