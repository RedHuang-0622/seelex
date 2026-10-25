package teamwork

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	// TeammateMessageDefaultWait 是 team_message 的默认有界等待。
	TeammateMessageDefaultWait = 30 * time.Second
	// TeammateMessageMaxWait 是 team_message 的硬上限；超时返回 job handle，
	// 不取消 teammate 的这一轮对话。
	TeammateMessageMaxWait = 120 * time.Second
)

// TeammateMessageRequest 是 team_message 解码后的请求。role + item 共同定位
// 唯一窗口：角色只是归属，真正持久化的 Session 是 WorkItem Session。
type TeammateMessageRequest struct {
	Role    string
	Item    string
	Message string
	Wait    time.Duration
}

// DecodeTeammateMessageRequest 解析并校验 team_message。message 是给 teammate
// 的正文，保留首尾空白；role/item 是标识，trim 后必填。缺失 item 不做角色级
// 私聊兜底。
func DecodeTeammateMessageRequest(argsJSON string) (TeammateMessageRequest, error) {
	var raw struct {
		Role    string `json:"role"`
		Item    string `json:"item"`
		Message string `json:"message"`
		WaitMS  int    `json:"wait_ms"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return TeammateMessageRequest{}, err
	}
	request := TeammateMessageRequest{
		Role: strings.TrimSpace(raw.Role), Item: strings.TrimSpace(raw.Item), Message: raw.Message,
	}
	switch {
	case request.Role == "":
		return TeammateMessageRequest{}, errors.New("team_message: role is required")
	case request.Item == "":
		return TeammateMessageRequest{}, errors.New("team_message: item is required（teammate 的对话窗口是 WorkItem Session，没有 item 就没有可对话窗口）")
	case strings.TrimSpace(request.Message) == "":
		return TeammateMessageRequest{}, errors.New("team_message: message is required")
	}
	request.Wait = time.Duration(raw.WaitMS) * time.Millisecond
	if request.Wait <= 0 {
		request.Wait = TeammateMessageDefaultWait
	}
	if request.Wait > TeammateMessageMaxWait {
		request.Wait = TeammateMessageMaxWait
	}
	return request, nil
}
