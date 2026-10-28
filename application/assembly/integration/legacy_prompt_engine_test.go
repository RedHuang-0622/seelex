package integration

import (
	"context"
	"sync"
	"time"
)

// completeToolForTest 走工具投影的完成段（真实完成路径），用于断言"完成即同步"。
func completeToolForTest(service *legacyTestService, name, id, result string, toolErr error, duration time.Duration) {
	service.components.conversationTurn.projection.Complete(
		context.Background(), name, id, "", result, toolErr, duration, nil,
	)
}

// recordingPromptEngine 记录 per-session prompt / 全局 prompt / maxLoops /
// clearHistory 调用，用于断言 effort 与 plugin 守卫（G0b）没有越过程序级
// 边界触达运行中的会话引擎。
type recordingPromptEngine struct {
	*multiSessionEngine
	mu                sync.Mutex
	promptFor         map[string]string
	globalPrompts     []string
	maxLoopsCalls     int
	clearHistoryCalls int
}

func newRecordingPromptEngine() *recordingPromptEngine {
	return &recordingPromptEngine{
		multiSessionEngine: newMultiSessionEngine(),
		promptFor:          map[string]string{},
	}
}

func (engine *recordingPromptEngine) SetSystemPromptFor(sessionID, prompt string) {
	engine.mu.Lock()
	engine.promptFor[sessionID] = prompt
	engine.mu.Unlock()
}

func (engine *recordingPromptEngine) SetSystemPrompt(prompt string) {
	engine.mu.Lock()
	engine.globalPrompts = append(engine.globalPrompts, prompt)
	engine.mu.Unlock()
}

func (engine *recordingPromptEngine) SetMaxLoops(loops int) {
	engine.mu.Lock()
	engine.maxLoopsCalls++
	engine.mu.Unlock()
}

func (engine *recordingPromptEngine) ClearHistory() {
	engine.mu.Lock()
	engine.clearHistoryCalls++
	engine.mu.Unlock()
}

// promptSnapshot 返回测试断言的引擎侧快照（加锁拷贝）。
func (engine *recordingPromptEngine) promptSnapshot() (promptFor map[string]string, globalCount, loops, clears int) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	promptFor = make(map[string]string, len(engine.promptFor))
	for id, prompt := range engine.promptFor {
		promptFor[id] = prompt
	}
	return promptFor, len(engine.globalPrompts), engine.maxLoopsCalls, engine.clearHistoryCalls
}
