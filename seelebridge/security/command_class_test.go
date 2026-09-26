package security

import "testing"

// bash_read 服务端守卫的表驱动用例（打点 K-4 / TC-K4-1..TC-K4-4）。
//
// 形态一律表驱动：本契约的判据就是"一串命令 → 一个分类"，逐条写 t.Run 只会把
// 同一张表拆成几十个函数。最关键的一条不在表里、在最后一个用例：
// ClassifyCommand 的签名上只有命令字符串，**拿不到模型的任何主张**。
func TestClassifyCommandTable(t *testing.T) {
	cases := []struct {
		name    string
		command string
		readOny bool
	}{
		// TC-K4-1：只读命令必须通过（否则 bash_read 就是死工具）。
		{"ls 带参数", "ls -la", true},
		{"git status", "git status", true},
		{"git log", "git log --oneline -5", true},
		{"git diff", "git diff --stat HEAD", true},
		{"go test", "go test ./... -count=1", true},
		{"go build", "go build ./...", true},
		{"go vet", "go vet ./seelebridge/tools/", true},
		{"grep", `grep -rn "func main" --include=*.go .`, true},
		{"rg", "rg -n TODO seelebridge", true},
		{"cat", "cat README.md", true},
		{"head/tail 管道式用法", "tail -n 20 docs/README.md", true},
		{"find 只读", `find . -name "*.go"`, true},
		{"git stash list", "git stash list", true},

		// TC-K4-2：写子命令黑名单必须被拒。
		{"git commit", "git commit -m x", false},
		{"npm install", "npm install", false},
		{"rm -rf", "rm -rf build", false},
		{"mv", "mv a b", false},
		{"cp", "cp a b", false},
		{"mkdir", "mkdir -p tmp/x", false},
		{"make", "make build", false},
		{"go mod tidy", "go mod tidy", false},
		{"go run", "go run ./cmd/x", false},
		{"git push", "git push origin main", false},
		{"git config 写入", "git config user.name someone", false},
		{"git branch 建分支", "git branch feature/x", false},
		{"git stash 落盘", "git stash", false},
		{"sudo", "sudo ls /root", false},
		{"写工具进只读面", "python -c \"open('x','w')\"", false},
		{"find -delete", "find . -name '*.tmp' -delete", false},
		{"git diff --output", "git diff --output=patch.diff", false},

		// TC-K4-3：写重定向必须被拒（重定向语法整串拒绝，无论首词是什么）。
		{"写文件", "echo hi > f", false},
		{"追加重定向", "ls >> out.txt", false},
		{"tee 管道", "cat x | tee y", false},
		{"读重定向", "cat < input.txt", false},

		// TC-K4-4：分类失败的一切形态一律按写处理（绝不能"没识别出来就放行"）。
		{"空命令", "   ", false},
		{"未知首词", "frobnicate --all", false},
		{"复合命令", "ls; rm -rf build", false},
		{"与串", "ls && rm -rf build", false},
		{"或串", "ls || rm -rf build", false},
		{"变量展开", "cat $HOME/.ssh/id_rsa", false},
		{"命令替换", "echo $(rm -rf build)", false},
		{"后台符", "ls &", false},
		{"换行复合", "ls\nrm -rf build", false},
		{"裸 git", "git", false},
		{"git 带 -C", "git -C repo status", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ClassifyCommand(testCase.command); got != testCase.readOny {
				t.Fatalf("ClassifyCommand(%q) = %v, want %v", testCase.command, got, testCase.readOny)
			}
		})
	}
}

// 模型在入参里写下与命令矛盾的说明时判定不变：ClassifyCommand 只吃命令字符串，
// 签名上就拿不到模型的任何主张（self-declared privilege 不是授权依据）。
func TestClassifyCommandIgnoresModelClaims(t *testing.T) {
	// 说明文本与命令无法一起传进来（这正是设计意图）：同一条写命令，无论调用方
	// 在别处怎么声称"这是只读的"，判定结果都必须是拒绝。
	for _, command := range []string{"rm -rf build", "git commit -m x"} {
		if ClassifyCommand(command) {
			t.Fatalf("写命令 %q 被判成只读：模型主张不得改变服务端分类", command)
		}
	}
}
