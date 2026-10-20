package core

import (
	"errors"
	"log"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

// releaseSessionAsync 把"这个会话没有了"转告执行域：杀掉它名下所有在途后台命令。
// 与完整会话释放面同一时点、同一语义（会话这一段生命结束）。
func (service *Service) releaseSessionAsync(sessionID string) {
	if service == nil || service.Deps.Runtime == nil || sessionID == "" {
		return
	}
	service.Deps.Runtime.ReleaseSessionAsync(sessionID)
}

// DeleteSession 删除会话记录并走完整释放面（会话域用例，留在 application/core）。
func (service *Service) DeleteSession(sessionID string) error {
	// 会话粒度删除：项目绑定由存储层从会话 record 解析（旧 workspace
	// 粒度 DeleteWorkspace 口已删除）。
	if err := service.Deps.Sessions.Delete(sessionID); err != nil {
		return err
	}
	// 同一时点终止它名下还在跑的后台命令：句柄表按会话持有执行体，会话删了就没有
	// 任何入口能再取回或杀掉它们。
	service.releaseSessionAsync(sessionID)
	// 会话没了就不该在内存里留任何一格：走**完整释放面**（引擎实例 + 桥侧会话
	// 本体（框架 Session / 上下文存储）+ 可见正文 + 会话单元/标题/任务投影）。
	// 记录已删，所以不 flush（LRU 释放与回合收尾早已落盘）；释放失败不让删除失败
	// ——记录已经删了，谁也没有重试入口，重启即回收。
	//
	// 删除前先记下"删的是不是当前视图"：会话单元随完整释放面离场，而视图单例必须
	// 始终有对应单元，所以删的是视图会话时下面要就地切到一个新草稿。
	service.ViewMu.RLock()
	viewed := service.Core.Snapshot.Session.ID == sessionID
	service.ViewMu.RUnlock()
	if err := service.releaseSessionMemory(sessionID, releaseFull); err != nil {
		log.Printf("[session-release] delete %q: %v", sessionID, err)
	}
	if service.Deps.Workspace != nil {
		service.Deps.Workspace.UnbindSession(sessionID)
		workspaceProjection := service.components.workspace.Projection()
		service.ViewMu.Lock()
		if viewed {
			service.switchViewToFreshDraftLocked()
		}
		service.applyWorkspaceProjectionLocked(workspaceProjection)
		revision := service.bumpLocked()
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		service.components.sessions.RequestCatalogRefresh()
		return nil
	}
	if viewed {
		service.ViewMu.Lock()
		service.switchViewToFreshDraftLocked()
		revision := service.bumpLocked()
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		service.components.sessions.RequestCatalogRefresh()
	}
	return nil
}

// CreateWorkspace 建工作区并绑定到当前视图（实现：middle/switch/workspace）。
func (service *Service) CreateWorkspace(name, rootPath, gitRemote string) error {
	return service.components.workspace.CreateWorkspace(name, rootPath, gitRemote)
}

// CreateWorkspaces 批量登记项目，不绑定当前会话（GUI 新建项目 temp list 的提交口）。
// 创建成功后只刷新 workspace 目录投影并发布 snapshot.changed；当前会话、绑定与运行时根都不动。
func (service *Service) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	result, err := service.components.workspace.CreateWorkspaces(requests)
	if err != nil {
		return result, err
	}
	projection := service.components.workspace.Projection()
	service.ViewMu.Lock()
	service.applyWorkspaceProjectionLocked(projection)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
	return result, nil
}

// BindWorkspace 把工作区绑定到当前视图会话/草稿（实现：middle/switch/workspace）。
func (service *Service) BindWorkspace(workspaceID string) error {
	return service.components.workspace.BindWorkspace(workspaceID)
}

// UnbindWorkspace 解绑当前视图会话/草稿的工作区（实现：middle/switch/workspace）。
func (service *Service) UnbindWorkspace() {
	service.components.workspace.UnbindWorkspace()
}

// WorkspaceTree 列出当前工作区某目录的子条目（root 只来自后端当前 workspace）。
func (service *Service) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	return service.components.workspace.Tree(relPath, depth)
}

// WorkspaceFileCount 统计当前工作区文件/目录数。
func (service *Service) WorkspaceFileCount() (dto.TreeCount, error) {
	return service.components.workspace.FileCount()
}

// WorkspaceGitLog 返回**指定仓库**（repo 相对工作区根，"." = 工作区根）最近
// limit 条提交。repo 越界/绝对路径是调用方传错参数 → Go error。
func (service *Service) WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error) {
	port, root, err := service.workspaceRepoPort(repo)
	if err != nil {
		return dto.GitLogResult{}, err
	}
	treePort, ok := port.(contract.WorkspaceTreePort)
	if !ok {
		return dto.GitLogResult{}, errors.New("worktree: workspace backend does not support tree listing")
	}
	return treePort.GitLog(root, limit)
}

// WorkspaceChanges 返回**指定仓库**未提交的改动。
func (service *Service) WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error) {
	port, root, err := service.workspaceRepoPort(repo)
	if err != nil {
		return dto.WorkspaceChangesResult{}, err
	}
	treePort, ok := port.(contract.WorkspaceTreePort)
	if !ok {
		return dto.WorkspaceChangesResult{}, errors.New("worktree: workspace backend does not support tree listing")
	}
	return treePort.GitChanges(root, limit)
}

// WorkspaceGitCommitDetail 返回**指定仓库**某提交改了哪些文件。
func (service *Service) WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error) {
	port, root, err := service.workspaceRepoPort(repo)
	if err != nil {
		return dto.GitCommitDetail{}, err
	}
	treePort, ok := port.(contract.WorkspaceTreePort)
	if !ok {
		return dto.GitCommitDetail{}, errors.New("worktree: workspace backend does not support tree listing")
	}
	return treePort.GitCommitDetail(root, hash, limit)
}

// WorkspaceFileContent 读取当前工作区某文件的前 limit 字节。
func (service *Service) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error) {
	return service.components.workspace.FileContent(relPath, limit)
}

// WorkspaceGitCommitFileContent 读取某文件在**指定仓库**某个提交时的内容
// （git 对象库）。
func (service *Service) WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error) {
	port, root, err := service.workspaceRepoPort(repo)
	if err != nil {
		return dto.FileContent{}, err
	}
	filePort, ok := port.(contract.WorkspaceFilePort)
	if !ok {
		return dto.FileContent{}, errors.New("worktree: workspace backend does not support file preview")
	}
	return filePort.GitCommitFileContent(root, hash, relPath, limit)
}

// WorkspaceWriteFile 覆盖当前工作区某文件的全部内容（面板编辑保存）。
//
// 权限口径：这是用户自己在面板里点下的保存（用户即动作主体），不走主代理
// 权限档位；工具面（write_file/edit_file）的档位判定不受影响。
func (service *Service) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error) {
	return service.components.workspace.WriteFile(relPath, content)
}

// applyWorkspaceProjectionLocked 把 middle 产出的窄投影写进 Snapshot。
// 调用方必须持有 Core.ViewMu；投影 owner 是应用视图，本函数只做拷贝。
func (service *Service) applyWorkspaceProjectionLocked(projection switchworkspace.Projection) {
	service.Core.Snapshot.Workspaces = append([]model.WorkspaceInfo(nil), projection.Workspaces...)
	service.Core.Snapshot.SessionWorkspaces = make(map[string]string, len(projection.Bindings))
	for sessionID, workspaceID := range projection.Bindings {
		service.Core.Snapshot.SessionWorkspaces[sessionID] = workspaceID
	}
}
