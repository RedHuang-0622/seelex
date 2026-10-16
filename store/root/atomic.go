package root

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// 本文件是基座的原子发布与身份动作（原 `store/store.go` 的 `writeAtomic` / `hash` /
// `randomID` / `removeAllWithBackoff`）。迁移只换包与导出名：实现逐字不变。

// RemoveAllWithBackoff 对目录删除做与 rename 发布同一量级的有界退避：
// Windows 上瞬时外部句柄（扫描器/索引器）会让 RemoveAll 报 “being used”。
func RemoveAllWithBackoff(path string) error {
	var err error
	for _, wait := range renameBackoff {
		if wait > 0 {
			time.Sleep(wait)
		}
		err = os.RemoveAll(path)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
	}
	return err
}

func Hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
func RandomID() string {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}

// renameBackoff 是原子发布 rename 的重试预算（累计 ≈77 ms）。
//
// Windows 下目标文件只要被任何句柄打开（实测为外部扫描器，非本进程读者），
// rename 立即返回 Access denied；实测持柄者松开耗时 0–18 ms，故短退避即可。
// 重试只能落在 rename 本身：数据 append 已经发生，提交级重试会重复追加。
var renameBackoff = []time.Duration{0, 2 * time.Millisecond, 5 * time.Millisecond,
	10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}

func WriteAtomic(path string, data []byte, permission os.FileMode) error {
	temp := path + "." + RandomID() + ".tmp"
	if err := os.WriteFile(temp, data, permission); err != nil {
		return err
	}
	var lastErr error
	for _, wait := range renameBackoff {
		if wait > 0 {
			time.Sleep(wait)
		}
		if err := os.Rename(temp, path); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	_ = os.Remove(temp)
	return lastErr
}
