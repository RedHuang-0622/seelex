//go:build !windows

package workspace

// seal_other.go —— 非 Windows 平台的密文面（诚实版）。
//
// 这里**没有真正的加密**：值只是 base64 编码，文件权限 0600。为什么不做成"用本地
// 密钥 AES"：密钥只能放在同一块盘上，对"同用户可读"这个真实威胁模型毫无增益，
// 却会让人误以为已加密。因此选择把事实写清楚（前缀 `plain:` + 文档标注），
// 等接入内核钥匙串（Keychain / Secret Service）时替换本文件即可，调用方零改动。
//
// 这也是 docs/arch/ssh-tool.md §7.2 里 Unix 那一格的口径。

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// sealPrefixPlain 明确标注"只是编码，不是加密"。
const sealPrefixPlain = "plain:"

// sealSecret 把明文口令编成可落盘的串（非加密，见文件头）。
func sealSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	return sealPrefixPlain + base64.StdEncoding.EncodeToString([]byte(plain)), nil
}

// openSecret 解出明文。
func openSecret(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if !strings.HasPrefix(sealed, sealPrefixPlain) {
		return "", fmt.Errorf("workspace: unsupported secret format (want %q prefix)", sealPrefixPlain)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefixPlain))
	if err != nil {
		return "", fmt.Errorf("workspace: decode secret: %w", err)
	}
	return string(raw), nil
}
