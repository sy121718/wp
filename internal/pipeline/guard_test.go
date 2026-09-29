package pipeline

// guard_test.go — AccessGuard 发布侧（守卫产物生成、位置约定、映射单源）。
//
// 两条断言是这次改造的承重墙：
//   · TestGuardPasswordChangeChangesArtifactHash —— 改密码必须换产物 hash。
//     否则 PutArtifact 会命中「同 hash 目录已存在」的幂等分支，新 guard.json
//     永远写不进去，表现是「改了密码还是旧密码生效」；
//   · TestLocalStorePersistsGuardFiles —— 产物落盘不再硬编码两个文件名。
//     store.go 里那份硬编码会让 guard.html 永远不落盘，而编译与其它测试
//     全都看不出来（访问面只能一直走内置兜底页）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// testManifest 一个最小可用的 Manifest（NewArtifact* 要求 canonicalPath 与 sourceId）。
func testManifest(path string) *Manifest {
	return &Manifest{
		ManifestSchemaVersion:     ManifestSchemaVersion,
		PageDocumentSchemaVersion: 1,
		CompilerVersion:           "test",
		SourceID:                  "page-1",
		SourceType:                SourceTypePage,
		CanonicalPath:             path,
		SourceHash:                "src",
		BuildInputHash:            "bin",
		Files:                     map[string]string{},
	}
}

// TestBuildGuardEntriesPublicIsNoop 公开页面不生成任何守卫产物。
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

// TestBuildGuardEntriesPasswordCarriesHashAndFingerprint 密码类型生成守卫页与元数据。
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
	// 守卫页必须是 noindex：少了它搜索引擎会收录「要密码」这一页。
	if !strings.Contains(string(page), "noindex") {
		t.Error("守卫页缺少 noindex")
	}
	// 构建期写入的守卫页带路径占位符（路径由访问面按请求注入）。
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
	// Manifest 上的标记只带类型：manifest 可被公开读取，哈希与指纹不进清单。
	markerJSON, merr := json.Marshal(meta)
	if merr != nil {
		t.Fatal(merr)
	}
	if string(markerJSON) != `{"type":"password"}` {
		t.Errorf("Manifest 访问标记形状不符（不应含哈希/指纹）：%s", markerJSON)
	}
}

// TestBuildGuardEntriesMembersHasNoPasswordForm 登录可见不提供密码表单。
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

// TestGuardPasswordChangeChangesArtifactHash 改密码必须换产物 hash。
func TestGuardPasswordChangeChangesArtifactHash(t *testing.T) {
	hashOf := func(hash string) string {
		files, meta := BuildGuardEntries(&builder.AccessGuardSettings{
			Type: builder.AccessPassword, PasswordHash: hash,
		}, "zh-CN")
		m := testManifest("/about")
		m.Access = meta
		a, err := NewArtifactWithEntries([]byte("<html>page</html>"), m, files)
		if err != nil {
			t.Fatalf("组装产物失败: %v", err)
		}
		return a.Hash
	}
	if hashOf("$2a$10$aaaa") == hashOf("$2a$10$bbbb") {
		t.Fatal("改密码后产物 hash 未变 —— 新 guard.json 永远写不进去")
	}
}

// TestGuardDoNotChangePublicArtifactHash 公开页面不因新增能力而改变产物字节。
func TestGuardDoNotChangePublicArtifactHash(t *testing.T) {
	newPublic := func() *Artifact {
		a, err := NewArtifact([]byte("<html>page</html>"), testManifest("/about"))
		if err != nil {
			t.Fatalf("组装产物失败: %v", err)
		}
		return a
	}
	a := newPublic()
	// access 字段必须被 omitempty 省略，否则一次升级会改掉全站产物 hash。
	encoded, err := EncodeManifest(&a.Manifest)
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

// TestLocalStorePersistsGuardFiles 产物落盘按 Entries 写，guard.html 真的落盘。
func TestLocalStorePersistsGuardFiles(t *testing.T) {
	store := &LocalStore{Root: t.TempDir()}
	files, meta := BuildGuardEntries(&builder.AccessGuardSettings{
		Type: builder.AccessPassword, PasswordHash: "$2a$10$abcdefghijklmnopqrstuv",
	}, "zh-CN")
	m := testManifest("/about")
	m.Access = meta
	a, err := NewArtifactWithEntries([]byte("<html>page</html>"), m, files)
	if err != nil {
		t.Fatalf("组装产物失败: %v", err)
	}
	loc, err := store.PutArtifact(a)
	if err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	dir := filepath.Join(store.Root, "artifacts", locatorHash(loc))
	for _, name := range []string{"index.html", "manifest.json", GuardPageFileName, GuardMetaFileName} {
		if _, serr := os.Stat(filepath.Join(dir, name)); serr != nil {
			t.Errorf("%s 未落盘: %v（store 是否还在硬编码文件名？）", name, serr)
		}
	}
	// 落盘的守卫页必须能被访问面读出来（位置约定同源）。
	if _, ok := GuardPageBody(dir); !ok {
		t.Error("GuardPageBody 读不到刚落盘的守卫页")
	}
	if got, gerr := ReadGuardMeta(dir); gerr != nil || got == nil {
		t.Errorf("ReadGuardMeta 读不到刚落盘的元数据: %v", gerr)
	}
	// 幂等重放必须一致（同 hash 不覆盖）。
	if _, err := store.PutArtifact(a); err != nil {
		t.Errorf("重复落盘应幂等成功: %v", err)
	}
}

// TestNewArtifactWithEntriesRejectsBadNames 伴随文件重名与越界路径一律拒绝。
func TestNewArtifactWithEntriesRejectsBadNames(t *testing.T) {
	if _, err := NewArtifactWithEntries([]byte("x"), testManifest("/a"), map[string][]byte{
		"index.html": []byte("dup"),
	}); err == nil {
		t.Error("与入口重名应被拒绝")
	}
	if _, err := NewArtifactWithEntries([]byte("x"), testManifest("/a"), map[string][]byte{
		"../escape.html": []byte("x"),
	}); err == nil {
		t.Error("越界路径应被拒绝")
	}
}

// TestReadGuardMetaMissingVsCorrupted 存在性判据与 fail closed 的分界。
func TestReadGuardMetaMissingVsCorrupted(t *testing.T) {
	dir := t.TempDir()
	// 不存在 → (nil, nil)：访问面据此放行。
	got, err := ReadGuardMeta(dir)
	if err != nil || got != nil {
		t.Fatalf("缺失期望 (nil, nil)，实际 (%v, %v)", got, err)
	}
	// 存在但解析失败 → error：访问面据此 fail closed。
	if werr := os.WriteFile(filepath.Join(dir, GuardMetaFileName), []byte("{not json"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if _, err := ReadGuardMeta(dir); err == nil {
		t.Fatal("损坏的元数据必须返回 error（否则访问面会当公开放行）")
	}
}

// TestGuardPageWithPathEscapes 注入的站点路径必须 HTML 转义（否则是一处反射 XSS）。
func TestGuardPageWithPathEscapes(t *testing.T) {
	page := RenderGuardPage(GuardMeta{Type: builder.AccessPassword}, GuardBackPlaceholder, false)
	out := string(GuardPageWithPath(page, `/a"onmouseover="alert(1)`))
	if strings.Contains(out, `value="/a"onmouseover=`) {
		t.Fatal("路径未转义，存在属性注入")
	}
	if strings.Contains(out, GuardBackPlaceholder) {
		t.Fatal("占位符未被替换")
	}
}

// TestGuardFingerprint 指纹随哈希变化且稳定。
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
	if a == SHA256([]byte("hash-a")) {
		t.Fatal("指纹与裸 SHA256 相同（领域前缀失效，可能与别的摘要撞读）")
	}
}

// TestResolveActiveEntryMapping 条目映射单源：访问面与守卫共用一条规则。
func TestResolveActiveEntryMapping(t *testing.T) {
	root := t.TempDir()
	// 符号链接目标与生产同规则（LocalPublicationStore.Activate）：
	// 上溯层数 = 路径段数 + 1，目标是相对链接。
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
		// 显式文件名写法必须归到同一条目 —— 否则守卫被 `/about/index.html` 绕过。
		{"about/index.html", "about", true},
		{"about/", "about", true},
		// 页面 URL 真叫 /foo/index.html 时，它有自己的产物，不能归到 foo。
		{"foo/index.html", "foo/index.html", true},
		{"ghost", "", false},
		{"../escape", "", false},
	}
	for _, c := range cases {
		entry, ok := ResolveActiveEntry(active, c.rel)
		if ok != c.ok || entry != c.entry {
			t.Errorf("ResolveActiveEntry(%q) = (%q, %v)，期望 (%q, %v)", c.rel, entry, ok, c.entry, c.ok)
		}
	}
}

// TestCleanSiteRelRejectsTraversal 归一的越界判据（守卫与静态面共用）。
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
		got, ok := CleanSiteRel(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("CleanSiteRel(%q) = (%q, %v)，期望 (%q, %v)", c.raw, got, ok, c.want, c.ok)
		}
	}
}

// TestPageGuardEntriesFromDocument 从冻结的 Page Document 取访问设置。
func TestPageGuardEntriesFromDocument(t *testing.T) {
	doc := []byte(`{"settings":{"access":{"type":"password","passwordHash":"$2a$10$abcdefghijklmnopqrstuv"}},"root":[]}`)
	files, meta, err := PageGuardEntries(doc, "zh-CN")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if meta == nil || meta.Type != builder.AccessPassword || files == nil {
		t.Fatalf("期望生成守卫产物，实际 meta=%+v files=%v", meta, files)
	}
	// 没有 access 字段的存量文档：不生成（行为与加能力前逐字一致）。
	if files, meta, err := PageGuardEntries([]byte(`{"root":[]}`), "zh-CN"); err != nil || files != nil || meta != nil {
		t.Fatalf("公开文档不应生成守卫产物，实际 err=%v meta=%+v", err, meta)
	}
	// 坏文档必须报错（构建失败），绝不能按公开出产物。
	if _, _, err := PageGuardEntries([]byte(`{not json`), "zh-CN"); err == nil {
		t.Fatal("坏文档必须报错，而不是按公开放行")
	}
}

// TestBuildGuardEntriesUnknownTypeFailsClosed 未知类型照样生成守卫（谁都进不去）。
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
