package pubservice

// seo_audit_test.go — 产物体检（审计 SEO-019）。
//
// 钉住的是 verification 的三条：故意造的内链死链要被检出、重复 title 要被检出、
// 体检只读产物文件（不写库、不发请求）—— 第三条用「遍历一个临时目录」来体现。

import (
	"os"
	"path/filepath"
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

// TestAuditSiteDetectsBrokenInternalLink 内链指向未激活路径 → error 级结论。
func TestAuditSiteDetectsBrokenInternalLink(t *testing.T) {
	doc := mustParse(t, "/about", auditPageHTML("关于", "描述", "/about", `<a href="/gone-page">gone</a>`))
	issues := AuditSite([]AuditDocument{doc}, map[string]bool{"/about": true, "/shop": true})
	found := false
	for _, it := range issues {
		if it.Item == AuditInternalLinkBroken && strings.Contains(it.Message, "/gone-page") {
			found = true
			if it.Level != AuditLevelError {
				t.Errorf("死链应是 error 级，实际 %s", it.Level)
			}
		}
	}
	if !found {
		t.Fatalf("未检出内链死链，实际结论 %+v", issues)
	}

	// 反例：链接目标已激活时不该报 —— 否则这条检查会变成噪音，然后被忽略。
	ok := mustParse(t, "/about", auditPageHTML("关于", "描述", "/about", `<a href="/shop">shop</a>`))
	for _, it := range AuditSite([]AuditDocument{ok}, map[string]bool{"/about": true, "/shop": true}) {
		if it.Item == AuditInternalLinkBroken {
			t.Errorf("链接目标存在却报了死链: %+v", it)
		}
	}
}

// TestAuditSiteDetectsDuplicateTitle 重复 title → 一条 warning，列出全部命中页面。
func TestAuditSiteDetectsDuplicateTitle(t *testing.T) {
	a := mustParse(t, "/a", auditPageHTML("同一个标题", "d", "/a", ""))
	b := mustParse(t, "/b", auditPageHTML("同一个标题", "d", "/b", ""))
	c := mustParse(t, "/c", auditPageHTML("独有标题", "d", "/c", ""))
	issues := AuditSite([]AuditDocument{a, b, c}, map[string]bool{"/a": true, "/b": true, "/c": true})
	hits := 0
	for _, it := range issues {
		if it.Item == AuditTitleDuplicate {
			hits++
			if !strings.Contains(it.Message, "/a") || !strings.Contains(it.Message, "/b") {
				t.Errorf("重复 title 结论应列出全部命中页面，实际 %q", it.Message)
			}
			if strings.Contains(it.Message, "/c") {
				t.Errorf("标题唯一的页面不该出现在结论里: %q", it.Message)
			}
		}
	}
	if hits != 1 {
		t.Fatalf("应恰好一条重复 title 结论，实际 %d 条", hits)
	}
}

// TestAuditSiteFlagsMissingHeadFields 缺 title / description / canonical 各有对应结论。
func TestAuditSiteFlagsMissingHeadFields(t *testing.T) {
	doc := mustParse(t, "/x", "<html><head></head><body></body></html>")
	seen := map[string]bool{}
	for _, it := range AuditSite([]AuditDocument{doc}, map[string]bool{"/x": true}) {
		seen[it.Item] = true
	}
	if !seen[AuditTitleMissing] {
		t.Error("缺 title 应报 title_missing")
	}

	// 有 title 但缺 description / canonical：只报后两项，不误报 title。
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

// TestCollectActiveDocumentsFollowsActiveSymlinks 按激活路径直读，且能穿过符号链接。
//
// 这个测试是**用真实形态**写的，因为最初那版用 t.TempDir() 造的是普通目录，
// 于是全绿 —— 而生产的激活目录里放的全是「指向 artifacts/{hash} 的目录符号链接」。
// filepath.Walk 用 Lstat 不下沉进目录符号链接，扫描结果恒为 0，体检永远报「无问题」；
// 报告里 scanned=0 看起来像「站点还没有产物」，坏了也不响。
//
// 所以这里照访问面的真实形状搭：artifacts/<hash>/about/index.html +
// public/active/about 是指向它的符号链接。测试必须能穿过链接看到文件。
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
	// 激活目录里的条目是**目录符号链接**（与 LocalPublicationStore 的 relActivePath 同形状）。
	if err := os.Symlink(filepath.Join("..", "..", "artifacts", "h1", "about"),
		filepath.Join(active, "about")); err != nil {
		t.Fatalf("建立目录符号链接失败: %v", err)
	}
	// 首页的激活条目名是 **index**（pipeline.relActivePath 对空路径返回 "index"），
	// 它同样是指向 artifacts 目录的符号链接 —— 不是 index.html 这个文件名。
	// 这一点必须照真源写：自己发明一套形状的测试，绿了也不代表线上对。
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

	// 激活路径在目录里没有产物（如只有一条 301 重定向）时跳过，而不是报错或空手而归。
	if _, ok := byPath["/gone"]; ok {
		t.Error("没有产物的路径不该出现在结果里")
	}

	// 目录不存在时返回空结果而不是错误：体检是可选能力，没配目录不该让调用方失败。
	empty, eerr := CollectActiveDocuments(filepath.Join(root, "nope"), []string{"/"})
	if eerr != nil || len(empty) != 0 {
		t.Errorf("目录不存在应返回空结果，实际 %v / %v", empty, eerr)
	}
	// 没有激活路径时也不报错（站点还没发布任何页面）。
	none, nerr := CollectActiveDocuments(active, nil)
	if nerr != nil || len(none) != 0 {
		t.Errorf("无激活路径应返回空结果，实际 %v / %v", none, nerr)
	}
}

// TestActiveHTMLFileRejectsParentEscape 路径里带 .. 一律拒绝（最后一道闸）。
func TestActiveHTMLFileRejectsParentEscape(t *testing.T) {
	if _, ok := activeHTMLFile("/tmp", "/../etc"); ok {
		t.Error("含 .. 的激活路径应被拒绝")
	}
}
