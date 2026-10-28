package gui

// dependencies_test.go — GUI 装配面的测试替身（M3，宽门面退场）。
//
// 一个端口一个 fake：每个替身只声明该端口自己的 owner 方法名，并把观察值记到共享的
// 记录器（`fakeApplication`）上。这样端口形状一旦漂移（方法名/签名与 owner 不一致），
// 替身就不再满足端口，测试包直接编译失败——与生产装配表的"字段赋值即编译期结构断言"
// 是同一道门禁。
//
// 为什么不是"一个全能 fake 塞进 App"：那会让端口退化成一个可以随便塞东西的抽屉，
// 迁移完成的组又能悄悄回到宽门面上；逐端口填字段时，缺哪个替身是本文件一眼可见的。

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/application"
	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// frontendDependencies 把一条真实流水线的 owner 绑成 GUI 依赖表，供打穿真实实现
// 的用例使用。
//
// 为什么不复用 wiring/bootstrap 的那张表：生产装配表所在的包 import 了 gui，
// 内部测试包（package gui）再反向 import 它就成环。这里逐字段绑定的是同一份真实
// owner（不是替身、不是适配器），端口形状漂移时同样编译失败。
func frontendDependencies(service *assembly.Service) Dependencies {
	components := assembly.ComponentsFor(service)
	return Dependencies{
		Accounts:         components.Admin.Account,
		Switches:         components.Policy.Switch,
		RoleSettings:     components.Policy.Role,
		Completion:       components.Conversation.Completion,
		History:          components.Conversation.History,
		ViewSnapshot:     components.Session.Snapshot,
		ViewEvents:       components.Session.View,
		SessionCatalog:   components.Session.Query,
		Draft:            components.Session.DraftEntry,
		Resume:           components.Session.Resume,
		Fork:             components.Session.Fork,
		Delete:           components.Session.Delete,
		Archive:          components.Session.Archive,
		Meta:             components.Session.Meta,
		SessionSubmit:    components.Session.Submit,
		Submission:       components.Conversation.Submission,
		Turn:             components.Runtime.Lifecycle,
		Interaction:      components.Session.Interaction,
		Prompts:          components.Conversation.PromptStack,
		Perf:             components.Admin.Perf,
		MessageEdit:      components.Conversation.EditFork,
		Queue:            components.Conversation.Queue,
		Conversation:     components.Conversation.Unified,
		HistorySearch:    components.Conversation.HistorySearch,
		References:       components.Conversation.Reference,
		WorkspaceCatalog: components.Workspace.Projects,
		WorkspaceActions: components.Workspace.Switch,
		// Composer：图片附件面在引擎里（媒体域），测试流水线没有引擎实例，
		// 这里绑一个只记录调用的替身（真实链路见 media / bridge 的用例）。
		Composer:     &fakeComposerMedia{},
		Schedules:    components.Runtime.Scheduler,
		WorkItems:    components.Execution.WorkItem,
		Subagents:    components.Execution.Subagent,
		RoleSessions: components.Policy.RoleSessions,
		ToolRecords:  components.Execution.ToolRecords,
		SessionSSH:   components.Session.SSH,
	}
}

// deps 装配本记录器提供的全部端口。某个用例要驱动变体替身时，在自己的类型上
// 重新声明 deps() 覆盖对应字段（凭 Shadowing 生效，不是加一个"全能"字段）。
func (fake *fakeApplication) deps() Dependencies {
	return Dependencies{
		Accounts:         fake,
		Switches:         fake,
		RoleSettings:     &fakeRoleSettings{},
		Completion:       fake,
		History:          fake,
		ViewSnapshot:     fake,
		ViewEvents:       fakeViewEvents{recorder: fake},
		SessionCatalog:   fake,
		Draft:            fake,
		Resume:           fake,
		Fork:             fake,
		Delete:           fake,
		Archive:          fake,
		Meta:             fake,
		SessionSubmit:    fake,
		Submission:       fake,
		Turn:             fake,
		Interaction:      fake,
		Prompts:          fake,
		Perf:             fake,
		Queue:            fake,
		Conversation:     fake,
		HistorySearch:    fake,
		References:       fakeReferences{recorder: fake},
		WorkspaceCatalog: fake,
		WorkspaceActions: fake,
		Composer:         &fakeComposerMedia{recorder: fake},
		Schedules:        fake,
		WorkItems:        fake,
		Subagents:        fake,
		RoleSessions:     fake,
		ToolRecords:      fake,
		SessionSSH:       fake,
	}
}

// ── S1：账号与权限档位 ──────────────────────────────────────────────────

// fakeAccounts 是 Accounts 端口的替身（owner：admin.AccountApplication）。
type fakeAccounts struct{ recorder *fakeApplication }

func (fake fakeAccounts) AddAccount(ctx context.Context, draft application.AccountDraft) (application.AccountAddResult, error) {
	return fake.recorder.AddAccount(ctx, draft)
}

func (fake fakeAccounts) AccountDetails(name string) (application.AccountDetails, error) {
	return fake.recorder.AccountDetails(name)
}

func (fake fakeAccounts) UpdateAccount(update application.AccountUpdate) (application.AccountDetails, error) {
	return fake.recorder.UpdateAccount(update)
}

func (fake fakeAccounts) DeleteAccount(name string) (application.AccountDetails, error) {
	return fake.recorder.DeleteAccount(name)
}

func (fake fakeAccounts) AccountBalance(ctx context.Context, name string) (application.AccountBalance, error) {
	return fake.recorder.AccountBalance(ctx, name)
}

func (fake fakeAccounts) SelectAccount(ctx context.Context, name string) error {
	return fake.recorder.SelectAccount(ctx, name)
}

func (fake fakeAccounts) SetAccountWindow(ctx context.Context, name string, window int) (int, error) {
	return fake.recorder.SetAccountWindow(ctx, name, window)
}

// fakeSwitches 是 Switches 端口的替身（owner：policy.SwitchApplication）。
type fakeSwitches struct{ recorder *fakeApplication }

func (fake fakeSwitches) SetFullAccess(on bool) bool { return fake.recorder.SetFullAccess(on) }

func (fake fakeSwitches) SetPermissionTier(tier string) (string, error) {
	return fake.recorder.SetPermissionTier(tier)
}

func (fake fakeSwitches) SwitchEffort(level string) error {
	return fake.recorder.SwitchEffort(level)
}

func (fake fakeSwitches) SetReasoningEffort(effort string) error {
	return fake.recorder.SetReasoningEffort(effort)
}

func (fake fakeSwitches) SetLoopLimit(limit int) error { return fake.recorder.SetLoopLimit(limit) }

func (fake fakeSwitches) SetSessionReleaseWindow(window int) (int, error) {
	return fake.recorder.SetSessionReleaseWindow(window)
}

func (fake fakeSwitches) SessionReleaseWindow() int { return fake.recorder.SessionReleaseWindow() }

func (fake fakeSwitches) SwitchPlugin(ctx context.Context, name string) error {
	return fake.recorder.SwitchPlugin(ctx, name)
}

// fakeRoleSettings 是 RoleSettings 端口的替身（owner：policy.RoleApplication）。
//
// 它自带状态而不转发到记录器：权限面板的用例问的是"写进去之后读回来的是什么"，
// 那份状态属于角色设置面本身，与宽门面上记录的调用轨迹是两回事。
type fakeRoleSettings struct {
	account      string
	inherit      bool
	userSpace    []string
	defaultSpace []string
}

func (fake *fakeRoleSettings) RolePermissionVector(_, _ string) ([]string, error) {
	return []string{}, nil
}

func (fake *fakeRoleSettings) SetRolePermissionVector(_, _ string, _ []string) error { return nil }

func (fake *fakeRoleSettings) RoleDefaultVector(_, _ string) ([]string, bool, error) {
	return []string{}, false, nil
}

func (fake *fakeRoleSettings) SetRoleDefaultVector(_, _ string, _ []string) error { return nil }

func (fake *fakeRoleSettings) RoleAccount(_, _ string) (string, bool, error) {
	return fake.account, fake.account != "", nil
}

func (fake *fakeRoleSettings) SetRoleAccount(_, _, account string) error {
	fake.account = account
	return nil
}

func (fake *fakeRoleSettings) RoleInheritMainAgent(_, _ string) (bool, error) {
	return fake.inherit, nil
}

func (fake *fakeRoleSettings) SetRoleInheritMainAgent(_, _ string, inherit bool) error {
	fake.inherit = inherit
	return nil
}

func (fake *fakeRoleSettings) RoleSSHUserSpaces(_, _ string) ([]string, error) {
	return append([]string{}, fake.userSpace...), nil
}

func (fake *fakeRoleSettings) RoleDefaultSSHUserSpaces(_, _ string) ([]string, error) {
	return append([]string{}, fake.defaultSpace...), nil
}

func (fake *fakeRoleSettings) SetRoleSSHUserSpaces(_, _ string, paths []string) error {
	fake.defaultSpace = append([]string{}, paths...)
	fake.userSpace = append([]string{}, paths...)
	return nil
}

func (fake *fakeRoleSettings) SetRoleSessionSSHUserSpaces(_, _ string, paths []string) error {
	fake.userSpace = append([]string{}, paths...)
	return nil
}

// fakeCompletion 是 Completion 端口的替身（owner：input.Application）。
type fakeCompletion struct{ recorder *fakeApplication }

func (fake fakeCompletion) Suggestions(value string) []application.Suggestion {
	return fake.recorder.Suggestions(value)
}

// fakeHistory 是 History 端口的替身（owner：history.Application）。
type fakeHistory struct{ recorder *fakeApplication }

func (fake fakeHistory) LoadMoreHistory(limit int) error { return fake.recorder.LoadMoreHistory(limit) }

func (fake fakeHistory) LoadLatestHistory() error { return fake.recorder.LoadLatestHistory() }

// fakeReferences 把工具结果读面的无 ctx owner 形状接到共享记录器。
type fakeReferences struct{ recorder *fakeApplication }

func (fake fakeReferences) ToolResultContent(resultRef string, offset, limit int) (application.ToolResultPage, error) {
	return fake.recorder.ToolResultContent(context.Background(), resultRef, offset, limit)
}

// ── S3：视图快照与事件订阅 ──────────────────────────────────────────────

// fakeViewSnapshot 是 ViewSnapshot 端口的替身（owner：viewstate 的 SnapshotApplication）。
type fakeViewSnapshot struct{ recorder *fakeApplication }

func (fake fakeViewSnapshot) Snapshot() application.Snapshot { return fake.recorder.Snapshot() }

// fakeViewEvents 是 ViewEvents 端口的替身（owner：viewstate 的 Coordinator）。
//
// 基础记录器模拟"只有全量订阅"的宿主：会话级重放窗口被拒（owner 返回错误），
// 桥面因此走 SubscribeSession → Subscribe 的既有错误回退链。要验证会话订阅键
// 与重放窗口的用例在自己的变体上覆盖本端口（见 bridge_events_test.go、
// bridge_session_test.go 的 deps()），而不是在本替身里加开关。
type fakeViewEvents struct{ recorder *fakeApplication }

func (fake fakeViewEvents) Subscribe(buffer int) application.Subscription {
	return fake.recorder.Subscribe(buffer)
}

// SubscribeSession 转发到记录器的全量订阅：基础记录器不区分会话路由（路由判据
// 的用例在 owner 侧与变体替身里，本替身只保证桥面能拿到一条订阅）。
func (fake fakeViewEvents) SubscribeSession(_ string, buffer int) (application.Subscription, error) {
	return fake.recorder.Subscribe(buffer), nil
}

func (fake fakeViewEvents) SubscribeSessionWithReplay(string, int, int) (application.Subscription, error) {
	return application.Subscription{}, errors.New("view events: replay window is not supported")
}

// ── 输入框图片附件（owner：backend/tool/io/media 的 SessionMedia）──────────────

// fakeComposerMedia 是 ComposerMedia 端口的替身：只记录"桥面把什么交给了媒体面"。
// 落盘/入队/归属的判据属于媒体域自己的用例（backend/tool/io/media/user_image_test.go），
// 这里钉的是桥面这一段：参数不改写、base64 解码正确、错误原样冒出来。
type fakeComposerMedia struct {
	recorder  *fakeApplication
	attached  []dto.ComposerImageRequest
	openCalls []string
	// resolveCalls / readCalls 记录"提交解析"与"缩略图读回"两条只读调用
	// （入参是 `会话|...` 形态，断言归属没有被串成别的会话）。
	resolveCalls []string
	readCalls    []string
	attachErr    error
	openErr      error
	openPath     string
	resolveErr   error
	readErr      error
}

func (fake *fakeComposerMedia) AttachUserImage(_ context.Context, request dto.ComposerImageRequest) (dto.ComposerImage, error) {
	fake.attached = append(fake.attached, request)
	if fake.attachErr != nil {
		return dto.ComposerImage{}, fake.attachErr
	}
	return dto.ComposerImage{
		Ref: "media:stub", Name: request.Name, MimeType: request.MimeType,
		Bytes: len(request.Data), Width: request.Width, Height: request.Height,
	}, nil
}

func (fake *fakeComposerMedia) MediaFilePath(_ context.Context, sessionID, ref string) (string, error) {
	fake.openCalls = append(fake.openCalls, sessionID+"|"+ref)
	if fake.openErr != nil {
		return "", fake.openErr
	}
	return fake.openPath, nil
}

// ResolveUserImages 把 ref 桩成固定的展示元数据；resolveErr 用来钉"解析失败
// 不提交"这条判据。
func (fake *fakeComposerMedia) ResolveUserImages(_ context.Context, sessionID string, refs []string) ([]dto.ComposerImage, error) {
	fake.resolveCalls = append(fake.resolveCalls, sessionID+"|"+strings.Join(refs, ","))
	if fake.resolveErr != nil {
		return nil, fake.resolveErr
	}
	images := make([]dto.ComposerImage, 0, len(refs))
	for _, ref := range refs {
		images = append(images, dto.ComposerImage{Ref: ref, Name: ref + ".png", MimeType: "image/png", Bytes: 7})
	}
	return images, nil
}

// ReadUserImage 桩回缩略图字节（真实实现按会话媒体分区读盘）。
func (fake *fakeComposerMedia) ReadUserImage(_ context.Context, sessionID, ref string) (dto.ComposerImageData, error) {
	fake.readCalls = append(fake.readCalls, sessionID+"|"+ref)
	if fake.readErr != nil {
		return dto.ComposerImageData{}, fake.readErr
	}
	return dto.ComposerImageData{Ref: ref, MimeType: "image/png", Bytes: 7, DataBase64: "cG5nLWJ5dGVz"}, nil
}
