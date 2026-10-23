//go:build windows

package workspace

// seal_windows.go —— 口令密文用 **Windows DPAPI**（CryptProtectData）。
//
// 为什么是 DPAPI 而不是自己写 AES：密钥由操作系统按**当前用户**托管，我们不持有
// 任何密钥材料，也就不存在"密钥文件放在哪里"这个次生问题。代价是密文只能被同一
// 用户解开（换用户/换机读不到），这与"桌面单用户本地存储"的定位一致。
//
// 依赖零新增：绑定已在 vendor/golang.org/x/sys/windows（zsyscall_windows.go）。

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sealPrefixDPAPI 标记密文形态，为将来更换实现（钥匙串/DPAPI-NG）留出判据。
const sealPrefixDPAPI = "dpapi:"

// sealSecret 把明文口令封成可落盘的密文串。
func sealSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	in := []byte(plain)
	var inBlob, outBlob windows.DataBlob
	inBlob.Size = uint32(len(in))
	inBlob.Data = &in[0]
	if err := windows.CryptProtectData(&inBlob, nil, nil, 0, nil, 0, &outBlob); err != nil {
		return "", fmt.Errorf("workspace: dpapi protect: %w", err)
	}
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data))) }()
	sealed := unsafe.Slice(outBlob.Data, outBlob.Size)
	return sealPrefixDPAPI + base64.StdEncoding.EncodeToString(sealed), nil
}

// openSecret 解开密文串。
func openSecret(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if !strings.HasPrefix(sealed, sealPrefixDPAPI) {
		return "", fmt.Errorf("workspace: unsupported secret format (want %q prefix)", sealPrefixDPAPI)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefixDPAPI))
	if err != nil {
		return "", fmt.Errorf("workspace: decode secret: %w", err)
	}
	if len(raw) == 0 {
		return "", nil
	}
	var inBlob, outBlob windows.DataBlob
	inBlob.Size = uint32(len(raw))
	inBlob.Data = &raw[0]
	if err := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, 0, &outBlob); err != nil {
		return "", fmt.Errorf("workspace: dpapi unprotect (secret belongs to another user?): %w", err)
	}
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data))) }()
	return string(unsafe.Slice(outBlob.Data, outBlob.Size)), nil
}
