package pipeline

import (
	"go_wp/pkg/publish"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncNotFoundPageWritesAndReads(t *testing.T) {
	active := filepath.Join(t.TempDir(), "public", "active")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	const page = "<!doctype html><h1>页面不存在</h1>"
	if err := publish.SyncNotFoundPage(active, page); err != nil {
		t.Fatalf("写入 404 页失败: %v", err)
	}
	body, ok := publish.ReadNotFoundPage(active)
	if !ok {
		t.Fatal("写入后读不到 404 页（配置等于没生效）")
	}
	if string(body) != page {
		t.Errorf("内容不一致：期望 %q 实际 %q", page, string(body))
	}

	if _, err := os.Stat(publish.NotFoundPagePath(active) + ".tmp"); !os.IsNotExist(err) {
		t.Error("写入后残留 .tmp 临时文件")
	}
}

func TestSyncNotFoundPageEmptyRemovesExisting(t *testing.T) {
	active := filepath.Join(t.TempDir(), "public", "active")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	if err := publish.SyncNotFoundPage(active, "<h1>旧页</h1>"); err != nil {
		t.Fatalf("写入 404 页失败: %v", err)
	}
	if err := publish.SyncNotFoundPage(active, "   "); err != nil {
		t.Fatalf("清空 404 页失败: %v", err)
	}
	if _, ok := publish.ReadNotFoundPage(active); ok {
		t.Error("配置清空后 404 页仍然可读")
	}
	if _, err := os.Stat(publish.NotFoundPagePath(active)); !os.IsNotExist(err) {
		t.Error("配置清空后 404.html 仍然存在（旧页会继续对外服务）")
	}

	if err := publish.SyncNotFoundPage(active, ""); err != nil {
		t.Errorf("重复清空应幂等，实际报错: %v", err)
	}
}

func TestSyncNotFoundPageMissingActiveRootIsNoop(t *testing.T) {
	active := filepath.Join(t.TempDir(), "public", "active")
	if err := publish.SyncNotFoundPage(active, "<h1>x</h1>"); err != nil {
		t.Fatalf("激活目录不存在时应按幂等成功处理，实际报错: %v", err)
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Error("不应为了写 404 页而新建激活目录")
	}
}

func TestReadNotFoundPageEmptyFileNotConfigured(t *testing.T) {
	active := t.TempDir()
	if err := os.WriteFile(publish.NotFoundPagePath(active), nil, 0o644); err != nil {
		t.Fatalf("造空文件失败: %v", err)
	}
	if _, ok := publish.ReadNotFoundPage(active); ok {
		t.Error("空的 404.html 应按未配置处理")
	}
	if _, ok := publish.ReadNotFoundPage(""); ok {
		t.Error("未提供激活目录时应按未配置处理")
	}
}

func TestIsSiteRootFileRootOnly(t *testing.T) {
	for _, name := range []string{"sitemap.xml", "robots.txt", "feed.xml", publish.NotFoundFileName} {
		if !publish.IsSiteRootFile(name) {
			t.Errorf("%q 应属于激活目录根的站点级文件", name)
		}
	}
	for _, rel := range []string{
		"",
		"about",
		"index",
		"docs/" + publish.NotFoundFileName,
		"docs/sitemap.xml",
		"404.html.bak",
	} {
		if publish.IsSiteRootFile(rel) {
			t.Errorf("%q 不应被当成激活目录根的站点级文件", rel)
		}
	}
}
