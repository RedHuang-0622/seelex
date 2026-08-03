package core

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

// WindowPolicy 滑动窗口轮数 N 的确定机制（plan.md §3.7.3）。
// 类型定义与决策公式归属 seelexctx（seelexctx/controller.go 消费同一接口），
// 本文件只保留类型别名与 yaml 配置加载，避免双份实现。
type WindowPolicy = seelexctx.WindowPolicy

// ProviderContextInfo 携带窗口决策的全部输入。
type ProviderContextInfo = seelexctx.ProviderContextInfo

// WindowConfig 是 seele.yaml 的 window 配置段。
type WindowConfig = seelexctx.WindowConfig

// DefaultWindowPolicy 是 provider 推导策略（clamp 公式）。
type DefaultWindowPolicy = seelexctx.DefaultWindowPolicy

// NewDefaultWindowPolicy 从 window 配置段构建策略。
func NewDefaultWindowPolicy(config WindowConfig) DefaultWindowPolicy {
	return seelexctx.NewDefaultWindowPolicy(config)
}

// DefaultWindowConfig 返回确认点 5 的既定默认值（ratio/min_rounds/max_rounds）。
func DefaultWindowConfig() WindowConfig {
	return seelexctx.DefaultWindowConfig()
}

// windowConfigFile is the yaml envelope containing the window section.
type windowConfigFile struct {
	Window WindowConfig `yaml:"window"`
}

// LoadWindowConfig 读取 seele.yaml 的 window 配置段（路径门控同款加载风格）。
// 文件不存在 → 零值配置（全部未配置，走默认）；解析失败显式报错。
func LoadWindowConfig(path string) (WindowConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WindowConfig{}, nil
		}
		return WindowConfig{}, fmt.Errorf("window policy: read config: %w", err)
	}
	var file windowConfigFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return WindowConfig{}, fmt.Errorf("window policy: parse config: %w", err)
	}
	return file.Window, nil
}
