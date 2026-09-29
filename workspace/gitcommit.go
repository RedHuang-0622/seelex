package workspace

// 提交详情与「提交内文件内容」读取（gitcommit.go）。
//
// 两层能力，与工作树/提交记录同级：
//
//  1. GitCommitDetail —— 一个提交改了哪些文件（只读元数据：状态、路径、重命名
//     原路径、±行数）。这是"提交记录点开某一条"的数据源：面板要能回答"这次提交
//     动了什么"，而提交列表只给 hash/作者/时间/标题。
//  2. GitCommitFileContent —— 某文件在该提交时的内容（受控字节读取）。数据源从
//     工作区磁盘换成 git 对象库，**可见性边界与工作区读取完全同一条**
//     （sanitizeWorkspaceRelPath：containment、忽略目录、敏感文件名）。
//
// 为什么两件事放一起：它们共用同一组收敛——路径基准（git 的路径以仓库根为基准，
// 面板以工作区根为基准，绑定的目录可能是仓库子目录）、hash 形状校验、超时与失败
// 文案。分开写就会漂移成两套。
//
// 参数与安全（三条一起才成立，缺一条都不是"固定 argv"）：
//   - hash 必须是十六进制形状（4~64 位）：`--output=` 之类会被 git 当选项解析，
//     把 argv 的"位置"当作信任边界是靠不住的；
//   - 修订参数后跟 `--end-of-options`：hash 之后的一切按修订名解释，不再当选项；
//   - 路径前用 `--` 隔开（且范围钉在 `.`）：工作区根可能是仓库子目录时，
//     `-C 子目录` 本身不限制范围（实测同 gitchanges）。
//
// 与 `--graph` 一样，这里不产字符画、不下发补丁：Diff/Patch 一律不出现在 DTO 里，
// 文件内容走 dto.FileContent（同一形状，前端同一套渲染分派）。

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/winhide"
)

const (
	// gitCommitTimeout 是提交详情/文件内容每条 git 命令的单次超时。
	gitCommitTimeout = 8 * time.Second
	// gitCommitDefaultLimit 是默认返回的改动文件条数（前端可传 limit 覆盖）。
	gitCommitDefaultLimit = 200
	// gitCommitMaxLimit 是单次查询的改动文件条数上限（防御性钳制）。
	gitCommitMaxLimit = 1000
	// gitCommitScanBudget 是解析记录预算：解析前先按预算截断并标记，防御病理
	// 输出（一次性重排整个仓库的提交在这个量级上仍能给出前若干条）。
	gitCommitScanBudget = 20000
)

// gitCommitHashPattern 是允许进入 argv 的修订形状：4~64 位十六进制。
// 只认 hash 不认 `HEAD`/`master~1` 之类——调用方（GUI）手里拿的是提交列表
// 下发的完整 hash，接受表达式只会把"能写什么"的口子开大。
var gitCommitHashPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// GitCommitDetail 返回某个提交改了哪些文件（含状态/行数统计）。
// limit <= 0 使用默认 200；超过 gitCommitMaxLimit 钳制。非 git 仓库 / 提交不
// 存在 / git 不可用时返回 Result.Error 描述（调用方仍可展示错误态）；hash 形状
// 非法是调用方传错参数，返回 Go error。
func (r *Repo) GitCommitDetail(root, hash string, limit int) (dto.GitCommitDetail, error) {
	rootAbs, err := resolveGitRoot(root, "git commit")
	if err != nil {
		return dto.GitCommitDetail{}, err
	}
	hash = strings.TrimSpace(hash)
	if !gitCommitHashPattern.MatchString(hash) {
		return dto.GitCommitDetail{}, fmt.Errorf("git commit: %q is not a commit hash", hash)
	}
	if limit <= 0 {
		limit = gitCommitDefaultLimit
	}
	if limit > gitCommitMaxLimit {
		limit = gitCommitMaxLimit
	}

	// 仓库顶层目录：工作区根可能是仓库子目录，路径需要按它剥前缀。
	prefix := workspacePathPrefix(gitTopLevel(rootAbs), rootAbs)

	// 头部事实单独一条查询：`--name-status`/`--numstat` 与 `--no-patch` 不能同时
	// 出现（git 直接报错），而清单查询又必须抑制头部（`--format=`）——两者合并不了，
	// 就各取所需。字段口径与提交列表共用 gitCommitPrettyFormat。
	headerOut, message := runGitRead([]string{
		"-C", rootAbs,
		"show",
		"--no-patch",
		"--date=format:%m-%d %H:%M",
		"--pretty=format:" + gitCommitPrettyFormat,
		"--end-of-options", hash,
	}, gitCommitTimeout, "git 提交详情查询超时")
	if message != "" {
		return dto.GitCommitDetail{Root: rootAbs, Error: message}, nil
	}
	header, ok := parseGitCommitFields(string(headerOut))
	if !ok {
		return dto.GitCommitDetail{Root: rootAbs, Error: "无法解析提交头部"}, nil
	}

	filesOut, message := runGitRead(gitCommitFilesArgv(rootAbs, "--name-status", hash), gitCommitTimeout, "git 提交详情查询超时")
	if message != "" {
		return dto.GitCommitDetail{Root: rootAbs, Error: message}, nil
	}
	statsOut, message := runGitRead(gitCommitFilesArgv(rootAbs, "--numstat", hash), gitCommitTimeout, "git 提交详情查询超时")
	if message != "" {
		return dto.GitCommitDetail{Root: rootAbs, Error: message}, nil
	}

	files, scanTruncated := parseGitNameStatus(filesOut, gitCommitScanBudget)
	stats, _ := parseGitNumstat(statsOut, gitCommitScanBudget)

	result := dto.GitCommitDetail{
		Hash:      header.Hash,
		ShortHash: header.ShortHash,
		Author:    header.Author,
		Date:      header.Date,
		Parents:   header.Parents,
		Subject:   header.Subject,
		Root:      rootAbs,
	}
	kept := make([]dto.GitCommitFileEntry, 0, len(files))
	for _, item := range files {
		kind := commitFileKind(item.status)
		if kind == "" {
			// 畸形/未知状态：不产生半条改动，也不计入总数。
			continue
		}
		path, ok := relativeToWorkspace(item.path, prefix)
		if !ok {
			result.Filtered++
			continue
		}
		oldPath := ""
		if item.oldPath != "" {
			old, okOld := relativeToWorkspace(item.oldPath, prefix)
			if !okOld {
				result.Filtered++
				continue
			}
			oldPath = old
		}
		if isSensitiveChangePath(path) || (oldPath != "" && isSensitiveChangePath(oldPath)) {
			result.Filtered++
			continue
		}
		entry := dto.GitCommitFileEntry{
			Path:    path,
			OldPath: oldPath,
			Kind:    kind,
			Status:  item.status,
			Letter:  item.status[:1],
		}
		// 行数统计按**仓库根基准的原路径**取（两条查询的 key 空间一致），
		// 取不到就如实留 0——不猜"应该是多少行"。
		if stat, ok := stats[item.path]; ok {
			entry.Additions = stat.additions
			entry.Deletions = stat.deletions
			entry.Binary = stat.binary
		}
		result.Total++
		kept = append(kept, entry)
	}
	if len(kept) > limit {
		kept = kept[:limit]
		result.Truncated = true
	}
	if scanTruncated {
		result.Truncated = true
	}
	result.Files = kept
	return result, nil
}

// GitCommitFileContent 读取某文件在某个提交时的内容（前 limit 字节）。
// 可见性边界与工作区读取同一条（sanitizeWorkspaceRelPath）；路径基准按仓库根
// 补齐（工作区根即仓库根时前缀为空）。limit <= 0 用默认上限，超过硬上限钳制。
// 读完的字节以 base64 原样带回（文本/二进制同一通道），截断如实标记。
//
// 读的是**对象库**：这个提交那一刻的那一份，工作区怎么改都不影响它；反过来它
// 也不会被写回工作区（这条通道只有读）。
func (r *Repo) GitCommitFileContent(root, hash, relPath string, limit int64) (dto.FileContent, error) {
	rootAbs, err := resolveGitRoot(root, "git commit file")
	if err != nil {
		return dto.FileContent{}, err
	}
	hash = strings.TrimSpace(hash)
	if !gitCommitHashPattern.MatchString(hash) {
		return dto.FileContent{}, fmt.Errorf("git commit file: %q is not a commit hash", hash)
	}
	rel, err := sanitizeWorkspaceRelPath(relPath)
	if err != nil {
		return dto.FileContent{}, err
	}
	if limit <= 0 {
		limit = filePreviewDefaultLimit
	}
	if limit > filePreviewHardLimit {
		limit = filePreviewHardLimit
	}

	prefix := workspacePathPrefix(gitTopLevel(rootAbs), rootAbs)
	revPath := hash + ":" + prefix + filepath.ToSlash(rel)

	// 对象类型先问清楚：`<rev>:<path>` 也能解析到目录（tree）与子模块
	// （gitlink → commit），把它们当"文件内容"渲染出来是在说谎。
	objectType, message := runGitRead([]string{
		"-C", rootAbs, "cat-file", "-t", "--end-of-options", revPath,
	}, gitCommitTimeout, "git 提交文件读取超时")
	if message != "" {
		// 路径在该提交里不存在（删除的文件、拼错的路径）走这里：git 的
		// "path '...' does not exist in '<hash>'" 原样上抛，便于定位。
		return dto.FileContent{}, fmt.Errorf("git commit file: %s", message)
	}
	if kind := strings.TrimSpace(string(objectType)); kind != "blob" {
		return dto.FileContent{}, fmt.Errorf("git commit file: %q 在该提交里不是普通文件（对象类型 %s）", filepath.ToSlash(rel), kind)
	}

	sizeOut, message := runGitRead([]string{
		"-C", rootAbs, "cat-file", "-s", "--end-of-options", revPath,
	}, gitCommitTimeout, "git 提交文件读取超时")
	if message != "" {
		return dto.FileContent{}, fmt.Errorf("git commit file: %s", message)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64)
	if err != nil || size < 0 {
		return dto.FileContent{}, fmt.Errorf("git commit file: 无法解析对象大小 %q", strings.TrimSpace(string(sizeOut)))
	}

	truncated := size > limit
	chunk, err := readGitBlob(rootAbs, revPath, limit, truncated)
	if err != nil {
		return dto.FileContent{}, err
	}
	return dto.FileContent{
		Name:      filepath.Base(rel),
		Path:      filepath.ToSlash(rel),
		Size:      size,
		Base64:    base64.StdEncoding.EncodeToString(chunk),
		Limit:     limit,
		Truncated: truncated,
		TextLike:  textLikeBytes(chunk),
	}, nil
}

// gitCommitFilesArgv 组一次"某提交的文件清单"查询 argv。mode 是 --name-status
// （状态与路径）或 --numstat（±行数），两次查询的其余参数必须逐字相同，否则
// 两次结果对不上（顺序、重命名识别、范围都会变）。
func gitCommitFilesArgv(rootAbs, mode, hash string) []string {
	return []string{
		"-C", rootAbs,
		"show",
		// 抑制头部：清单查询不需要 commit/Author/Date 那几行，头部由上面的
		// 单独查询给（与 --no-patch 不能同时用）。
		"--format=",
		mode,
		// -z：NUL 分隔且不做引号转义（含空格/中文的路径原样返回）。
		"-z",
		// 重命名要认出来，否则一次改名会读成"删除 + 新增"两条。
		"--find-renames",
		// 合并提交默认什么都不给：按首父给差异，与"这次合并把什么带进了主线"对齐。
		"--diff-merges=first-parent",
		"--end-of-options", hash,
		// 范围钉在工作区子树内（-C 子目录本身不限制范围）。
		"--", ".",
	}
}

// readGitBlob 从对象库读一个 blob，最多 limit 字节。截断时**早停**：读够就关读端，
// 不等 git 把整个对象写完——大文件预览的代价必须与"看多少"成正比。
// expectTruncated 为真时，git 因管道破裂而退出非零不算失败（那是我们主动关的）；
// 非截断路径上任何失败都是真失败（错误文案取自 stderr）。
func readGitBlob(rootAbs, revPath string, limit int64, expectTruncated bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", rootAbs, "show", "--end-of-options", revPath)
	winhide.Apply(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git commit file: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git commit file: %w", err)
	}
	chunk, readErr := io.ReadAll(io.LimitReader(stdout, limit))
	if expectTruncated {
		_ = stdout.Close()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("git commit file: 读取对象失败: %w", readErr)
	}
	if waitErr != nil && !expectTruncated {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = waitErr.Error()
		}
		if ctx.Err() != nil {
			message = "git 提交文件读取超时"
		}
		return nil, fmt.Errorf("git commit file: %s", message)
	}
	return chunk, nil
}

// parsedCommitFile 是一条解析出来的提交内文件改动（尚未做路径归一化与过滤）。
type parsedCommitFile struct {
	status  string // git 名状态原样："M" / "A" / "D" / "T" / "R062" / "C100"
	oldPath string // 仅 R/C：原路径
	path    string // 新路径（非 R/C 即唯一路径）
}

// parseGitNameStatus 解析 `git show --format= --name-status -z` 的输出。
// 纯函数（无 IO），供单元测试直接喂入固定样例。
//
// 记录形态（NUL 分隔，-z 下路径不做引号转义；实测 git 2.51）：
//
//	"<status>"            名状态：M/A/D/T，重命名/复制带相似度分数（R062/C100）
//	"<path>"              非 R/C：紧跟一条路径
//	"<old>" "<new>"       R/C：紧跟两条路径，**原路径在前、新路径在后**
//
// 读法是**位置消费**，不拿"像不像状态"去猜：-z 下路径是裸字节，一个名叫 `M`
// 的文件与状态 M 在文本上无法区分（git 自己的格式就没打算让你区分）。因此畸形
// 记录的处理只做一件事——不让它带偏后续记录：R/C 缺任一条路径时，连它已经占用
// 的原路径记录一起跳过；缺路径的普通状态同样连带跳过；空记录直接忽略。git 不会
// 产出半条记录，这里的容忍只为畸形输入不产生"半条改动"。达到 maxFiles 停止并
// 置截断。
func parseGitNameStatus(output []byte, maxFiles int) ([]parsedCommitFile, bool) {
	if maxFiles <= 0 {
		maxFiles = gitCommitScanBudget
	}
	records := bytes.Split(output, []byte{0})
	files := make([]parsedCommitFile, 0, len(records)/2)
	truncated := false
	for index := 0; index < len(records); index++ {
		status := records[index]
		if len(status) == 0 {
			continue
		}
		if len(files) >= maxFiles {
			truncated = true
			break
		}
		pathIndex := index + 1
		if status[0] == 'R' || status[0] == 'C' {
			if pathIndex+1 >= len(records) || len(records[pathIndex]) == 0 || len(records[pathIndex+1]) == 0 {
				// 半条重命名：原路径记录已被这条状态占用（哪怕它是空的），
				// 一起跳过，免得下一轮把新路径当状态读。
				index = pathIndex
				continue
			}
			files = append(files, parsedCommitFile{
				status:  string(status),
				oldPath: string(records[pathIndex]),
				path:    string(records[pathIndex+1]),
			})
			index = pathIndex + 1
			continue
		}
		if pathIndex >= len(records) || len(records[pathIndex]) == 0 {
			index = pathIndex
			continue
		}
		files = append(files, parsedCommitFile{status: string(status), path: string(records[pathIndex])})
		index = pathIndex
	}
	return files, truncated
}

// parsedCommitStat 是一条 numstat 解析结果（±行数；二进制无行数）。
type parsedCommitStat struct {
	additions int
	deletions int
	binary    bool
}

// parseGitNumstat 解析 `git show --format= --numstat -z` 的输出，返回
// **仓库根基准的新路径 → 行数统计**（重命名按新路径登记，与 --name-status 对齐）。
//
// 记录形态（NUL 分隔；重命名的两个路径各自成一条记录）：
//
//	"<add>\t<del>\t<path>"
//	"<add>\t<del>\t"  "<old>"  "<new>"
//
// 二进制文件的 add/del 是 `-`（git 不给行数）→ binary=true、两个计数保持 0。
// 畸形记录（字段不足、计数不是数字且不是 `-`、路径为空）跳过——宁可这条没有
// 行数，也不臆造。
func parseGitNumstat(output []byte, maxFiles int) (map[string]parsedCommitStat, bool) {
	if maxFiles <= 0 {
		maxFiles = gitCommitScanBudget
	}
	records := bytes.Split(output, []byte{0})
	stats := make(map[string]parsedCommitStat)
	truncated := false
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) == 0 {
			continue
		}
		fields := bytes.Split(record, []byte{'\t'})
		if len(fields) < 3 {
			continue
		}
		stat, ok := parseNumstatCounts(fields[0], fields[1])
		if !ok {
			continue
		}
		// 路径里理论上也能有制表符：按余下字段重新拼回（重命名时路径字段为空，
		// 拼回后仍是空串 → 走下面的两条路径分支）。
		path := string(bytes.Join(fields[2:], []byte{'\t'}))
		if path == "" {
			if index+2 >= len(records) || len(records[index+1]) == 0 || len(records[index+2]) == 0 {
				continue
			}
			path = string(records[index+2])
			index += 2
		}
		if len(stats) >= maxFiles {
			truncated = true
			break
		}
		stats[path] = stat
	}
	return stats, truncated
}

// parseNumstatCounts 解析 numstat 的两个计数字段。`-` `-` 是二进制的约定
// 写法（git 数不了行数），其余必须是十进制非负整数。
func parseNumstatCounts(addRaw, delRaw []byte) (parsedCommitStat, bool) {
	if bytes.Equal(addRaw, []byte{'-'}) && bytes.Equal(delRaw, []byte{'-'}) {
		return parsedCommitStat{binary: true}, true
	}
	additions, err := strconv.Atoi(string(addRaw))
	if err != nil || additions < 0 {
		return parsedCommitStat{}, false
	}
	deletions, err := strconv.Atoi(string(delRaw))
	if err != nil || deletions < 0 {
		return parsedCommitStat{}, false
	}
	return parsedCommitStat{additions: additions, deletions: deletions}, true
}

// commitFileKind 把 git 名状态的首字母解释成展示分类。字母表与 porcelain 的
// XY 是同一张（M/A/D/R/C/T/U），所以复用 statusKind——git 的差别语义只有
// 一处解释，前端不重推。
func commitFileKind(status string) string {
	if status == "" {
		return ""
	}
	return statusKind(status[0])
}
