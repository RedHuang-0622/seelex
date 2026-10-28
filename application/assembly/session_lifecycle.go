package assembly

// 阶段 2：会话生命周期（design-model 1.2/1.4/1.5）。
// cold_load = resumeSession 未驻留分支；hot_attach = 驻留会话只换视图指针；
// unload = persist + 释放内存态（scope/引擎/任务运行时）。

// hotAttachSession 热加载：目标会话已驻留（引擎实例在内存，含运行中），
// 只换视图指针 + 投影会话 scope；不重建历史、不触碰 X/M/R（不变量 Ⅱ）。
// 运行中会话也允许回看（design-model 第 5 节决策：热加载支持运行中查看）。
func (service *Service) hotAttachSession(sessionID string) error {
	_, err := service.components.sessionHistoryRuntime.AttachSession(sessionID, &service.Core.Snapshot)
	return err
}

// UnloadSession 卸载会话：先持久化（非活跃时），再释放内存态（引擎实例、
// 任务运行时、会话 view scope）；registry 保留 COLD 元数据，重开走
// cold_load。运行中会话拒绝卸载。

// switchViewToFreshDraftLocked 把视图切到一个**就地新建**的早分配草稿单元（调用方
// 持有 Core.ViewMu）。用点：卸载/删除掉的恰是**当前视图**会话——会话单元已被完整释放
// 面移除，而视图单例必须始终有对应单元，否则后续所有 `Unit(viewID)` 读成 nil，而
// "卸载/删掉正在看的那个会话"是正常入口。
//
// 草稿不写槽位（不进入会话树，与卸载前语义一致），档位与任务投影在同一临界区里重算：
// 否则快照里留着的是刚被移除会话的档位/任务（与 BeginNewSession 同一口径）。
func (service *Service) switchViewToFreshDraftLocked() {
	draftID := service.newDraftSessionIDLocked()
	// 差异（`draftShellOptions` 的显式开关）：本路径只换单元、不碰工作区绑定，
	// 也不清窗口读数与标题（对齐"与卸载前语义一致"，见类型文档里的三条角色）；
	// 进程级交互保留，会话级交互属于被卸下的会话，随之失效。
	service.enterDraftShellLocked(draftShellOptions{
		sessionID:              draftID,
		keepWorkspace:          true,
		keepProcessInteraction: true,
		tier:                   func(sessionID string) { service.syncViewPermissionTierLocked(sessionID) },
	})
}
