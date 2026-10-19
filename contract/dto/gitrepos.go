// 本文件：工作区内 git 仓库发现（BFS 扫描）的只读 DTO——只含仓库位置/展示名/
// 是否主仓库/当前分支，绝不携带提交、diff、文件内容或绝对路径。
package dto

// GitRepoRef 是扫描到的一个 git 仓库（相对工作区根的只读引用）。
// Rel 是相对工作区根的路径（"/" 分隔）；工作区根自身是仓库时为 "."。
// Name 是展示名：根视角（Rel=="."）用工作区名，其余用目录 basename。
// Primary 是前端默认展示的那一个（根是仓库，或根落在某个祖先仓库里）。
// Branch 读不到就留空，不阻塞列表。
type GitRepoRef struct {
	Rel     string `json:"rel"`              // 相对工作区根（"/" 分隔）；根视角 = "."
	Name    string `json:"name"`             // 展示名（basename；根视角用工作区名）
	Primary bool   `json:"primary"`          // 默认展示的那一个
	Branch  string `json:"branch,omitempty"` // 当前分支（读不到留空）
}

// GitReposResult 是一次仓库发现的结果（GUI 仓库标签栏数据源）。
// Truncated 表示已经达到深度/目录/条目预算，可能还有没扫到的仓库（如实上报）；
// Scanned 是真正进过的目录数（"扫了多深"要能说清）；Error 是非致命展示文案
// （未绑定工作区、根不可读等），此时 Repos 为空而不是报错。
type GitReposResult struct {
	Root      string       `json:"root"`
	Repos     []GitRepoRef `json:"repos"`
	Truncated bool         `json:"truncated"`
	Scanned   int          `json:"scanned"`
	Error     string       `json:"error,omitempty"`
}
