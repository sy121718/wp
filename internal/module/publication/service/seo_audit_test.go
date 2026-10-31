package pubservice

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func auditPageHTML(title, desc, canonical string, extraBody string) string {
	return "<html><head><title>" + title + "</title>" +
		"<meta name=\"description\" content=\"" + desc + "\">" +
		"<link rel=\"canonical\" href=\"" + canonical + "\">" +
		"</head><body>" + extraBody + "</body></html>"
}

func mustParse(t *testing.T, path, src string) AuditDocument {
	t.Helper()
	doc, err := ParseAuditDocument(path, strings.NewReader(src))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return doc
}

func TestAuditSiteDetectsBrokenInternalLink(t *testing.T) {
	doc := mustParse(t, "/about", auditPageHTML("关于", "描述", "/about", `<a href="/gone-page">gone</a>`))
	issues := AuditSite([]AuditDocument{doc}, map[string]bool{"/about": true, "/shop": true})
	found := false
	for _, it := range issues {

		if it.Item == AuditInternalLinkBroken && it.Params.Href == "/gone-page" {
			found = true
			if it.Level != AuditLevelError {
				t.Errorf("死链应是 error 级，实际 %s", it.Level)
			}
			if it.Path != "/about" {
				t.Errorf("死链结论应归属 /about，实际 %q", it.Path)
			}
		}
	}
	if !found {
		t.Fatalf("未检出内链死链，实际结论 %+v", issues)
	}

	ok := mustParse(t, "/about", auditPageHTML("关于", "描述", "/about", `<a href="/shop">shop</a>`))
	for _, it := range AuditSite([]AuditDocument{ok}, map[string]bool{"/about": true, "/shop": true}) {
		if it.Item == AuditInternalLinkBroken {
			t.Errorf("链接目标存在却报了死链: %+v", it)
		}
	}
}

func TestAuditSiteDetectsDuplicateTitle(t *testing.T) {
	a := mustParse(t, "/a", auditPageHTML("同一个标题", "d", "/a", ""))
	b := mustParse(t, "/b", auditPageHTML("同一个标题", "d", "/b", ""))
	c := mustParse(t, "/c", auditPageHTML("独有标题", "d", "/c", ""))
	issues := AuditSite([]AuditDocument{a, b, c}, map[string]bool{"/a": true, "/b": true, "/c": true})
	hits := 0
	for _, it := range issues {
		if it.Item == AuditTitleDuplicate {
			hits++

			if !slices.Equal(it.Params.Paths, []string{"/a", "/b"}) {
				t.Errorf("重复 title 结论应列出全部命中页面 [/a /b]，实际 %v", it.Params.Paths)
			}
			if it.Params.Title != "同一个标题" {
				t.Errorf("重复 title 结论应带标题原值，实际 %q", it.Params.Title)
			}

			if it.Path != "" {
				t.Errorf("重复 title 结论不该有归属页面，实际 %q", it.Path)
			}
		}
	}
	if hits != 1 {
		t.Fatalf("应恰好一条重复 title 结论，实际 %d 条", hits)
	}
}

func TestAuditSiteFlagsMissingHeadFields(t *testing.T) {
	doc := mustParse(t, "/x", "<html><head></head><body></body></html>")
	seen := map[string]bool{}
	for _, it := range AuditSite([]AuditDocument{doc}, map[string]bool{"/x": true}) {
		seen[it.Item] = true
	}
	if !seen[AuditTitleMissing] {
		t.Error("缺 title 应报 title_missing")
	}

	doc2 := mustParse(t, "/y", "<html><head><title>Y</title></head><body></body></html>")
	seen2 := map[string]bool{}
	for _, it := range AuditSite([]AuditDocument{doc2}, map[string]bool{"/y": true}) {
		seen2[it.Item] = true
	}
	if seen2[AuditTitleMissing] {
		t.Error("有 title 时不该报 title_missing")
	}
	if !seen2[AuditDescriptionMissing] || !seen2[AuditCanonicalMissing] {
		t.Errorf("缺 description / canonical 应各报一条，实际 %+v", seen2)
	}
}

func TestCollectActiveDocumentsFollowsActiveSymlinks(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(root, "artifacts", "h1")
	active := filepath.Join(root, "public", "active")
	if err := os.MkdirAll(filepath.Join(artifacts, "about"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "about", "index.html"),
		[]byte(auditPageHTML("关于", "d", "/about", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "index.html"),
		[]byte(auditPageHTML("首页", "d", "/", "")), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(filepath.Join("..", "..", "artifacts", "h1", "about"),
		filepath.Join(active, "about")); err != nil {
		t.Fatalf("建立目录符号链接失败: %v", err)
	}

	if err := os.Symlink(filepath.Join("..", "..", "artifacts", "h1"),
		filepath.Join(active, "index")); err != nil {
		t.Fatalf("建立首页激活链接失败: %v", err)
	}

	docs, err := CollectActiveDocuments(active, []string{"/", "/about", "/gone"})
	if err != nil {
		t.Fatalf("收集失败: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("应穿过符号链接读到 2 份产物（/ 与 /about），实际 %d（为 0 说明又退回成遍历目录了）", len(docs))
	}
	byPath := map[string]AuditDocument{}
	for _, d := range docs {
		byPath[d.Path] = d
	}
	if byPath["/about"].Title != "关于" {
		t.Errorf("/about 应解析出 title，实际 %q", byPath["/about"].Title)
	}
	if byPath["/"].Title != "首页" {
		t.Errorf("/ 应解析出 title，实际 %q", byPath["/"].Title)
	}

	if _, ok := byPath["/gone"]; ok {
		t.Error("没有产物的路径不该出现在结果里")
	}

	empty, eerr := CollectActiveDocuments(filepath.Join(root, "nope"), []string{"/"})
	if eerr != nil || len(empty) != 0 {
		t.Errorf("目录不存在应返回空结果，实际 %v / %v", empty, eerr)
	}

	none, nerr := CollectActiveDocuments(active, nil)
	if nerr != nil || len(none) != 0 {
		t.Errorf("无激活路径应返回空结果，实际 %v / %v", none, nerr)
	}
}

func TestActiveHTMLFileRejectsParentEscape(t *testing.T) {
	if _, ok := activeHTMLFile("/tmp", "/../etc"); ok {
		t.Error("含 .. 的激活路径应被拒绝")
	}
}
