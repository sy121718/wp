package unit

import (
	"bytes"
	publish "go_wp/pkg/publish"
	"testing"
)

func baseManifest() *publish.Manifest {
	return &publish.Manifest{
		ManifestSchemaVersion:     publish.ManifestSchemaVersion,
		PageDocumentSchemaVersion: 1,
		CompilerVersion:           "test",
		SourceID:                  "page-1",
		SourceType:                publish.SourceTypePage,
		CanonicalPath:             "/about",
		SourceHash:                "src-hash",
		BuildInputHash:            "input-hash",
	}
}

func TestManifestDeterminism(t *testing.T) {
	mk := func() *publish.Manifest {
		m := baseManifest()
		m.Dependencies = []publish.Dependency{
			{Kind: "media", Key: "asset-1", Revision: "r1"},
			{Kind: "direct_content", Key: "post:1", Revision: "r2"},
			{Kind: "media", Key: "asset-0", Revision: "r3"},
		}
		m.Files = map[string]string{"style.css": "h2", "index.html": "h1"}
		return m
	}

	a, _ := publish.EncodeManifest(mk())
	b, _ := publish.EncodeManifest(mk())
	if !bytes.Equal(a, b) {
		t.Fatalf("同输入声明的 manifest 编码不一致:\n%s\n%s", a, b)
	}
}

func TestNewArtifactDeterminism(t *testing.T) {
	html := []byte("<html><body>hello</body></html>")
	a1, err := publish.NewArtifact(html, baseManifest())
	if err != nil {
		t.Fatalf("构建产物失败: %v", err)
	}
	a2, err := publish.NewArtifact(html, baseManifest())
	if err != nil {
		t.Fatalf("构建产物失败: %v", err)
	}
	if a1.Hash != a2.Hash {
		t.Errorf("同输入应产出相同 hash: %s vs %s", a1.Hash, a2.Hash)
	}
	a3, err := publish.NewArtifact([]byte("<html>other</html>"), baseManifest())
	if err != nil {
		t.Fatalf("构建产物失败: %v", err)
	}
	if a3.Hash == a1.Hash {
		t.Errorf("不同输入不应产出相同 hash")
	}
	if string(a1.Entries["index.html"]) != string(html) {
		t.Errorf("入口文件内容应等于输入 HTML")
	}
}

func TestNewArtifactManifestIncomplete(t *testing.T) {
	if _, err := publish.NewArtifact([]byte("x"), nil); err == nil {
		t.Error("空 manifest 应被拒绝")
	}
	m := baseManifest()
	m.CanonicalPath = ""
	if _, err := publish.NewArtifact([]byte("x"), m); err == nil {
		t.Error("缺 canonicalPath 应被拒绝")
	}
}

func TestRedirectArtifact(t *testing.T) {
	r, err := publish.NewRedirectArtifact("/new-url/", 301)
	if err != nil {
		t.Fatalf("NewRedirectArtifact 失败: %v", err)
	}
	if r.Directive.TargetPath != "/new-url" || r.Directive.StatusCode != 301 {
		t.Errorf("重定向指令错误: %+v", r.Directive)
	}
	if publish.SHA256(r.Entry) != r.Hash {
		t.Errorf("重定向 hash 与内容不一致")
	}

	if _, err := publish.NewRedirectArtifact("/new-url", 303); err == nil {
		t.Error("303 状态码应被拒绝")
	}
	if _, err := publish.NewRedirectArtifact("no-slash", 301); err == nil {
		t.Error("非法目标路径应被拒绝")
	}
	if _, err := publish.NewRedirectArtifact("/admin", 301); err == nil {
		t.Error("保留路径应被拒绝")
	}
}

func TestParseRedirectEntry(t *testing.T) {
	d, err := publish.ParseRedirectEntry([]byte(`{"targetPath":"/b","statusCode":301}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if d.TargetPath != "/b" || d.StatusCode != 301 {
		t.Errorf("解析结果错误: %+v", d)
	}
	if _, err := publish.ParseRedirectEntry([]byte(`{"statusCode":301}`)); err == nil {
		t.Error("缺目标路径应被拒绝")
	}
}
