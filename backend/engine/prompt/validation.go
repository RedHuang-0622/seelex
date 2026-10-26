package prompt_layer

import promptassets "github.com/RedHuang-0622/seelex/backend/engine/prompt/assets"

// ValidateAssets validates the resolved embedded-or-overridden prompt set.
// Application consumes this narrow Engine prompt face instead of importing the
// asset package directly.
func ValidateAssets() error {
	return promptassets.Validate()
}
