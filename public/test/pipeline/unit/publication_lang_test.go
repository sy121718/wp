package unit

import (
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalPublicationLangPrefix(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	zh := putTestArtifact(t, store, "<html>zh</html>", "/zh-CN/about")
	en := putTestArtifact(t, store, "<html>en</html>", "/en-US/about")

	for _, c := range []struct {
		path string
		loc  publish.Locator
		want string
	}{
		{"/zh-CN/about", zh, "<html>zh</html>"},
		{"/en-US/about", en, "<html>en</html>"},
	} {
		if err := pub.Activate(c.path, c.loc); err != nil {
			t.Fatalf("激活 %s 失败: %v", c.path, err)
		}
		st, err := pub.Inspect(c.path)
		if err != nil || st.Kind != publish.PublicationPage || st.Locator == nil || st.Locator.Key != c.loc.Key {
			t.Fatalf("激活状态错误 %s: %+v err=%v", c.path, st, err)
		}
		data, rerr := os.ReadFile(filepath.Join(pub.ActiveRoot, strings.TrimPrefix(c.path, "/"), "index.html"))
		if rerr != nil || string(data) != c.want {
			t.Fatalf("激活入口不可读 %s: %v %q", c.path, rerr, data)
		}
	}

	root := putTestArtifact(t, store, "<html>root</html>", "/zh-CN/index")
	if err := pub.Activate("/zh-CN/index", root); err != nil {
		t.Fatalf("语言根页激活失败: %v", err)
	}
	if st, err := pub.Inspect("/zh-CN/index"); err != nil || st.Kind != publish.PublicationPage {
		t.Fatalf("语言根页状态错误: %+v err=%v", st, err)
	}

	if st, err := pub.Inspect("/zh-CN/about"); err != nil || st.Kind != publish.PublicationPage {
		t.Fatalf("语言根页激活后子路径状态被破坏: %+v err=%v", st, err)
	}
}

func TestLocalPublicationRejectsSymlinkAncestor(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	parent := putTestArtifact(t, store, "<html>parent</html>", "/zh-CN")
	if err := pub.Activate("/zh-CN", parent); err != nil {
		t.Fatalf("父路径激活失败: %v", err)
	}
	child := putTestArtifact(t, store, "<html>child</html>", "/zh-CN/about")
	if err := pub.Activate("/zh-CN/about", child); err == nil {
		t.Fatal("父路径已为符号链接时子路径激活应失败")
	}

	artifactDir := filepath.Join(store.Root, "artifacts", parent.Key[strings.LastIndex(parent.Key, "/")+1:])
	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		t.Fatalf("读取产物目录失败: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "index.html" && e.Name() != "manifest.json" {
			t.Fatalf("产物目录被污染，出现额外条目: %s", e.Name())
		}
	}

	st, err := pub.Inspect("/zh-CN")
	if err != nil || st.Kind != publish.PublicationPage {
		t.Fatalf("父路径激活状态被破坏: %+v err=%v", st, err)
	}
}
