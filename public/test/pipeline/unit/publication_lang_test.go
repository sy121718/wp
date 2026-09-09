package unit

// publication_lang_test.go — 多语言激活与父子路径防线（多语言 P2，docs/06-D §4.4/§5）。
//
// 1. 语言前缀路径（/{lang}/path）在激活层天然可用：多级路径的 symlink 上溯深度
//    由路径层数自动计算，语言段只是普通目录段；
// 2. 语言根 /{lang} 与同语言子路径的父子冲突必须在激活层显式失败，
//    绝不允许把符号链接写进不可变产物目录（否则污染 artifacts/{hash}）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/pipeline"
)

// TestLocalPublicationLangPrefix 语言前缀路径可独立激活并直读产物入口。
func TestLocalPublicationLangPrefix(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	zh := putTestArtifact(t, store, "<html>zh</html>", "/zh-CN/about")
	en := putTestArtifact(t, store, "<html>en</html>", "/en-US/about")

	for _, c := range []struct {
		path string
		loc  pipeline.Locator
		want string
	}{
		{"/zh-CN/about", zh, "<html>zh</html>"},
		{"/en-US/about", en, "<html>en</html>"},
	} {
		if err := pub.Activate(c.path, c.loc); err != nil {
			t.Fatalf("激活 %s 失败: %v", c.path, err)
		}
		st, err := pub.Inspect(c.path)
		if err != nil || st.Kind != pipeline.PublicationPage || st.Locator == nil || st.Locator.Key != c.loc.Key {
			t.Fatalf("激活状态错误 %s: %+v err=%v", c.path, st, err)
		}
		data, rerr := os.ReadFile(filepath.Join(pub.ActiveRoot, strings.TrimPrefix(c.path, "/"), "index.html"))
		if rerr != nil || string(data) != c.want {
			t.Fatalf("激活入口不可读 %s: %v %q", c.path, rerr, data)
		}
	}

	// 语言根页按 D1 决策映射为 /{lang}/index（语言根不单独占 /{lang}）。
	root := putTestArtifact(t, store, "<html>root</html>", "/zh-CN/index")
	if err := pub.Activate("/zh-CN/index", root); err != nil {
		t.Fatalf("语言根页激活失败: %v", err)
	}
	if st, err := pub.Inspect("/zh-CN/index"); err != nil || st.Kind != pipeline.PublicationPage {
		t.Fatalf("语言根页状态错误: %+v err=%v", st, err)
	}
	// 与同语言子路径互不干扰（父子冲突在 /{lang}/index 方案下不存在）。
	if st, err := pub.Inspect("/zh-CN/about"); err != nil || st.Kind != pipeline.PublicationPage {
		t.Fatalf("语言根页激活后子路径状态被破坏: %+v err=%v", st, err)
	}
}

// TestLocalPublicationRejectsSymlinkAncestor 父路径已是符号链接时再激活子路径
// 必须显式失败，且不得在不可变产物目录内部留下任何痕迹（§4.4 硬坑回归）。
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

	// 不可变产物目录内不得出现子路径符号链接（污染检测）。
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
	// 父路径自身仍可正常读取。
	st, err := pub.Inspect("/zh-CN")
	if err != nil || st.Kind != pipeline.PublicationPage {
		t.Fatalf("父路径激活状态被破坏: %+v err=%v", st, err)
	}
}
