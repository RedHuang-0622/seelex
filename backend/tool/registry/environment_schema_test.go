package registry

import "testing"

func TestEnvironmentIsInjectedByAdapterNotExposedAsToolArgument(t *testing.T) {
	schemas := map[string]map[string]interface{}{
		ReadFileToolName:  readFileSchema(),
		WriteFileToolName: writeFileSchema(),
		EditFileToolName:  editFileSchema(),
		BashToolName:      bashSchema(),
		bgManageToolName:  jobManageSchema(),
		"bash_bg":         bashBgSchema(),
	}
	for name, schema := range schemas {
		properties, _ := schema["properties"].(map[string]interface{})
		if _, exposed := properties["env"]; exposed {
			t.Fatalf("%s 不得把 env 暴露给模型；环境由会话适配器注入", name)
		}
	}
}
