package assemble

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	bridgecomputer "github.com/RedHuang-0622/seelex/backend/tool/computer"
	"github.com/RedHuang-0622/seelex/backend/tool/io/imageattach"
	toolmedia "github.com/RedHuang-0622/seelex/backend/tool/io/media"
)

func TestComputerUseEnabledParsing(t *testing.T) {
	for _, value := range []string{"0", "off", "OFF", "false", "no", "  off "} {
		t.Setenv(computerUseEnv, value)
		if computerUseEnabled() {
			t.Fatalf("%s=%q 应关闭 computer use", computerUseEnv, value)
		}
	}
	for _, value := range []string{"", "1", "on", "true", "yes"} {
		t.Setenv(computerUseEnv, value)
		if !computerUseEnabled() {
			t.Fatalf("%s=%q 应开启 computer use", computerUseEnv, value)
		}
	}
}

// TestRegisterBuiltinsRegistersComputerTools 验证正式接入点：支持桌面的平台
// 上 RegisterBuiltins 会挂上整个 computer use 工具族；SEELEX_COMPUTER_USE
// 关闭时不注册（宁可没有，也不挂一串必然失败的摆设）。
func TestRegisterBuiltinsRegistersComputerTools(t *testing.T) {
	if !bridgecomputer.Supported() {
		t.Skip("当前平台没有桌面 computer use 实现")
	}
	want := []string{
		bridgecomputer.ToolScreenshot, bridgecomputer.ToolWindows, bridgecomputer.ToolFocus,
		bridgecomputer.ToolClick, bridgecomputer.ToolMove, bridgecomputer.ToolDrag,
		bridgecomputer.ToolScroll, bridgecomputer.ToolType, bridgecomputer.ToolKeys,
		bridgecomputer.ToolWait,
	}

	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	registered := map[string]bool{}
	for _, tool := range runtime.AllTools() {
		registered[tool.Name] = true
	}
	for _, name := range want {
		if !registered[name] {
			t.Errorf("RegisterBuiltins 未注册 %q", name)
		}
	}

	t.Setenv(computerUseEnv, "off")
	disabled := newTestRuntime(t)
	defer disabled.Shutdown()
	disabled.RegisterBuiltins()
	for _, tool := range disabled.AllTools() {
		if tool.Name == bridgecomputer.ToolScreenshot {
			t.Fatalf("%s=off 时不应注册 computer use 工具", computerUseEnv)
		}
	}
}

type stubCompleter struct{}

func (stubCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	return types.Message{}, nil
}

// TestWrapImageAttachmentsWiresMediaLoader 验证装配位置与接线：装配链把
// `media.SessionMedia.Wrap` 的结果装回 `completer`，且随图包装上带着真实媒体分区
// 的 Loader（附件只带 `media:<hash>` 时也能把字节读回来）。媒体域自己的链式行为
// 由 `backend/tool/io/media` 的用例钉住，这里只钉"装配器接上了它"。
func TestWrapImageAttachmentsWiresMediaLoader(t *testing.T) {
	runtime := &Runtime{completer: stubCompleter{}, media: toolmedia.New(nil)}
	runtime.wrapImageAttachments()
	wrapper, ok := runtime.completer.(*imageattach.Wrapper)
	if !ok {
		t.Fatalf("completer 未被随图包装: %T", runtime.completer)
	}
	if wrapper.Loader == nil {
		t.Fatal("随图包装缺少媒体 Loader")
	}
}
