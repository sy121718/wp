package pipeline

// publication_audit_test.go — AuditActiveLinks 的站点级文件白名单（审计 SEO-013 的附带修复）。
//
// 背景：站点级产物（sitemap.xml / robots.txt / feed.xml / 404.html）只能以**真实文件**
// 落在激活目录根（它们没有对应的站点路径可挂），而审计的判据是「非符号链接即异常」。
// 没有白名单时每次审计都报 4 条假 issue，真正的失联链接会淹没在里面。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAuditActiveLinksSkipsSiteRootFiles 站点级真实文件不报 issue，异常链接照报。
func TestAuditActiveLinksSkipsSiteRootFiles(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "public", "active")
	artifacts := filepath.Join(root, "artifacts", "hash1")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatalf("建产物目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	// 正常激活链接：目标相对激活目录可达（与 Activate 生成的相对链接同形）。
	if err := os.Symlink("../../artifacts/hash1", filepath.Join(active, "index")); err != nil {
		t.Fatalf("建激活链接失败: %v", err)
	}
	// 悬空激活链接：产物被误删的那种静默失联，审计必须报出来。
	if err := os.Symlink("../../artifacts/missing", filepath.Join(active, "ghost")); err != nil {
		t.Fatalf("建悬空链接失败: %v", err)
	}
	// 站点级真实文件：白名单内，不报。
	for _, name := range []string{"sitemap.xml", "robots.txt", "feed.xml", NotFoundFileName} {
		if err := os.WriteFile(filepath.Join(active, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("写站点级文件 %s 失败: %v", name, err)
		}
	}
	// 深层同名文件不是站点级文件：白名单只认根层，这里必须照旧报异常。
	deep := filepath.Join(active, "docs")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("建深层目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deep, NotFoundFileName), []byte("x"), 0o644); err != nil {
		t.Fatalf("写深层文件失败: %v", err)
	}

	store := &LocalPublicationStore{ActiveRoot: active}
	issues, checked, err := store.AuditActiveLinks()
	if err != nil {
		t.Fatalf("审计失败: %v", err)
	}
	// 受检的只有两条激活链接（index / ghost）；站点级文件与深层文件不是「激活链接」。
	if checked != 2 {
		t.Errorf("受检链接数期望 2，实际 %d", checked)
	}
	if len(issues) != 2 {
		t.Fatalf("issue 数期望 2（悬空链接 + 深层非链接文件），实际 %d: %+v", len(issues), issues)
	}
	var sawDangling, sawDeep bool
	for _, is := range issues {
		switch {
		case strings.Contains(is.Reason, "链接目标不可达"):
			sawDangling = true
		case strings.Contains(is.Reason, "非符号链接") && strings.HasSuffix(filepath.ToSlash(is.Link), "docs/"+NotFoundFileName):
			sawDeep = true
		}
	}
	if !sawDangling {
		t.Errorf("悬空激活链接未被报出（审计对该异常失明）: %+v", issues)
	}
	if !sawDeep {
		t.Errorf("深层同名真实文件未被报出（白名单越界到了子目录）: %+v", issues)
	}
	for _, is := range issues {
		for _, name := range []string{"sitemap.xml", "robots.txt", "feed.xml", NotFoundFileName} {
			if !strings.Contains(filepath.ToSlash(is.Link), "/docs/") && strings.HasSuffix(filepath.ToSlash(is.Link), "/"+name) {
				t.Errorf("站点级文件 %s 被误报为异常: %+v", name, is)
			}
		}
	}
}

// TestAuditActiveLinksNeverPublished 从未发布过（激活目录不存在）时零 issue 零错误。
func TestAuditActiveLinksNeverPublished(t *testing.T) {
	store := &LocalPublicationStore{ActiveRoot: filepath.Join(t.TempDir(), "public", "active")}
	issues, checked, err := store.AuditActiveLinks()
	if err != nil {
		t.Fatalf("未发布过不该报错: %v", err)
	}
	if len(issues) != 0 || checked != 0 {
		t.Errorf("未发布过应零 issue，实际 issues=%d checked=%d", len(issues), checked)
	}
}
