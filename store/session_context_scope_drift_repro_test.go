package store

import (
	"context"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── 错误复现：会话上下文的落盘键跟随「活跃写作用域」────────────────────────
//
// 现象（本机真实存储，2026-10-14）：
//
//	dist/seelex-gui-dev/.seelex/sessions-json/
//	  project-03b5b8d29af3f44b/            ← sha256("ws-1789103724798058600")（seelex 工作区）
//	    session-ff14a72e0e1dc437/          ← frame: event/ goal/ teamwork/ metadata/{board_goal,event,stack_goal}.json
//	    session-4c45c21a6faca4ce/          ← 只有 metadata/{guide.json,system.json}（stub）
//	    session-9e13fb392cb09f8c/          ← 只有 metadata/{guide.json,system.json}（stub）
//	    session-a6c9ac65f288acd8/          ← 只有 metadata/{guide.json,system.json}（stub）
//	  project-e3b0c44298fc1c14/            ← sha256("")＝默认项目（"未关联会话"）
//	    session-ff14a72e0e1dc437/          ← message/（1036 行）+ compaction-records + big_tool_result
//
// 同一个 session-<hash> 出现在两个项目目录里，于是目录枚举（左侧栏的数据源，按项目
// 分格）把它列出两次；而 project-03b5 里那三份只有 guide.json + system.json 的 stub，
// 正是"会话上下文的归属键用错了项目"留下的痕迹——system.json 是
// SessionContextStore.Persist → SaveSystemPromptWorkspace 的唯一产物，且
// ensureLayoutGuide 是目录里 guide.json 的唯一创建者（module_heads.go）。
//
// 根因（装配侧缺口 + 本层兜底）：wiring/adapters.SessionPort.AttachSessionContext
// 把入参 workspaceID 整条丢掉、也不注入 resolver，于是 SessionContextStore.workspace()
// 回退 router.Workspace()——**进程级活跃写作用域**，即"当前视图所在项目"。为后台会话
// （会话 A 在视图里、会话 B 在后台跑）写 system prompt / 压缩帧时，数据就落进了当前
// 视图项目；视图切走再切回来，同一会话就多一个目录。
//
// 本用例钉住两层契约：
//
//	① 没有会话绑定时，落盘键只能是默认项目（未关联会话）——**绝不是**活跃写作用域。
//	   （旧实现在这一刻把数据写进了视图项目：本用例因此曾经是红的。）
//	② 装配注入绑定解析器后，落盘键是会话自己的项目，与当前视图无关。
func TestSessionContextStoreNeverFollowsActiveWriteScope(t *testing.T) {
	router := newTestRouter(t)
	const ownerProject = "ws-owner" // 会话真正归属的项目（绑定）
	const viewProject = "ws-view"   // 视图当前项目 = Router 活跃写作用域
	const sessionID = "sess-context-scope-drift"

	// 会话的数据/索引在它自己的项目里（目录枚举面存在 = 这是一个有数据的会话）。
	if err := router.SaveCommitWorkspace(ownerProject, sessionID, Commit{
		Events: []Event{{Seq: 1, Role: "user", Content: "owner-side"}},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	write := func(store *SessionContextStore) {
		t.Helper()
		if err := store.Load(context.Background()); err != nil {
			t.Fatalf("load context: %v", err)
		}
		// system prompt（Persist）与压缩帧（PushCompact，压缩之后）走同一把键
		// s.workspace()。
		if err := store.SetSystemPrompt("会话级提示：永不压缩"); err != nil {
			t.Fatalf("set system prompt: %v", err)
		}
		if err := store.PushCompact(CompactFrame{
			SegmentID: "seg-1", From: 0, To: 4, Summary: "compressed rounds", CompressedAt: time.Now(),
		}); err != nil {
			t.Fatalf("push compact frame: %v", err)
		}
	}

	// 用户在视图里切到了另一个项目：活跃写作用域随之改变
	// （service.setWorkspaceWriteScope → Router.SetWorkspace）。会话自己的归属没有变。
	router.SetWorkspace(viewProject)

	root := routerConfigPath(t, router)
	viewDir := filepath.Join(root, "project-"+storeroot.Hash(viewProject), "session-"+storeroot.Hash(sessionID))

	// ① 装配漏注 resolver（事故现场就是这条）：落盘键 = 默认项目（未关联会话）。
	unbound := NewSessionContextStore(router, sessionID)
	write(unbound)
	if pathExists(viewDir) {
		t.Fatalf("会话 %q 的上下文落进了视图项目 %q：\n  %s\n"+
			"同一会话因此在两个项目目录里各留一份，目录枚举按项目分格 ⇒ 前端把它显示成两个会话（没有 fork 指令）。\n"+
			"活跃写作用域是**视图**状态，不是会话归属：无绑定时会话上下文只能按未关联会话（默认项目）落盘。",
			sessionID, viewProject, viewDir)
	}
	if !pathExists(filepath.Join(root, "project-"+storeroot.Hash(""), "session-"+storeroot.Hash(sessionID), "metadata", "system.json")) {
		t.Fatalf("无绑定会话 %q 的上下文既没落进视图项目、也没落进默认项目（%q）——落盘键丢了。",
			sessionID, filepath.Join(root, "project-"+storeroot.Hash("")))
	}

	// ② 装配注入绑定解析器（wiring/adapters.SessionPort.AttachSessionContext 的
	//    修复形态）：落盘键是会话自己的项目，即便视图在别的项目。
	bound := NewSessionContextStore(router, sessionID)
	bound.SetWorkspaceResolver(func() string { return ownerProject })
	write(bound)
	ownerDir := filepath.Join(root, "project-"+storeroot.Hash(ownerProject), "session-"+storeroot.Hash(sessionID))
	if !pathExists(filepath.Join(ownerDir, "metadata", "system.json")) {
		t.Fatalf("会话自己的项目 %q 里没有 system prompt 落盘（%s）", ownerProject, ownerDir)
	}
}

// pathExists 报告路径是否存在（文件或目录）。
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
