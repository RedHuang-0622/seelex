package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
)

type applicationEventMsg struct{ event application.Event }
type submitResultMsg struct{ err error }
type loadMoreMsg struct{ err error }
type tickMsg time.Time

func waitApplicationEvent(subscription application.Subscription) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-subscription.Events
		if !ok {
			return applicationEventMsg{}
		}
		return applicationEventMsg{event: event}
	}
}

func submitInput(app AppController, input string) tea.Cmd {
	return func() tea.Msg { return submitResultMsg{err: app.Submit(context.Background(), input)} }
}

func loadMoreHistory(app AppController, limit int) tea.Cmd {
	return func() tea.Msg { return loadMoreMsg{err: app.LoadMoreHistory(limit)} }
}

// tickEvery 每 d 间隔发送一次 tickMsg，用于刷新界面中的时间显示等。
func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}
