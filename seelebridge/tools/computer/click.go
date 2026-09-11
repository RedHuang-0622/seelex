package computer

import "time"

// injectMouse 注入一次鼠标事件：flags 是平台鼠标事件标志，x/y 是已归一化的
// 绝对坐标，data 是滚轮增量等附加数据。返回错误表示这次注入没有生效。
type injectMouse func(flags uint32, x, y uintptr, data uint32) error

// runClickSequence 按 clicks 次「按下 + 释放」发送点击，并把释放与按下绑成
// 一对：按下失败时也要尽力补发一次释放。
//
// 这条约束来自一次真实事故：Windows 的 mouse_event 返回 void，调用方若把它
// 当失败判断，就会在按下之后直接返回——点击既报错，又把按钮留在按下状态
// （桌面进入拖拽、后续移动变成框选）。注入端因此必须能报告真实成功/失败，
// 且序列本身不允许出现「只有按下没有释放」的出口。
func runClickSequence(inject injectMouse, down, up uint32, x, y uintptr, clicks int, interval time.Duration, sleep func(time.Duration)) error {
	if clicks < 1 {
		clicks = 1
	}
	for index := 0; index < clicks; index++ {
		if err := inject(down, x, y, 0); err != nil {
			// 注入失败可能发生在事件已入队之后（例如计数不符但事件已生效），
			// 补一次释放是唯一能保证「不卡在按下」的兜底。
			_ = inject(up, x, y, 0)
			return err
		}
		if err := inject(up, x, y, 0); err != nil {
			return err
		}
		if index < clicks-1 {
			sleep(interval)
		}
	}
	return nil
}
