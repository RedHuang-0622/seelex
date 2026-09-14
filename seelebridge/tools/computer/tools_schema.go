package computer

// ── 工具 JSON Schema ─────────────────────────────────────────
// 与工具描述一样是模型可见契约：参数名、单位与坐标语义都必须写清，避免模型
// 把逻辑像素当物理像素、把秒当毫秒。

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func integerSchema(description string, minimum, maximum int) map[string]any {
	return map[string]any{
		"type": "integer", "description": description,
		"minimum": minimum, "maximum": maximum,
	}
}

func pointProperties() map[string]any {
	return map[string]any{
		"x": map[string]any{"type": "integer", "description": "虚拟桌面物理像素 X"},
		"y": map[string]any{"type": "integer", "description": "虚拟桌面物理像素 Y"},
	}
}

func regionSchema() map[string]any {
	return objectSchema(map[string]any{
		"x":      map[string]any{"type": "integer", "description": "区域左上角 X（缺省 0）"},
		"y":      map[string]any{"type": "integer", "description": "区域左上角 Y（缺省 0）"},
		"width":  map[string]any{"type": "integer", "description": "区域宽度（像素，必须为正）"},
		"height": map[string]any{"type": "integer", "description": "区域高度（像素，必须为正）"},
	}, "width", "height")
}

func screenshotSchema() map[string]any {
	return objectSchema(map[string]any{
		"region":    regionSchema(),
		"max_width": integerSchema("给模型看的最大宽度（像素）；超过则等比降采样。缺省 1600，范围 320-4096。坐标不受缩放影响", minScreenshotWidth, maxScreenshotWidth),
	})
}

func windowsSchema() map[string]any {
	return objectSchema(map[string]any{
		"match":          map[string]any{"type": "string", "description": "按标题子串过滤（大小写不敏感）；缺省列出全部可见窗口"},
		"limit":          integerSchema("返回的窗口行数上限", 1, maxListedWindows),
		"include_hidden": map[string]any{"type": "boolean", "description": "是否包含不可见/最小化的顶层窗口（缺省 false：只看可见窗口）"},
	})
}

func focusSchema() map[string]any {
	return objectSchema(map[string]any{
		"match": map[string]any{"type": "string", "description": "目标窗口标题子串（大小写不敏感）；空串只报告当前前台窗口"},
	})
}

func clickSchema() map[string]any {
	properties := pointProperties()
	properties["button"] = map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "description": "鼠标键，缺省 left"}
	properties["clicks"] = integerSchema("点击次数", 1, 3)
	properties["interval_ms"] = integerSchema("多次点击之间的间隔（毫秒）", 0, 2000)
	properties["window"] = map[string]any{"type": "string", "description": "先按标题子串聚焦该窗口；只给 window 不给 x/y 时点击窗口中心"}
	return objectSchema(properties)
}

func moveSchema() map[string]any {
	return objectSchema(pointProperties(), "x", "y")
}

func dragSchema() map[string]any {
	return objectSchema(map[string]any{
		"from":        objectSchema(pointProperties(), "x", "y"),
		"to":          objectSchema(pointProperties(), "x", "y"),
		"duration_ms": integerSchema("按住拖拽的时长（毫秒），缺省 300", 0, 10000),
	}, "from", "to")
}

func scrollSchema() map[string]any {
	properties := pointProperties()
	properties["delta"] = integerSchema("滚轮增量：120 = 一格，正数向上（朝文档开头），负数向下", -maxScrollDelta, maxScrollDelta)
	return objectSchema(properties, "delta")
}

func typeSchema() map[string]any {
	return objectSchema(map[string]any{
		"text": map[string]any{"type": "string", "description": "要键入的字面文本（支持中文与 emoji）；注入到当前焦点控件"},
	}, "text")
}

func keysSchema() map[string]any {
	return objectSchema(map[string]any{
		"keys":  map[string]any{"type": "string", "description": "组合键，例如 ctrl+shift+t、alt+tab、enter、f5"},
		"times": integerSchema("重复次数", 1, 10),
	}, "keys")
}

func waitSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"milliseconds": integerSchema("等待毫秒数", 0, maxWaitMilliseconds),
		},
		"required": []string{"milliseconds"},
	}
}
