package unit

import (
	"os"
	"path/filepath"
	"testing"

	"go_wp/internal/pipeline"
)

// newPublicationEnv 构造本地产物体 + 发布激活体（ActiveRoot 位于产物根下的 public/active，
// 与 artifacts 同级，保证 symlink 相对目标 ../../artifacts/{hash} 物理可达）。
func newPublicationEnv(t *testing.T) (*pipeline.LocalStore, *pipeline.LocalPublicationStore, string) {
	t.Helper()
	root := t.TempDir()
	store := &pipeline.LocalStore{Root: root}
	pub := &pipeline.LocalPublicationStore{ActiveRoot: filepath.Join(root, "public/active")}
	return store, pub, root
}

// putTestArtifact 写入一个测试产物并返回定位器。
func putTestArtifact(t *testing.T, s *pipeline.LocalStore, html, path string) pipeline.Locator {
	t.Helper()
	m := baseManifest()
	m.CanonicalPath = path
	a, err := pipeline.NewArtifact([]byte(html), m)
	if err != nil {
		t.Fatalf("构建产物失败: %v", err)
	}
	loc, err := s.PutArtifact(a)
	if err != nil {
		t.Fatalf("PutArtifact 失败: %v", err)
	}
	return loc
}

// TestLocalPublicationActivateLifecycle 激活/检查/取消激活闭环。
func TestLocalPublicationActivateLifecycle(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	loc := putTestArtifact(t, store, "<html>v1</html>", "/about")

	if err := pub.Activate("/about/", loc); err != nil {
		t.Fatalf("Activate 失败: %v", err)
	}
	st, err := pub.Inspect("/about")
	if err != nil {
		t.Fatalf("Inspect 失败: %v", err)
	}
	if st.Kind != pipeline.PublicationPage || st.Locator == nil || st.Locator.Key != loc.Key {
		t.Errorf("激活状态错误: %+v", st)
	}
	// 激活路径可直接读到产物入口。
	link := filepath.Join(pub.ActiveRoot, "about")
	data, err := os.ReadFile(link + "/index.html")
	if err != nil || string(data) != "<html>v1</html>" {
		t.Errorf("激活后入口不可读: %v %q", err, data)
	}

	if err := pub.Deactivate("/about"); err != nil {
		t.Fatalf("Deactivate 失败: %v", err)
	}
	st, err = pub.Inspect("/about")
	if err != nil {
		t.Fatalf("Inspect 失败: %v", err)
	}
	if st.Kind != pipeline.PublicationNone {
		t.Errorf("取消激活后应为 none: %+v", st)
	}
	if err := pub.Deactivate("/about"); err != nil {
		t.Errorf("重复 Deactivate 应幂等: %v", err)
	}
}

// TestLocalPublicationAtomicSwitch 二次激活原子切换指针，旧请求文件仍可读。
func TestLocalPublicationAtomicSwitch(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	loc1 := putTestArtifact(t, store, "<html>v1</html>", "/about")
	loc2 := putTestArtifact(t, store, "<html>v2</html>", "/about")

	if err := pub.Activate("/about", loc1); err != nil {
		t.Fatalf("首次激活失败: %v", err)
	}
	if err := pub.Activate("/about", loc2); err != nil {
		t.Fatalf("二次激活失败: %v", err)
	}
	st, _ := pub.Inspect("/about")
	if st.Locator.Key != loc2.Key {
		t.Errorf("指针应指向新版本: %s", st.Locator.Key)
	}
	// 旧产物未被覆盖（不可变性）。
	old := filepath.Join(store.Root, loc1.Key, "index.html")
	if data, err := os.ReadFile(old); err != nil || string(data) != "<html>v1</html>" {
		t.Errorf("历史产物应保持不变: %v %q", err, data)
	}
}

// TestLocalPublicationMultiLevel 多级路径激活。
func TestLocalPublicationMultiLevel(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	loc := putTestArtifact(t, store, "<html>x</html>", "/products/phone")
	if err := pub.Activate("/products/phone", loc); err != nil {
		t.Fatalf("多级路径激活失败: %v", err)
	}
	st, _ := pub.Inspect("/products/phone")
	if st.Kind != pipeline.PublicationPage {
		t.Errorf("多级路径状态错误: %+v", st)
	}
}

// TestLocalPublicationRedirectActivate 重定向产物激活与检查。
func TestLocalPublicationRedirectActivate(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	r, _ := pipeline.NewRedirectArtifact("/new-url", 301)
	loc, err := store.PutRedirect(r)
	if err != nil {
		t.Fatalf("PutRedirect 失败: %v", err)
	}
	if err := pub.Activate("/old-url", loc); err != nil {
		t.Fatalf("重定向激活失败: %v", err)
	}
	st, err := pub.Inspect("/old-url")
	if err != nil {
		t.Fatalf("Inspect 失败: %v", err)
	}
	if st.Kind != pipeline.PublicationRedirect || st.Redirect == nil ||
		st.Redirect.TargetPath != "/new-url" || st.Redirect.StatusCode != 301 {
		t.Errorf("重定向状态错误: %+v", st)
	}
}

// TestLocalPublicationInspectGuards 异常状态与悬空目标检查。
func TestLocalPublicationInspectGuards(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)

	// 悬空目标：产物被删除后 inspect 报错。
	loc := putTestArtifact(t, store, "<html>x</html>", "/gone")
	if err := pub.Activate("/gone", loc); err != nil {
		t.Fatalf("激活失败: %v", err)
	}
	hash := loc.Key[lastSlashIndex(loc.Key)+1:]
	if err := store.DeleteArtifact(loc, hash); err != nil {
		t.Fatalf("删除产物失败: %v", err)
	}
	if _, err := pub.Inspect("/gone"); err == nil {
		t.Error("悬空目标应报错")
	}

	// 非符号链接占位报错。
	link := filepath.Join(pub.ActiveRoot, "occupied")
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatalf("mkdir 失败: %v", err)
	}
	if _, err := pub.Inspect("/occupied"); err == nil {
		t.Error("非 symlink 占位应报错")
	}
}

// lastSlashIndex 返回字符串最后一个 / 的下标（无则 -1）。
func lastSlashIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
