package unit

import (
	"bytes"
	"testing"

	"go_wp/internal/pipeline"
)

// baseManifest 构造测试用最小 manifest。
func baseManifest() *pipeline.Manifest {
	return &pipeline.Manifest{
		ManifestSchemaVersion:     pipeline.ManifestSchemaVersion,
		PageDocumentSchemaVersion: 1,
		CompilerVersion:           "test",
		SourceID:                  "page-1",
		SourceType:                pipeline.SourceTypePage,
		CanonicalPath:             "/about",
		SourceHash:                "src-hash",
		BuildInputHash:            "input-hash",
	}
}

// TestManifestDeterminism manifest 编码确定性：dependencies 乱序、files 乱序产出相同字节。
func TestManifestDeterminism(t *testing.T) {
	mk := func() *pipeline.Manifest {
		m := baseManifest()
		m.Dependencies = []pipeline.Dependency{
			{Kind: "media", Key: "asset-1", Revision: "r1"},
			{Kind: "direct_content", Key: "post:1", Revision: "r2"},
			{Kind: "media", Key: "asset-0", Revision: "r3"},
		}
		m.Files = map[string]string{"style.css": "h2", "index.html": "h1"}
		return m
	}

	a, _ := pipeline.EncodeManifest(mk())
	b, _ := pipeline.EncodeManifest(mk())
	if !bytes.Equal(a, b) {
		t.Fatalf("同输入声明的 manifest 编码不一致:\n%s\n%s", a, b)
	}
}

// TestNewArtifactDeterminism 同输入产物内容哈希一致；不同输入哈希不同。
func TestNewArtifactDeterminism(t *testing.T) {
	html := []byte("<html><body>hello</body></html>")
	a1, err := pipeline.NewArtifact(html, baseManifest())
	if err != nil {
		t.Fatalf("构建产物失败: %v", err)
	}
	a2, err := pipeline.NewArtifact(html, baseManifest())
	if err != nil {
		t.Fatalf("构建产物失败: %v", err)
	}
	if a1.Hash != a2.Hash {
		t.Errorf("同输入应产出相同 hash: %s vs %s", a1.Hash, a2.Hash)
	}
	a3, err := pipeline.NewArtifact([]byte("<html>other</html>"), baseManifest())
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

// TestNewArtifactManifestIncomplete manifest 缺字段拒绝组装。
func TestNewArtifactManifestIncomplete(t *testing.T) {
	if _, err := pipeline.NewArtifact([]byte("x"), nil); err == nil {
		t.Error("空 manifest 应被拒绝")
	}
	m := baseManifest()
	m.CanonicalPath = ""
	if _, err := pipeline.NewArtifact([]byte("x"), m); err == nil {
		t.Error("缺 canonicalPath 应被拒绝")
	}
}

// TestRedirectArtifact 重定向产物：301/302 合法，状态码与目标路径校验。
func TestRedirectArtifact(t *testing.T) {
	r, err := pipeline.NewRedirectArtifact("/new-url/", 301)
	if err != nil {
		t.Fatalf("NewRedirectArtifact 失败: %v", err)
	}
	if r.Directive.TargetPath != "/new-url" || r.Directive.StatusCode != 301 {
		t.Errorf("重定向指令错误: %+v", r.Directive)
	}
	if pipeline.SHA256(r.Entry) != r.Hash {
		t.Errorf("重定向 hash 与内容不一致")
	}

	if _, err := pipeline.NewRedirectArtifact("/new-url", 303); err == nil {
		t.Error("303 状态码应被拒绝")
	}
	if _, err := pipeline.NewRedirectArtifact("no-slash", 301); err == nil {
		t.Error("非法目标路径应被拒绝")
	}
	if _, err := pipeline.NewRedirectArtifact("/admin", 301); err == nil {
		t.Error("保留路径应被拒绝")
	}
}

// TestParseRedirectEntry redirect.json 解析与缺失字段校验。
func TestParseRedirectEntry(t *testing.T) {
	d, err := pipeline.ParseRedirectEntry([]byte(`{"targetPath":"/b","statusCode":301}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if d.TargetPath != "/b" || d.StatusCode != 301 {
		t.Errorf("解析结果错误: %+v", d)
	}
	if _, err := pipeline.ParseRedirectEntry([]byte(`{"statusCode":301}`)); err == nil {
		t.Error("缺目标路径应被拒绝")
	}
}
