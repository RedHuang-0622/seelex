package bootseed

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// rawPack 造一份"逐字节 + 编码"混合的默认数据包（测试里的资源集，不碰真实 assets）。
func rawPack(t *testing.T) (Pack, fstest.MapFS, map[string][]byte) {
	t.Helper()
	plain := []byte("limits:\n  message_shard_size: 100\n")
	gbk := []byte{0xB1, 0xB1, 0xBE, 0xA9, 0x0A} // GBK "北京"：非 UTF-8，逐字节保真才谈得上可用
	files := fstest.MapFS{
		"assets/plain.yaml":     {Data: plain},
		"assets/payload.b64":    {Data: []byte(EncodeBase64(gbk) + "\n")},
		"assets/entry.yaml":     {Data: plain},
		"assets/skeleton.md":    {Data: []byte("# 骨架\n")},
		"assets/no-entry-notes": {Data: []byte("notes")},
	}
	pack := Pack{Name: "test", Files: []File{
		{Rel: "entry.yaml", Source: "assets/entry.yaml"},
		{Rel: "nested/city_list.json", Source: "assets/payload.b64", Encoding: EncodingBase64},
	}}
	return pack, files, map[string][]byte{
		"entry.yaml":            plain,
		"nested/city_list.json": gbk,
	}
}

func TestResolveHitReadsExistingAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "config", "seelex.yaml")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	user := []byte("limits:\n  message_shard_size: 42   # 用户自己改过的那份\n")
	if err := os.WriteFile(existing, user, 0o644); err != nil {
		t.Fatal(err)
	}
	seedRoot := filepath.Join(dir, "package", "config")
	pack, files, _ := rawPack(t)

	result, err := ResolveFS(Spec{
		Name: "config/seelex.yaml",
		Candidates: []string{
			filepath.Join(dir, "package", "config", "seelex.yaml"), // 包内那份：不存在
			existing, // 用户/CWD 那份：存在 → 它说了算
		},
		SeedRoot: seedRoot,
		Entry:    "entry.yaml",
		Pack:     pack,
	}, files)
	if err != nil {
		t.Fatalf("ResolveFS: %v", err)
	}
	if result.Kind != KindHit {
		t.Fatalf("存在即读：期望 Kind=%s，得到 %s", KindHit, result.Kind)
	}
	if result.Path != existing {
		t.Fatalf("命中的候选要如实返回：期望 %s，得到 %s", existing, result.Path)
	}
	if got, readErr := os.ReadFile(existing); readErr != nil || string(got) != string(user) {
		t.Fatalf("命中时不得改写用户那份：%q (err=%v)", got, readErr)
	}
	if _, statErr := os.Stat(seedRoot); !os.IsNotExist(statErr) {
		t.Fatalf("命中时不得落盘：SeedRoot 应不存在，stat err=%v", statErr)
	}
}

func TestResolveCandidatePriorityFirstWins(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a", "seelex.yaml")
	second := filepath.Join(dir, "b", "seelex.yaml")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pack, files, _ := rawPack(t)
	result, err := ResolveFS(Spec{Name: "seelex.yaml", Candidates: []string{first, second}, Pack: pack}, files)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != first {
		t.Fatalf("候选链是责任链：第一个存在的胜出，期望 %s，得到 %s", first, result.Path)
	}
}

// TestResolveMissingSeedsDefaultBytes 是本次改动的核心：候选链全缺时按内嵌默认
// 数据初始化，且写出的字节与默认数据**逐字节相同**（含非 UTF-8 载荷）。
func TestResolveMissingSeedsDefaultBytes(t *testing.T) {
	dir := t.TempDir()
	seedRoot := filepath.Join(dir, "package", "config")
	pack, files, want := rawPack(t)

	result, err := ResolveFS(Spec{
		Name:       "config/seelex.yaml",
		Candidates: []string{filepath.Join(dir, "package", "config", "seelex.yaml")},
		SeedRoot:   seedRoot,
		Entry:      "entry.yaml",
		Pack:       pack,
	}, files)
	if err != nil {
		t.Fatalf("ResolveFS: %v", err)
	}
	if result.Kind != KindSeeded {
		t.Fatalf("全缺时应初始化：期望 Kind=%s，得到 %s", KindSeeded, result.Kind)
	}
	if want := filepath.Join(seedRoot, "entry.yaml"); result.Path != want {
		t.Fatalf("落盘后入口路径：期望 %s，得到 %s", want, result.Path)
	}
	if len(result.Written) != len(pack.Files) {
		t.Fatalf("默认数据要逐文件写出：期望 %d 个，实际 %v", len(pack.Files), result.Written)
	}
	for rel, expected := range want {
		got, readErr := os.ReadFile(filepath.Join(seedRoot, filepath.FromSlash(rel)))
		if readErr != nil {
			t.Fatalf("读回初始化结果 %s: %v", rel, readErr)
		}
		if string(got) != string(expected) {
			t.Fatalf("初始化出来的 %s 必须与默认数据逐字节相同：得到 %q，期望 %q", rel, got, expected)
		}
	}
}

func TestResolveKeepsSeededThenNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	seedRoot := filepath.Join(dir, "package", "config")
	pack, files, _ := rawPack(t)
	// 入口永远由用户提供（骨架里没有它）：这就是"每次启动都会走落盘分支"的形状。
	spec := Spec{Name: "auto_get_jobs", Candidates: []string{filepath.Join(seedRoot, "main.py")}, SeedRoot: seedRoot, Entry: "entry.yaml", Pack: pack}

	if _, err := ResolveFS(spec, files); err != nil {
		t.Fatal(err)
	}
	// 用户改了初始化出来的那份，再启动一次：只能读它，不能再被默认数据盖回去。
	touched := filepath.Join(seedRoot, "nested", "city_list.json")
	if err := os.WriteFile(touched, []byte("用户改过的"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := ResolveFS(spec, files)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindSeeded {
		t.Fatalf("入口仍缺（用户自己放）时应继续走落盘分支：得到 %s", result.Kind)
	}
	if len(result.Existing) != len(pack.Files) || len(result.Written) != 0 {
		t.Fatalf("已存在的目标一律跳过：Existing=%v Written=%v", result.Existing, result.Written)
	}
	if got, _ := os.ReadFile(touched); string(got) != "用户改过的" {
		t.Fatalf("存在即读：不得覆盖已存在文件，得到 %q", got)
	}
}

func TestResolveWithoutSeedRootReportsMissing(t *testing.T) {
	pack, files, _ := rawPack(t)
	result, err := ResolveFS(Spec{Name: "seelex.yaml", Candidates: []string{filepath.Join(t.TempDir(), "nope.yaml")}, Pack: pack}, files)
	if err != nil {
		t.Fatalf("无处落盘不是错误（调用方回退代码默认值）：%v", err)
	}
	if result.Kind != KindMissing || result.Path != "" || len(result.Written) != 0 {
		t.Fatalf("期望 Kind=missing 且不写盘，得到 %+v", result)
	}
}

func TestResolveSeedFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pack, files, _ := rawPack(t)
	result, err := ResolveFS(Spec{Name: "t", Candidates: []string{filepath.Join(dir, "nope.yaml")}, SeedRoot: filepath.Join(blocker, "config"), Entry: "entry.yaml", Pack: pack}, files)
	if err == nil {
		t.Fatal("落盘失败必须报错（目录被文件占位），而不是假装初始化成功")
	}
	if result.Kind != KindMissing {
		t.Fatalf("落盘失败时调用方要能回退默认值：期望 Kind=missing，得到 %s", result.Kind)
	}
}

// TestResolveSeedWithoutEntryReportsEmptyPath：默认数据里没有入口文件（脚本要用户
// 自己放的场景）时，如实报告"入口仍缺"，不返回一个不存在的路径。
func TestResolveSeedWithoutEntryReportsEmptyPath(t *testing.T) {
	dir := t.TempDir()
	seedRoot := filepath.Join(dir, "local", "tools", "auto_get_jobs")
	pack := Pack{Name: "auto_get_jobs", Files: []File{{Rel: "README.md", Source: "assets/skeleton.md"}}}
	files := fstest.MapFS{"assets/skeleton.md": {Data: []byte("# 骨架\n")}}

	result, err := ResolveFS(Spec{Name: "auto_get_jobs", Candidates: []string{filepath.Join(seedRoot, "main.py")}, SeedRoot: seedRoot, Entry: "main.py", Pack: pack}, files)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindSeeded || result.Path != "" {
		t.Fatalf("骨架里没有 main.py：期望 Kind=seeded 且 Path 为空，得到 %+v", result)
	}
	if _, statErr := os.Stat(filepath.Join(seedRoot, "README.md")); statErr != nil {
		t.Fatalf("骨架应落盘：%v", statErr)
	}
}

func TestMaterializeRejectsEscapingRel(t *testing.T) {
	dir := t.TempDir()
	pack := Pack{Name: "bad", Files: []File{{Rel: "../escape.yaml", Source: "assets/plain.yaml"}}}
	files := fstest.MapFS{"assets/plain.yaml": {Data: []byte("x")}}
	if _, _, err := Materialize(dir, pack, files); err == nil {
		t.Fatal("相对路径逃逸必须被拒（不许把默认数据写到落盘根之外）")
	}
}

func TestDecodeBase64RoundTripKeepsNonUTF8Bytes(t *testing.T) {
	payload := []byte{0xB1, 0xB1, 0xBE, 0xA9, 0x00, 0xFF, 0x0A} // GBK + NUL + 非法 UTF-8 字节
	files := fstest.MapFS{"assets/x.b64": {Data: []byte(EncodeBase64(payload) + "\n")}}
	got, err := Decode(files, File{Rel: "x.json", Source: "assets/x.b64", Encoding: EncodingBase64})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("编码只是承载方式：解回来必须逐字节相同，得到 %v，期望 %v", got, payload)
	}
}
