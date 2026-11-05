package unit

import (
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newPublicationEnv(t *testing.T) (*publish.LocalStore, *publish.LocalPublicationStore, string) {
	t.Helper()
	root := t.TempDir()
	store := &publish.LocalStore{Root: root}
	pub := &publish.LocalPublicationStore{ActiveRoot: filepath.Join(root, "public/active")}
	return store, pub, root
}

func putTestArtifact(t *testing.T, s *publish.LocalStore, html, path string) publish.Locator {
	t.Helper()
	m := baseManifest()
	m.CanonicalPath = path
	a, err := publish.NewArtifact([]byte(html), m)
	if err != nil {
		t.Fatalf("构建产物失败: %v", err)
	}
	loc, err := s.PutArtifact(a)
	if err != nil {
		t.Fatalf("PutArtifact 失败: %v", err)
	}
	return loc
}

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
	if st.Kind != publish.PublicationPage || st.Locator == nil || st.Locator.Key != loc.Key {
		t.Errorf("激活状态错误: %+v", st)
	}

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
	if st.Kind != publish.PublicationNone {
		t.Errorf("取消激活后应为 none: %+v", st)
	}
	if err := pub.Deactivate("/about"); err != nil {
		t.Errorf("重复 Deactivate 应幂等: %v", err)
	}
}

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

	old := filepath.Join(store.Root, loc1.Key, "index.html")
	if data, err := os.ReadFile(old); err != nil || string(data) != "<html>v1</html>" {
		t.Errorf("历史产物应保持不变: %v %q", err, data)
	}
}

func TestLocalPublicationMultiLevel(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	loc := putTestArtifact(t, store, "<html>x</html>", "/products/phone")
	if err := pub.Activate("/products/phone", loc); err != nil {
		t.Fatalf("多级路径激活失败: %v", err)
	}
	st, _ := pub.Inspect("/products/phone")
	if st.Kind != publish.PublicationPage {
		t.Errorf("多级路径状态错误: %+v", st)
	}
}

func TestLocalPublicationRedirectActivate(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	r, _ := publish.NewRedirectArtifact("/new-url", 301)
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
	if st.Kind != publish.PublicationRedirect || st.Redirect == nil ||
		st.Redirect.TargetPath != "/new-url" || st.Redirect.StatusCode != 301 {
		t.Errorf("重定向状态错误: %+v", st)
	}
}

func TestLocalPublicationInspectGuards(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)

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

	link := filepath.Join(pub.ActiveRoot, "occupied")
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatalf("mkdir 失败: %v", err)
	}
	if _, err := pub.Inspect("/occupied"); err == nil {
		t.Error("非 symlink 占位应报错")
	}
}

func TestLocalPublicationConcurrentActivate(t *testing.T) {
	store, pub, _ := newPublicationEnv(t)
	loc1 := putTestArtifact(t, store, "<html>v1</html>", "/hot")
	loc2 := putTestArtifact(t, store, "<html>v2</html>", "/hot")

	const workers = 32
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			loc := loc1
			if i%2 == 1 {
				loc = loc2
			}
			errs[i] = pub.Activate("/hot", loc)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发激活 #%d 失败: %v", i, err)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(pub.ActiveRoot, "*activating*"))
	if err != nil || len(leftovers) != 0 {
		t.Errorf("激活后不应残留临时链接: %v err=%v", leftovers, err)
	}
	st, err := pub.Inspect("/hot")
	if err != nil || st.Kind != publish.PublicationPage {
		t.Fatalf("并发激活后状态错误: %+v err=%v", st, err)
	}
}

func TestLocalPublicationKeepsChildSubtreeOnFailure(t *testing.T) {
	store, pub, root := newPublicationEnv(t)
	child := putTestArtifact(t, store, "<html>child</html>", "/products/phone")
	if err := pub.Activate("/products/phone", child); err != nil {
		t.Fatalf("子路径激活失败: %v", err)
	}
	parent := putTestArtifact(t, store, "<html>parent</html>", "/products")
	if err := pub.Activate("/products", parent); err == nil {
		t.Fatal("父子路径在 FS 上互斥，父路径激活应失败")
	}
	st, err := pub.Inspect("/products/phone")
	if err != nil || st.Kind != publish.PublicationPage || st.Locator == nil || st.Locator.Key != child.Key {
		t.Fatalf("父路径激活失败后子路径激活树被破坏: %+v err=%v", st, err)
	}
	data, rerr := os.ReadFile(filepath.Join(root, "public", "active", "products", "phone", "index.html"))
	if rerr != nil || string(data) != "<html>child</html>" {
		t.Errorf("子路径激活入口应保持可读: %v %q", rerr, data)
	}
}

func lastSlashIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
