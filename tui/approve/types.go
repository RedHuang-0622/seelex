package approve

import "time"

type ChoiceOption struct {
	Key         string
	Label       string
	Description string
	Style       string
}

type Question struct {
	ID      string
	Content string
	Options []ChoiceOption
	Timeout time.Duration
}

func Choices(keys ...string) []ChoiceOption {
	builtin := map[string]ChoiceOption{
		"execute": {Key: "execute", Label: "执行", Description: "按计划执行", Style: "primary"},
		"skip":    {Key: "skip", Label: "跳过", Description: "跳过当前节点", Style: "secondary"},
		"abort":   {Key: "abort", Label: "终止", Description: "终止工作流", Style: "danger"},
		"confirm": {Key: "confirm", Label: "确认", Description: "确认并继续", Style: "primary"},
		"retry":   {Key: "retry", Label: "重试", Description: "重新执行", Style: "warning"},
	}
	result := make([]ChoiceOption, len(keys))
	for i, key := range keys {
		if option, ok := builtin[key]; ok {
			result[i] = option
		} else {
			result[i] = ChoiceOption{Key: key, Label: key}
		}
	}
	return result
}
