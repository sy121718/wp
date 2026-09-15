package pipeline

// notfound_test.go — 站点自定义 404 页的读写与站点级文件白名单（审计 SEO-013）。

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyncNotFoundPageWritesAndReads 配置写入 → 激活根可读，内容逐字一致。
func TestSyncNotFoundPageWritesAndReads(t *testing.T) {
	active := filepath.Join(t.TempDir(), "public", "active")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	const page = "<!doctype html><h1>页面不存在</h1>"
	if err := SyncNotFoundPage(active, page); err != nil {
		t.Fatalf("写入 404 页失败: %v", err)
	}
	body, ok := ReadNotFoundPage(active)
	if !ok {
		t.Fatal("写入后读不到 404 页（配置等于没生效）")
	}
	if string(body) != page {
		t.Errorf("内容不一致：期望 %q 实际 %q", page, string(body))
	}
	// 临时文件不得残留：它会被下一次 WalkDir 当成异常占位。
	if _, err := os.Stat(NotFoundPagePath(active) + ".tmp"); !os.IsNotExist(err) {
		t.Error("写入后残留 .tmp 临时文件")
	}
}

// TestSyncNotFoundPageEmptyRemovesExisting 空配置 = 删除既有文件，而不是不写。
//
// 激活目录直接对外服务：不删等于继续把已经下线的旧页面当 404 响应体返回，
// 且后台看起来「已经清空了」—— 这是本条与 feed 关闭即删除同一约定的理由。
func TestSyncNotFoundPageEmptyRemovesExisting(t *testing.T) {
	active := filepath.Join(t.TempDir(), "public", "active")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	if err := SyncNotFoundPage(active, "<h1>旧页</h1>"); err != nil {
		t.Fatalf("写入 404 页失败: %v", err)
	}
	if err := SyncNotFoundPage(active, "   "); err != nil {
		t.Fatalf("清空 404 页失败: %v", err)
	}
	if _, ok := ReadNotFoundPage(active); ok {
		t.Error("配置清空后 404 页仍然可读")
	}
	if _, err := os.Stat(NotFoundPagePath(active)); !os.IsNotExist(err) {
		t.Error("配置清空后 404.html 仍然存在（旧页会继续对外服务）")
	}
	// 幂等：再次清空不报错。
	if err := SyncNotFoundPage(active, ""); err != nil {
		t.Errorf("重复清空应幂等，实际报错: %v", err)
	}
}

// TestSyncNotFoundPageMissingActiveRootIsNoop 从未发布过（激活目录不存在）不报错。
//
// 激活目录由首次发布创建；站点还没发布就有 404 配置是正常状态，
// 不该让发布链在这种可自愈的状态上失败。
func TestSyncNotFoundPageMissingActiveRootIsNoop(t *testing.T) {
	active := filepath.Join(t.TempDir(), "public", "active")
	if err := SyncNotFoundPage(active, "<h1>x</h1>"); err != nil {
		t.Fatalf("激活目录不存在时应按幂等成功处理，实际报错: %v", err)
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Error("不应为了写 404 页而新建激活目录")
	}
}

// TestReadNotFoundPageEmptyFileNotConfigured 空文件视为未配置。
//
// 返回空 HTML 的后果是死链页白屏 —— 比默认 404 提示更糟，所以按未配置处理。
func TestReadNotFoundPageEmptyFileNotConfigured(t *testing.T) {
	active := t.TempDir()
	if err := os.WriteFile(NotFoundPagePath(active), nil, 0o644); err != nil {
		t.Fatalf("造空文件失败: %v", err)
	}
	if _, ok := ReadNotFoundPage(active); ok {
		t.Error("空的 404.html 应按未配置处理")
	}
	if _, ok := ReadNotFoundPage(""); ok {
		t.Error("未提供激活目录时应按未配置处理")
	}
}

// TestIsSiteRootFileRootOnly 站点级文件白名单只认激活目录根的那几个名字。
func TestIsSiteRootFileRootOnly(t *testing.T) {
	for _, name := range []string{"sitemap.xml", "robots.txt", "feed.xml", NotFoundFileName} {
		if !IsSiteRootFile(name) {
			t.Errorf("%q 应属于激活目录根的站点级文件", name)
		}
	}
	for _, rel := range []string{
		"",
		"about",
		"index",
		"docs/" + NotFoundFileName, // 深层同名不是站点级文件
		"docs/sitemap.xml",
		"404.html.bak",
	} {
		if IsSiteRootFile(rel) {
			t.Errorf("%q 不应被当成激活目录根的站点级文件", rel)
		}
	}
}
