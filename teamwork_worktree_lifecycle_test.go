package main

// teamwork_worktree_lifecycle_test.go — **teammate 那条链的现场生命周期端到端**
// （真 git + 真装配 + 脚本化 provider；不调真实 API，因此是默认跑的那一档）。
//
// 为什么补这一条：既有用例里，teammate 链的"现场"只有两半各自被验过——
//   - `TestDispatchBindsTeammateSceneAndRecordsLedger`（`seelebridge/teamwork`）用**替身**
//     验"派发会调 BindWorkspace、指派名写进成员与账本"（不碰真 git）；
//   - `TestWorkItemWorktreeIsBoundAsTeammateRoot`（`seelebridge`）验"绑根落到现场"，
//     但**没有 worker 回合真的在里面干活**；
//   - `TestTeamworkHeadlessSmoke` 走真装配，但 worker 回合只回正文、**不产文件**，
//     于是"现场上有工作 + 合回主工作区"这一跳从未被断言过。
// 本文件把三跳连起来：**派发 → 现场上有真文件 → 合并回主工作区 → 析构销项按 teammate
// 自己的策略走（收尾/验收都不拆会话，整队收口才是唯一回收点）**。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// sceneProbe 是运行期对 teammate 现场的逐帧取证（只读轮询，不改运行时行为）。
type sceneProbe struct {
	mu              sync.Mutex
	dir             string
	file            string
	mainFile        string
	dirSeen         bool
	fileInScene     bool
	fileInMain      bool
	sawBothAtOnce   bool
	samples         int
	firstMainFileAt time.Time
}

func (p *sceneProbe) record() {
	dirSeen := false
	if info, err := os.Stat(p.dir); err == nil && info.IsDir() {
		dirSeen = true
	}
	fileInScene := false
	if _, err := os.Stat(filepath.Join(p.dir, p.file)); err == nil {
		fileInScene = true
	}
	fileInMain := false
	if _, err := os.Stat(p.mainFile); err == nil {
		fileInMain = true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples++
	p.dirSeen = p.dirSeen || dirSeen
	p.fileInScene = p.fileInScene || fileInScene
	if fileInMain && !p.fileInMain {
		p.firstMainFileAt = time.Now()
	}
	p.fileInMain = p.fileInMain || fileInMain
	if dirSeen && fileInScene {
		p.sawBothAtOnce = true
	}
}

func (p *sceneProbe) snapshot() (dirSeen, fileInScene, fileInMain, together bool, samples int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dirSeen, p.fileInScene, p.fileInMain, p.sawBothAtOnce, p.samples
}

// TestTeamworkWorktreeLifecycleChain 端到端验 teammate 那条链：
//
//	① 派发建出**现场**（真 git worktree，落在主工作区之外）；
//	② worker 回合**真的在自己的现场里干活**（文件先出现在现场里）；
//	③ 收尾把改动**合并回主工作区**（主工作区拿到文件与那条提交）；
//	④ 析构销项按 teammate 的策略：收尾 / 验收都**不拆**现场与会话，回收唯一入口 = 整队收口。
func TestTeamworkWorktreeLifecycleChain(t *testing.T) {
	if testing.Short() {
		t.Skip("真实装配 + 真 git，short 模式跳过")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const marker = "TEAMMATE-ACCEPTANCE-MARKER"
	const fileName = "teammate-acceptance.txt"
	const itemID = "wi-file"
	const role = "exec"
	// 现场目录/分支名的口径 = `filepath.Base(root) + "-seelex-" + nodeID`（唯一一份
	// 命名实现 `sceneDirName`），Work Item 口径的 nodeID = `<role>-<itemID>`。
	sceneNodeID := role + "-" + itemID

	provider := &teamworkSmokeProvider{
		leaderCalls: []scriptedResponse{
			// ① 计划：一名在编 + 一道里程碑（排活要落在开着的里程碑上）。
			{toolName: "team_plan", toolArgs: `{"team_id":"wt-team","members":[{"role":"exec","role_session_id":"wt-exec","tools_policy":"readwrite"}],"milestones":[{"id":"m-wt","name":"现场"}]}`},
			{toolName: "team_work", toolArgs: `{"milestone":"m-wt","items":[{"id":"` + itemID + `","role":"` + role + `","name":"写文件","goal":"在仓库里创建 teammate-acceptance.txt 并提交"}]}`},
			// ① 派发（受理回执即返回，不等这一轮跑完）。
			{toolName: "team_dispatch", toolArgs: `{"item":"` + itemID + `","goal":"创建 teammate-acceptance.txt 并提交"}`},
			{text: "leader：已派发，等回执"},
			// ④ 验收：**不是**回收点（现场与会话归 team 托管）。
			{toolName: "team_accept", toolArgs: `{"id":"` + itemID + `"}`},
			{text: "leader：已验收"},
			// ④ 整队收口 = 唯一回收点。
			{toolName: "team_close"},
			{text: "leader：已收口"},
		},
		// worker 回合**真的调工具**：每一条是独立的一轮补全（工具调用 → 结果 → 下一轮），
		// 全部落在同一次 worker 回合里。
		//
		// 写文件用 `write_file` 而不是 `echo ... > file`：本机 shell 由 `scopedBashCommand`
		// 择一（这里落到 PowerShell），`>` 重定向写出的是 **UTF-16LE + BOM**，断言"内容对得上"
		// 就会被编码而不是被语义卡住（首跑的红正是这个：文件确实合回了主工作区，只是
		// 内容是 `\xff\xfeT\x00E\x00A\x00…`）。工具面写文件是确定的 UTF-8。
		workerCalls: []scriptedResponse{
			{toolName: "write_file", toolArgs: `{"path":"` + fileName + `","content":"` + marker + `\n"}`},
			{toolName: "bash", toolArgs: `{"command":"git add -A"}`},
			{toolName: "bash", toolArgs: `{"command":"git commit -m teammate-acceptance"}`},
			{text: "worker[exec]：文件已创建并提交。"},
		},
	}
	smoke := newTeamworkSmoke(t, provider)
	// 会话记录端口由测试基座按组合根装好（`newFullChainHarnessWithLimits`，见那里的注释）：
	// 缺它时 `teamUnitLedger()==nil`，teammate 的单元记录一律不落盘——"收尾之后记录还在"
	// 这条断言就变成空集上的恒真。这里读的就是基座装好的那一份。
	root := initSmokeGitRepo(t)
	if err := smoke.app.CreateWorkspace("wt-project", root, ""); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	probe := &sceneProbe{
		dir:      filepath.Join(filepath.Dir(root), filepath.Base(root)+"-seelex-"+sceneNodeID),
		file:     fileName,
		mainFile: filepath.Join(root, fileName),
	}
	stop := make(chan struct{})
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				probe.record()
			}
		}
	}()

	// ── ① 派发 ────────────────────────────────────────────────────────
	if err := smoke.app.Submit(ctx, "开一个团队，排一件事，让 exec 在仓库里创建 teammate-acceptance.txt 并提交，然后派发它"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	sessionID := smoke.app.Snapshot().Session.ID
	dispatchReceipt := smoke.toolResultOf("team_dispatch")
	if !strings.Contains(dispatchReceipt, `"item":"`+itemID+`"`) {
		t.Fatalf("① 派发受理回执不对：%q\n会话逐行：\n%s", dispatchReceipt, smoke.conversationDump())
	}
	var receipt struct {
		Handle string `json:"handle"`
	}
	if index := strings.Index(dispatchReceipt, `"handle":"`); index >= 0 {
		rest := dispatchReceipt[index+len(`"handle":"`):]
		receipt.Handle = rest[:strings.Index(rest, `"`)]
	}
	job := smoke.waitJobTerminal(t, receipt.Handle)
	t.Logf("① 派发：handle=%s state=%s exit=%d 归属=%s/%s", job.Handle, job.State, job.ExitCode, job.Role, job.WorkItem)
	if job.State != dto.AsyncStateDone {
		t.Fatalf("① worker 作业必须以 done 收场，得到 %s（summary=%q）", job.State, job.Summary)
	}

	// 等到这件事收尾（尾插把它推到待验收）。
	smoke.waitBoard(t, sessionID, func(board *dto.TeamworkBoardView) bool {
		for _, item := range board.WorkItems {
			if item.ID == itemID && item.Status == "review" {
				return true
			}
		}
		return false
	})

	// ── ② 现场上有工作 ────────────────────────────────────────────────
	dirSeen, fileInScene, _, together, samples := probe.snapshot()
	t.Logf("② 现场取证：目录见过=%v 现场里有文件=%v 采样=%d 场景=%s", dirSeen, fileInScene, samples, probe.dir)
	if !dirSeen {
		t.Fatalf("② 派发必须建出现场（真 git worktree），实际从未见到目录 %s", probe.dir)
	}
	if !together {
		t.Fatalf("② worker 的产出必须落在**自己的现场**里（现场目录存在期间从未见到 %s）", fileName)
	}

	// ── ③ 合并回主工作区 ─────────────────────────────────────────────
	content, err := os.ReadFile(filepath.Join(root, fileName))
	if err != nil {
		// 还没合回来时，先把现场事实吐出来（判断是"没合"还是"合到别处去了"）。
		t.Fatalf("③ 主工作区必须拿到 teammate 的产出 %s（收尾没有合并回主工作区）：%v\n现场目录还在=%v",
			fileName, err, dirExists(probe.dir))
	}
	if !strings.Contains(string(content), marker) {
		t.Fatalf("③ 主工作区 %s 的内容不对：%q（期望含 %s）", fileName, string(content), marker)
	}
	logOut, logErr := runGitOutput(root, "log", "--oneline", "-3")
	if logErr != nil {
		t.Fatalf("git log: %v", logErr)
	}
	t.Logf("③ 主工作区 git log：\n%s", logOut)
	if !strings.Contains(logOut, "teammate-acceptance") {
		t.Fatalf("③ 主工作区分支上应能看到 teammate 那条提交，实际：%q", logOut)
	}

	// ── ④ 析构销项：teammate 的策略（收尾不拆会话，回收唯一入口 = 整队收口）──
	key, ok := smoke.keyFor(sessionID)
	if !ok {
		t.Fatal("④ 拿不到会话作用域键（KeyFor 无解）")
	}
	nodeStore := sessionstore.NewNodeSessionStore(smoke.harness.store)
	records, err := nodeStore.List(key.ProjectID, key.SessionID)
	if err != nil {
		t.Fatalf("④ 读单元记录：%v", err)
	}
	found := false
	for _, record := range records {
		if record.NodeID == sceneNodeID {
			found = true
		}
	}
	if !found {
		t.Fatalf("④ 收尾之后 teammate 的单元记录必须还在（AtTeamClose：回收唯一入口 = team_close）\n实际记录：%+v", records)
	}
	itemSession := ""
	if board := smoke.waitBoard(t, sessionID, func(*dto.TeamworkBoardView) bool { return true }); board != nil {
		for _, item := range board.WorkItems {
			if item.ID == itemID {
				itemSession = item.SessionID
			}
		}
	}
	if itemSession != "" {
		if live := smoke.harness.runtime.TeammateSessionLive(itemSession); !live.Running {
			t.Fatalf("④ 收尾之后这件事的会话必须还在（lead 还要审查/人工处置），实际 executing=false：%+v", live)
		}
		t.Logf("④ 收尾后：单元记录在册、这件事的会话在跑（%s）", itemSession)
	}

	// 验收：**不是**回收点。
	if err := smoke.app.Submit(ctx, "验收 "+itemID); err != nil {
		t.Fatalf("Submit(accept): %v", err)
	}
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle(accept): %v", err)
	}
	if accept := smoke.toolResultOf("team_accept"); !strings.Contains(accept, `"ok":true`) {
		t.Fatalf("④ 验收必须成功（幂等释放）：%q", accept)
	}
	records, err = nodeStore.List(key.ProjectID, key.SessionID)
	if err != nil {
		t.Fatalf("④ 验收后读单元记录：%v", err)
	}
	if !hasNodeRecord(records, sceneNodeID) {
		t.Fatalf("④ 验收不是回收点：单元记录不该在验收时被清掉\n实际记录：%+v", records)
	}

	// 整队收口 = 唯一回收点。
	if err := smoke.app.Submit(ctx, "收口整队"); err != nil {
		t.Fatalf("Submit(close): %v", err)
	}
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle(close): %v", err)
	}
	if closeReceipt := smoke.toolResultOf("team_close"); !strings.Contains(closeReceipt, `"ok":true`) {
		t.Fatalf("④ 整队收口必须成功：%q", closeReceipt)
	}
	records, err = nodeStore.List(key.ProjectID, key.SessionID)
	if err != nil {
		t.Fatalf("④ 收口后读单元记录：%v", err)
	}
	if hasNodeRecord(records, sceneNodeID) {
		t.Fatalf("④ 整队收口之后单元记录必须被清掉（它描述的是已拆掉的现场）\n实际记录：%+v", records)
	}
	if dirExists(probe.dir) {
		t.Fatalf("④ 整队收口之后现场目录必须不在：%s", probe.dir)
	}
	if branch := gitBranch(root, "seelex/"+sceneNodeID); branch != "" {
		t.Fatalf("④ 整队收口之后现场分支必须删掉，实际还剩：%s", branch)
	}
	t.Logf("④ 析构销项：收尾保留记录与会话 → 验收保留 → 整队收口才清（现场目录/分支/登记全清）")

	close(stop)
	<-probeDone
}

func hasNodeRecord(records []sessionstore.NodeSessionRecord, nodeID string) bool {
	for _, record := range records {
		if record.NodeID == nodeID {
			return true
		}
	}
	return false
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// runGitOutput 取 git 的 stdout（`runGit` 只回错误码，诊断面要看原文）。
func runGitOutput(root string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return strings.TrimSpace(string(output)), nil
}

// gitBranch 返回某个本地分支的原文（空串 = 分支不在）。
func gitBranch(root, branch string) string {
	out, err := runGitOutput(root, "branch", "--list", branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
