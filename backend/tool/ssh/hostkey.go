package ssh

// hostkey.go —— 主机指纹：判定一律经过 knownhosts 包，禁止文本匹配。
//
// 为什么强调这一句：known_hosts 的条目可能是 `|1|salt|hash` 哈希形态，用
// grep/Select-String 搜 IP 会得到**假阴性**——"搜不到"被读成"主机不在
// known_hosts"，而连接其实是通的。存在性判断只能靠解析器（docs/arch/ssh-tool.md §6）。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// recordedKey 是握手期观测到的主机公钥（探针展示与显式信任都要用）。
type recordedKey struct {
	key         xssh.PublicKey
	fingerprint string
}

// hostKeyVerifier 把 knownhosts 的判定包一层，顺便记录"这次握手看到的公钥"。
//
// 为什么记录：首次连接的信任流程要求**人确认的指纹与本次握手的指纹逐字相等**，
// 否则会出现"确认的是 A 机器、写进去的是 B 机器"。指纹不落共享字段以外的任何
// 地方（无秘密：公钥本就是公开的）。
type hostKeyVerifier struct {
	mu   sync.Mutex
	seen recordedKey
	last error
	cb   xssh.HostKeyCallback
}

// callback 是交给 ClientConfig.HostKeyCallback 的函数。
func (v *hostKeyVerifier) callback(hostname string, remote net.Addr, key xssh.PublicKey) error {
	if key != nil {
		v.mu.Lock()
		v.seen = recordedKey{key: key, fingerprint: xssh.FingerprintSHA256(key)}
		v.mu.Unlock()
	}
	var err error
	if v.cb != nil {
		err = v.cb(hostname, remote, key)
	}
	v.mu.Lock()
	v.last = err
	v.mu.Unlock()
	return err
}

// observed 返回本次握手看到的主机公钥。
func (v *hostKeyVerifier) observed() recordedKey {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.seen
}

// knownHostsFiles 解析生效的指纹库文件清单（只保留**存在**的）。
//
// 一个都不存在时不报错：空指纹库的语义是"任何主机都未知"，握手会得到
// KeyError{Want:空}，探针据此报 host_unknown 并附指纹——比"文件不存在"这种
// 与安全无关的报错更能说明下一步该做什么。
func knownHostsFiles(spec KnownHostsSpec) []string {
	candidates := spec.Files
	if len(candidates) == 0 {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			candidates = []string{filepath.Join(home, ".ssh", "known_hosts")}
		}
	}
	files := make([]string, 0, len(candidates))
	for _, f := range candidates {
		if f == "" {
			continue
		}
		if _, err := os.Stat(f); err == nil {
			files = append(files, f)
		}
	}
	return files
}

// newHostKeyVerifier 构造指纹校验器。
func newHostKeyVerifier(spec Spec) (*hostKeyVerifier, error) {
	if mode := spec.KnownHosts.Mode; mode != "" && mode != KnownHostsStrict {
		// 只有 strict 一种连接期策略：TOFU/不校验都不提供（不校验 = InsecureIgnoreHostKey，
		// 库自己的注释就写着不该在 production 用）。
		return nil, codeErr("SSH_KNOWN_HOSTS_MODE", "unknown known_hosts mode %q (only %q)", mode, KnownHostsStrict)
	}
	cb, err := knownhosts.New(knownHostsFiles(spec.KnownHosts)...)
	if err != nil {
		return nil, fmt.Errorf("ssh: load known_hosts: %w", err)
	}
	return &hostKeyVerifier{cb: cb}, nil
}

// Trust 用**已确认的指纹**向指纹库追加一条记录。
//
// 纪律（docs/arch/ssh-tool.md §6.3）：
//   - wantFingerprint 必填，且必须与 key 的 SHA256 逐字相等，否则硬失败且**不写盘**；
//   - 只追加一行，不重写整份文件；行尾只补 `\n`，不做任何换行归一。
func Trust(spec Spec, key xssh.PublicKey, wantFingerprint string) (path string, line string, err error) {
	if key == nil {
		return "", "", codeErr(CodeFingerprintMismatch, "no host key observed in this handshake")
	}
	observed := xssh.FingerprintSHA256(key)
	if wantFingerprint == "" {
		return "", "", codeErr(CodeFingerprintMismatch,
			"refusing to trust without an explicit fingerprint; observed %s", observed)
	}
	if wantFingerprint != observed {
		return "", "", codeErr(CodeFingerprintMismatch,
			"fingerprint mismatch: confirmed %q but this handshake presented %q", wantFingerprint, observed)
	}

	path = ""
	if len(spec.KnownHosts.Files) > 0 {
		path = spec.KnownHosts.Files[0]
	}
	if path == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil || home == "" {
			return "", "", fmt.Errorf("ssh: resolve known_hosts path: %w", homeErr)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return "", "", fmt.Errorf("ssh: create known_hosts dir: %w", mkErr)
		}
	}

	line = knownhosts.Line([]string{spec.Addr()}, key)
	prefix := ""
	if needsNewlinePrefix(path) {
		prefix = "\n"
	}
	f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if openErr != nil {
		return "", "", fmt.Errorf("ssh: open known_hosts: %w", openErr)
	}
	defer f.Close()
	if _, writeErr := f.WriteString(prefix + line + "\n"); writeErr != nil {
		return "", "", fmt.Errorf("ssh: append known_hosts: %w", writeErr)
	}
	return path, line, nil
}

// needsNewlinePrefix 报告目标文件末字节是否缺换行（缺了才补，不缺就不动）。
func needsNewlinePrefix(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, statErr := f.Stat()
	if statErr != nil || info.Size() == 0 {
		return false
	}
	buf := make([]byte, 1)
	if _, readErr := f.ReadAt(buf, info.Size()-1); readErr != nil {
		return false
	}
	return buf[0] != '\n'
}

// TrustFromProbe 用一次探测观测到的公钥去信任主机。
//
// 这是首次连接的**唯一**入口：连接一定先失败（host_unknown），本函数把那次握手
// 观测到的公钥交给 Trust；wantFingerprint 必须是人确认过的值，且与观测结果逐字
// 相等，否则不写盘。
func TrustFromProbe(ctx context.Context, spec Spec, wantFingerprint string) (path, line, class string, err error) {
	client, trace, dialErr := dial(ctx, spec)
	if dialErr == nil {
		_ = client.Close()
		return "", "", ClassOK, codeErr(CodeFingerprintMismatch,
			"host %s is already trusted; nothing to write", spec.Addr())
	}
	var observed recordedKey
	if trace != nil && trace.Verifier != nil {
		observed = trace.Verifier.observed()
	}
	if observed.key == nil {
		gotClass, hint := classifyDial(trace, dialErr)
		return "", "", gotClass, codeErr(CodeFingerprintMismatch,
			"no host key was observed (%s): %s", gotClass, hint)
	}
	path, line, err = Trust(spec, observed.key, wantFingerprint)
	return path, line, ClassHostUnknown, err
}

// classifyHostKeyError 把指纹类错误翻成分类（非指纹类返回 false）。
func classifyHostKeyError(err error) (class, hint string, ok bool) {
	var revoked *knownhosts.RevokedError
	if errors.As(err, &revoked) {
		return ClassHostRevoked, "主机公钥在 known_hosts 里被标记为吊销（@revoked）；不提供绕过路径，请人工核查。", true
	}
	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		if len(keyErr.Want) == 0 {
			return ClassHostUnknown,
				"首次连接：该主机在 known_hosts 里没有条目。核对指纹后用显式信任写入（trust），再重连。", true
		}
		return ClassKeyMismatch,
			"主机指纹与 known_hosts 记录不符（换机或中间人）。硬失败：人工核对指纹后再轮换条目。", true
	}
	return "", "", false
}
