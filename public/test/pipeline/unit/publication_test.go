package unit

import (
	"os"
	"path/filepath"
	"sync"
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

// TestLocalPublicationConcurrentActivate 同内容并发激活互不干扰（H8 回归）：
// 临时名含 pid + 进程内唯一序列后，并发 rename 原子竞争，不得因临时链接
// 同名 EEXIST 而失败；完成后不残留 .activating- 临时链接。
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
	if err != nil || st.Kind != pipeline.PublicationPage {
		t.Fatalf("并发激活后状态错误: %+v err=%v", st, err)
	}
}

// TestLocalPublicationKeepsChildSubtreeOnFailure 父路径激活失败不得删除
// 子路径激活树（H9 回归）：/products/phone 已激活时再激活 /products 因
// FS 目录/符号链接互斥而失败，子路径激活必须原样保留。
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
	if err != nil || st.Kind != pipeline.PublicationPage || st.Locator == nil || st.Locator.Key != child.Key {
		t.Fatalf("父路径激活失败后子路径激活树被破坏: %+v err=%v", st, err)
	}
	data, rerr := os.ReadFile(filepath.Join(root, "public", "active", "products", "phone", "index.html"))
	if rerr != nil || string(data) != "<html>child</html>" {
		t.Errorf("子路径激活入口应保持可读: %v %q", rerr, data)
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
