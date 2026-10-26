package config

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/backend/limit/overlay"
	"github.com/RedHuang-0622/seelex/internal/bootseed"
	"github.com/RedHuang-0622/seelex/kernel/prompt/assets"
	"github.com/RedHuang-0622/seelex/store/root"
)

// RuntimeLimitsPath 把「运行时上限覆盖层」的落点绑到数据根：数据根来自 `-store`
// 参数（组合根的事），拼接口径在 `backend/limit/overlay.Path`（IO 与形状在那一层）。
func RuntimeLimitsPath(storePath string) string {
	return overlay.Path(filepath.Dir(storePath))
}

// AccountsPath 返回 accounts.yaml 的路径。
// 优先使用二进制所在目录（正式部署），回退到当前工作目录（go run / 开发场景）。
func AccountsPath() string {
	exe, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(exe), "config", "accounts.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join("config", "accounts.yaml")
}

// RuntimeConfigChain 返回配置文件在责任链上的候选（按优先级）与允许初始化的落盘根：
//
//  1. config/<name>        CWD 相对（仓库里那份 / 开发场景；用户改过的就是它）
//  2. <name>               根目录回退（历史兼容）
//  3. <exe>/config/<name>  包内配置（正式部署：二进制旁边自带一份）
//
// 落盘根**固定取 <exe>/config**：CWD 可能是用户的项目目录，不能在那里凭空造出
// config/ 来；二进制所在目录才是这个应用自己的地盘。落盘失败（只读安装目录等）
// 由调用方回退代码默认值，不阻断启动。
func RuntimeConfigChain(name string) (candidates []string, seedRoot string) {
	candidates = []string{filepath.Join("config", name), name}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, "config", name))
		seedRoot = filepath.Join(exeDir, "config")
	}
	return candidates, seedRoot
}

// GUIAppearancePath 用同一套配置责任链给出 **GUI 外观设置**（当前皮肤 + 窗口材质）的
// 落盘路径，口径与 EnsureConfigFile 一致，只差"不播种"：外观没有"内嵌默认档"要下发，
// 缺省就是代码里的不透明，所以这里只挑路径——
//
//	存在即读也即写：链上第一份已存在的文件就是它（读和写都落同一份，不会出现
//	                "读 CWD 那份、写 <exe> 那份"的错位）；
//	全缺则落到 <exe>/config（与其它配置同一个"应用自己的地盘"，不往用户项目目录里造）。
//
// 链本身由 RuntimeConfigChain 给（CWD config/ → CWD → <exe>/config/）。
func GUIAppearancePath() string {
	candidates, seedRoot := RuntimeConfigChain("gui-appearance.json")
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if seedRoot != "" {
		return filepath.Join(seedRoot, "gui-appearance.json")
	}
	return candidates[0]
}

// EnsureConfigFile 按责任链给出配置文件路径（口径见 internal/bootseed）：
//
//	存在即读：候选链上第一份存在的文件就是答案——用户改过的、包内自带的、
//	          仓库里的，都优先于默认数据，本函数一个字节都不写。
//	缺失即初始化：候选链上全都没有 → 用内嵌默认档在 <exe>/config 里落盘，再读它。
//
// 返回"该读哪个路径"；确实没有可用文件时返回链上第一个候选（与老口径一致：让
// 宽容的加载器按"文件不存在"处置，走代码默认值）。
func EnsureConfigFile(name string, pack bootseed.Pack) string {
	candidates, seedRoot := RuntimeConfigChain(name)
	result, err := bootseed.Resolve(bootseed.Spec{
		Name:       name,
		Candidates: candidates,
		SeedRoot:   seedRoot,
		Entry:      name,
		Pack:       pack,
	})
	if err != nil {
		log.Printf("config: %s 初始化默认档失败（回退代码默认值）: %v", name, err)
		return candidates[0]
	}
	switch result.Kind {
	case bootseed.KindSeeded:
		log.Printf("config: %s 候选链上都没有，已用内嵌默认档初始化到 %s（存在即读：之后直接用这份）", name, result.Path)
	case bootseed.KindMissing:
		log.Printf("config: %s 候选链上都没有，且无处落盘（回退代码默认值）", name)
	}
	if result.Path != "" {
		return result.Path
	}
	return candidates[0]
}

// PromptDirChain 返回提示词资产目录在责任链上的候选与落盘根（口径同
// RuntimeConfigChain，只是载荷从"一个文件"变成"一个目录"）：
//
//  1. config/prompt/system/instructions.md        CWD 相对（仓库 / go run 开发场景；
//     用户改过的就是它）
//  2. <exe>/config/prompt/system/instructions.md  包内目录（正式部署：二进制旁边自带一份）
//
// 判据取目录里的入口文件（promptassets.Entry）——空目录不算命中，会走初始化。
// 落盘根固定取 <exe>/config/prompt：CWD 可能是用户的项目目录，不能在那里凭空造出
// 一个 prompt/ 来。
func PromptDirChain() (candidates []string, seedRoot string) {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return PromptDirChainAt(exe)
}

// PromptDirChainAt 是 PromptDirChain 的可测内核（可执行文件路径由调用方给，
// 测试才好摆现场），与 PluginRootChain 同一姿势。
func PromptDirChainAt(exePath string) (candidates []string, seedRoot string) {
	entry := filepath.FromSlash(promptassets.Entry)
	candidates = []string{filepath.Join("config", "prompt", entry)}
	if exePath != "" {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, "config", "prompt", entry))
		seedRoot = filepath.Join(exeDir, "config", "prompt")
	}
	return candidates, seedRoot
}

// ResolvePromptDir 按责任链给出**提示词资产目录**并返回它（调用方拿它设
// promptassets.SetDir）。口径与 EnsureConfigFile 一致：
//
//	存在即读：候选链上第一份存在的入口文件所在目录就是答案——用户改过的优先于
//	          内嵌默认，本函数一个字节都不写。
//	缺失即初始化：候选链全缺 → 用内嵌默认词在 <exe>/config/prompt 落盘，再读它。
//
// 返回 "" 表示没有可用目录（无处落盘）：调用方据此让 promptassets 全走内嵌词，
// 不视为启动失败（与"配置读不到就回退代码默认值"同一条口径）。
func ResolvePromptDir() string {
	candidates, seedRoot := PromptDirChain()
	return ResolvePromptDirAt(candidates, seedRoot)
}

// ResolvePromptDirAt 是 ResolvePromptDir 的可测内核（候选链与落盘根由调用方给）。
func ResolvePromptDirAt(candidates []string, seedRoot string) string {
	pack := bootseed.Pack{Name: "prompt"}
	for _, rel := range promptassets.DefaultFiles() {
		pack.Files = append(pack.Files, bootseed.File{Rel: rel, Source: rel})
	}
	result, err := bootseed.ResolveFS(bootseed.Spec{
		Name:       "prompt",
		Candidates: candidates,
		SeedRoot:   seedRoot,
		Entry:      promptassets.Entry,
		Pack:       pack,
	}, promptassets.DefaultFS())
	if err != nil {
		log.Printf("prompt: 初始化默认提示词失败（回退内嵌提示词）: %v", err)
		return ""
	}
	switch result.Kind {
	case bootseed.KindHit:
		// 命中的是"入口文件"，result.Root 是入口所在目录
		// （…/config/prompt/system）；SetDir 要的是提示词根，按 Entry 层级上溯。
		return promptassets.DirOf(result.Path)
	case bootseed.KindSeeded:
		log.Printf("prompt: 候选链上都没有，已用内嵌默认词初始化到 %s（存在即读：之后直接用这份）", result.Root)
		return result.Root
	case bootseed.KindMissing:
		log.Printf("prompt: 候选链上都没有，且无处落盘（回退内嵌提示词）")
	}
	return ""
}

// ResolveAutoGetJobsDir 定位 auto_get_jobs 脚本目录（责任链 + 缺失即初始化，
// 口径见 internal/bootseed）：
//
//	存在即读：CWD 相对的 local/tools/auto_get_jobs/main.py、二进制旁边那份，
//	          谁先存在就用谁（用户自己放好的脚本永远优先）。
//	缺失即初始化：两处都没有 main.py → 把**骨架**（README.md + .env.example）
//	          写到二进制旁边的 local/tools/auto_get_jobs/，让"该放什么、放哪儿"
//	          在文件系统上可见；脚本本体是第三方项目，不随包分发。
//
// 命令登记的安全口径不变：没有 main.py 就不登记（登记即信任、argv 固定直传）。
func ResolveAutoGetJobsDir() (string, bool) {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	return ResolveToolDir(filepath.Join("local", "tools", "auto_get_jobs"), exeDir, bootseed.AutoGetJobsPack())
}

// ResolveToolDir 是 ResolveAutoGetJobsDir 的可测内核（exeDir 由调用方给，测试才好
// 摆现场）：按责任链找工具目录，两个候选是 CWD 相对的那份与 <exeDir>/ 下那份。
// 命中即用；全缺则把骨架（pack）初始化到 <exeDir>/<relative>，但没有 main.py 就
// 不认账——返回 false，调用方跳过白名单命令登记。
func ResolveToolDir(relative, exeDir string, pack bootseed.Pack) (string, bool) {
	candidates := []string{filepath.Join(relative, "main.py")}
	seedRoot := ""
	if exeDir != "" {
		candidates = append(candidates, filepath.Join(exeDir, relative, "main.py"))
		seedRoot = filepath.Join(exeDir, relative)
	}
	result, err := bootseed.Resolve(bootseed.Spec{
		Name:       relative,
		Candidates: candidates,
		SeedRoot:   seedRoot,
		Pack:       pack,
	})
	if err != nil {
		log.Printf("scheduled tasks: 初始化 %s 目录骨架失败: %v", relative, err)
	}
	if result.Kind == bootseed.KindHit {
		return filepath.Dir(result.Path), true
	}
	if len(result.Written) > 0 {
		log.Printf("scheduled tasks: %s 脚本目录未找到（期望 %s），已初始化骨架 %v；仍缺 main.py，跳过该白名单命令登记", pack.Name, relative, result.Written)
		return "", false
	}
	log.Printf("scheduled tasks: %s 脚本目录未找到（期望 %s），跳过该白名单命令登记", pack.Name, relative)
	return "", false
}

// ResolvePythonCommand 探测可用的 python 解释器（python → py；找不到时返回空）。
func ResolvePythonCommand() string {
	for _, candidate := range []string{"python", "py"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	log.Printf("scheduled tasks: 未找到 python 解释器，跳过 auto_get_jobs 白名单命令登记")
	return ""
}

// ResolveStorePath 确保多实例不冲突：检测 .lock 文件中的 PID，
// 若该 PID 还活着则自动递增路径后缀（sessions → sessions_1 → sessions_2…）。
func ResolveStorePath(basePath string) string {
	for i := 0; i < 100; i++ {
		path := basePath
		if i > 0 {
			path = basePath + "_" + strconv.Itoa(i)
		}
		lockFile := filepath.Join(path, ".lock")
		if tryAcquireLock(lockFile) {
			return path
		}
	}
	// 理论上不会到这里（100 个实例够多了）
	path := basePath + "_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	os.MkdirAll(path, 0755)
	return path
}

// tryAcquireLock 尝试创建锁文件并写入当前 PID。返回 true 表示获取成功。
// 如果锁文件已存在但持有进程已死（stale lock），则覆盖。
func tryAcquireLock(lockFile string) bool {
	// 检查已有锁
	if data, err := os.ReadFile(lockFile); err == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil && pid > 0 && root.ProcessAlive(pid) {
			return false // 锁被活着的进程持有
		}
		// Stale lock — 清理
		os.Remove(lockFile)
	}
	// 创建目录 + 锁文件
	if err := os.MkdirAll(filepath.Dir(lockFile), 0755); err != nil {
		return false
	}
	return os.WriteFile(lockFile, []byte(strconv.Itoa(os.Getpid())), 0644) == nil
}
