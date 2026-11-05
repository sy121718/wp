package pipeline

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/publish"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/pkg/builder"
)

func testManifest(path string) *publish.Manifest {
	return &publish.Manifest{
		ManifestSchemaVersion:     publish.ManifestSchemaVersion,
		PageDocumentSchemaVersion: 1,
		CompilerVersion:           "test",
		SourceID:                  "page-1",
		SourceType:                publish.SourceTypePage,
		CanonicalPath:             path,
		SourceHash:                "src",
		BuildInputHash:            "bin",
		Files:                     map[string]string{},
	}
}

func TestBuildGuardEntriesPublicIsNoop(t *testing.T) {
	for _, access := range []*builder.AccessGuardSettings{
		nil,
		{},
		{Type: builder.AccessPublic},
	} {
		files, meta := BuildGuardEntries(access, "zh-CN")
		if files != nil || meta != nil {
			t.Fatalf("公开设置 %+v 期望不生成守卫产物，实际 files=%v meta=%+v", access, files, meta)
		}
	}
}

func TestBuildGuardEntriesPasswordCarriesHashAndFingerprint(t *testing.T) {
	access := &builder.AccessGuardSettings{Type: builder.AccessPassword, PasswordHash: "$2a$10$abcdefghijklmnopqrstuv"}
	files, meta := BuildGuardEntries(access, "en-US")
	if meta == nil || meta.Type != builder.AccessPassword {
		t.Fatalf("期望 password 标记，实际 %+v", meta)
	}
	if files == nil {
		t.Fatal("期望生成守卫产物")
	}
	page, ok := files[GuardPageFileName]
	if !ok {
		t.Fatalf("缺少 %s", GuardPageFileName)
	}

	if !strings.Contains(string(page), "noindex") {
		t.Error("守卫页缺少 noindex")
	}

	if !strings.Contains(string(page), GuardBackPlaceholder) {
		t.Error("守卫页缺少路径占位符")
	}
	metaJSON, ok := files[GuardMetaFileName]
	if !ok {
		t.Fatalf("缺少 %s", GuardMetaFileName)
	}
	var gm GuardMeta
	if err := json.Unmarshal(metaJSON, &gm); err != nil {
		t.Fatalf("元数据解析失败: %v", err)
	}
	if gm.PasswordHash != access.PasswordHash {
		t.Error("元数据未带密码哈希")
	}
	if gm.Fingerprint != GuardFingerprint(access.PasswordHash) || gm.Fingerprint == "" {
		t.Errorf("指纹不符：%q", gm.Fingerprint)
	}
	if gm.Lang != "en-US" {
		t.Errorf("元数据语言未记录：%q", gm.Lang)
	}

	markerJSON, merr := json.Marshal(meta)
	if merr != nil {
		t.Fatal(merr)
	}
	if string(markerJSON) != `{"type":"password"}` {
		t.Errorf("publish.Manifest 访问标记形状不符（不应含哈希/指纹）：%s", markerJSON)
	}
}

func TestBuildGuardEntriesMembersHasNoPasswordForm(t *testing.T) {
	files, meta := BuildGuardEntries(&builder.AccessGuardSettings{Type: builder.AccessMembers}, "zh-CN")
	if meta == nil || meta.Type != builder.AccessMembers {
		t.Fatalf("期望 members 标记，实际 %+v", meta)
	}
	page := string(files[GuardPageFileName])
	if strings.Contains(page, "name=\"password\"") {
		t.Error("members 守卫页不应包含密码输入框")
	}
	if !strings.Contains(page, "noindex") {
		t.Error("守卫页缺少 noindex")
	}
}

func TestGuardPasswordChangeChangesArtifactHash(t *testing.T) {
	hashOf := func(hash string) string {
		files, meta := BuildGuardEntries(&builder.AccessGuardSettings{
			Type: builder.AccessPassword, PasswordHash: hash,
		}, "zh-CN")
		m := testManifest("/about")
		m.Access = meta
		a, err := publish.NewArtifactWithEntries([]byte("<html>page</html>"), m, files)
		if err != nil {
			t.Fatalf("组装产物失败: %v", err)
		}
		return a.Hash
	}
	if hashOf("$2a$10$aaaa") == hashOf("$2a$10$bbbb") {
		t.Fatal("改密码后产物 hash 未变 —— 新 guard.json 永远写不进去")
	}
}

func TestGuardDoNotChangePublicArtifactHash(t *testing.T) {
	newPublic := func() *publish.Artifact {
		a, err := publish.NewArtifact([]byte("<html>page</html>"), testManifest("/about"))
		if err != nil {
			t.Fatalf("组装产物失败: %v", err)
		}
		return a
	}
	a := newPublic()

	encoded, err := publish.EncodeManifest(&a.Manifest)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if strings.Contains(string(encoded), "\"access\"") {
		t.Fatalf("公开产物的 manifest 里出现了 access 字段：%s", encoded)
	}
	if a.Manifest.Access != nil {
		t.Error("公开产物的 Access 应为 nil")
	}
}

func TestLocalStorePersistsGuardFiles(t *testing.T) {
	store := &publish.LocalStore{Root: t.TempDir()}
	files, meta := BuildGuardEntries(&builder.AccessGuardSettings{
		Type: builder.AccessPassword, PasswordHash: "$2a$10$abcdefghijklmnopqrstuv",
	}, "zh-CN")
	m := testManifest("/about")
	m.Access = meta
	a, err := publish.NewArtifactWithEntries([]byte("<html>page</html>"), m, files)
	if err != nil {
		t.Fatalf("组装产物失败: %v", err)
	}
	loc, err := store.PutArtifact(a)
	if err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	dir := filepath.Join(store.Root, "artifacts", publish.LocatorHash(loc))
	for _, name := range []string{"index.html", "manifest.json", GuardPageFileName, GuardMetaFileName} {
		if _, serr := os.Stat(filepath.Join(dir, name)); serr != nil {
			t.Errorf("%s 未落盘: %v（store 是否还在硬编码文件名？）", name, serr)
		}
	}

	if _, ok := GuardPageBody(dir); !ok {
		t.Error("GuardPageBody 读不到刚落盘的守卫页")
	}
	if got, gerr := ReadGuardMeta(dir); gerr != nil || got == nil {
		t.Errorf("ReadGuardMeta 读不到刚落盘的元数据: %v", gerr)
	}

	if _, err := store.PutArtifact(a); err != nil {
		t.Errorf("重复落盘应幂等成功: %v", err)
	}
}

func TestNewArtifactWithEntriesRejectsBadNames(t *testing.T) {
	if _, err := publish.NewArtifactWithEntries([]byte("x"), testManifest("/a"), map[string][]byte{
		"index.html": []byte("dup"),
	}); err == nil {
		t.Error("与入口重名应被拒绝")
	}
	if _, err := publish.NewArtifactWithEntries([]byte("x"), testManifest("/a"), map[string][]byte{
		"../escape.html": []byte("x"),
	}); err == nil {
		t.Error("越界路径应被拒绝")
	}
}

func TestReadGuardMetaMissingVsCorrupted(t *testing.T) {
	dir := t.TempDir()

	got, err := ReadGuardMeta(dir)
	if err != nil || got != nil {
		t.Fatalf("缺失期望 (nil, nil)，实际 (%v, %v)", got, err)
	}

	if werr := os.WriteFile(filepath.Join(dir, GuardMetaFileName), []byte("{not json"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if _, err := ReadGuardMeta(dir); err == nil {
		t.Fatal("损坏的元数据必须返回 error（否则访问面会当公开放行）")
	}
}

func TestGuardPageWithPathEscapes(t *testing.T) {
	page := publish.RenderGuardPage(GuardMeta{Type: builder.AccessPassword}, GuardBackPlaceholder, false)
	out := string(GuardPageWithPath(page, `/a"onmouseover="alert(1)`))
	if strings.Contains(out, `value="/a"onmouseover=`) {
		t.Fatal("路径未转义，存在属性注入")
	}
	if strings.Contains(out, GuardBackPlaceholder) {
		t.Fatal("占位符未被替换")
	}
}

func TestGuardFingerprint(t *testing.T) {
	a := GuardFingerprint("hash-a")
	if a == "" || len(a) != 32 {
		t.Fatalf("指纹形状不符：%q", a)
	}
	if a != GuardFingerprint("hash-a") {
		t.Fatal("同一哈希的指纹必须稳定（否则每次构建都换产物）")
	}
	if a == GuardFingerprint("hash-b") {
		t.Fatal("不同哈希的指纹必须不同（否则改密码不失效旧解锁）")
	}
	if a == publish.SHA256([]byte("hash-a")) {
		t.Fatal("指纹与裸 publish.SHA256 相同（领域前缀失效，可能与别的摘要撞读）")
	}
}

func TestResolveActiveEntryMapping(t *testing.T) {
	root := t.TempDir()

	mk := func(entry string) {
		dirName := "h-" + strings.ReplaceAll(entry, "/", "_")
		dir := filepath.Join(root, "artifacts", dirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, "active", entry)), 0o755); err != nil {
			t.Fatal(err)
		}
		target := strings.Repeat("../", strings.Count(entry, "/")+1) + filepath.Join("artifacts", dirName)
		if err := os.Symlink(target, filepath.Join(root, "active", entry)); err != nil {
			t.Fatal(err)
		}
	}
	mk("index")
	mk("about")
	mk("foo/index.html")

	active := filepath.Join(root, "active")
	cases := []struct {
		rel   string
		entry string
		ok    bool
	}{
		{"", "index", true},
		{"about", "about", true},

		{"about/index.html", "about", true},
		{"about/", "about", true},

		{"foo/index.html", "foo/index.html", true},
		{"ghost", "", false},
		{"../escape", "", false},
	}
	for _, c := range cases {
		entry, ok := publish.ResolveActiveEntry(active, c.rel)
		if ok != c.ok || entry != c.entry {
			t.Errorf("publish.ResolveActiveEntry(%q) = (%q, %v)，期望 (%q, %v)", c.rel, entry, ok, c.entry, c.ok)
		}
	}
}

func TestCleanSiteRelRejectsTraversal(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"", "", true},
		{"/about/", "about", true},
		{"a//b", "a/b", true},
		{"../etc/passwd", "", false},
		{"/..", "", false},
		{"a/../../b", "", false},
	}
	for _, c := range cases {
		got, ok := publish.CleanSiteRel(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("publish.CleanSiteRel(%q) = (%q, %v)，期望 (%q, %v)", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestPageGuardEntriesFromDocument(t *testing.T) {
	doc := []byte(`{"settings":{"access":{"type":"password","passwordHash":"$2a$10$abcdefghijklmnopqrstuv"}},"root":[]}`)
	files, meta, err := PageGuardEntries(doc, "zh-CN")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if meta == nil || meta.Type != builder.AccessPassword || files == nil {
		t.Fatalf("期望生成守卫产物，实际 meta=%+v files=%v", meta, files)
	}

	if files, meta, err := PageGuardEntries([]byte(`{"root":[]}`), "zh-CN"); err != nil || files != nil || meta != nil {
		t.Fatalf("公开文档不应生成守卫产物，实际 err=%v meta=%+v", err, meta)
	}

	if _, _, err := PageGuardEntries([]byte(`{not json`), "zh-CN"); err == nil {
		t.Fatal("坏文档必须报错，而不是按公开放行")
	}
}

func TestBuildGuardEntriesUnknownTypeFailsClosed(t *testing.T) {
	files, meta := BuildGuardEntries(&builder.AccessGuardSettings{Type: "sso"}, "zh-CN")
	if meta == nil || files == nil {
		t.Fatal("未知类型不能当作公开放行")
	}
	if meta.Type != "sso" {
		t.Fatalf("类型应原样记录，实际 %q", meta.Type)
	}
	if _, ok := files[GuardMetaFileName]; !ok {
		t.Fatal("未知类型也要写元数据（访问面据其判定拒绝）")
	}
}
