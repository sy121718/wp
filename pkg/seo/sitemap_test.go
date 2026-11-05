package seo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSitemap(t *testing.T) {
	entries := []SitemapEntry{
		{Loc: "https://e.com/b", ChangeFreq: "weekly", Priority: "0.7"},
		{Loc: "https://e.com/a", LastMod: "2026-09-08"},
		{Loc: "https://e.com/b"},
		{Loc: ""},
		{Loc: "https://e.com/x?a=1&b=2"},
	}
	out, err := BuildSitemap(entries)
	if err != nil {
		t.Fatalf("BuildSitemap: %v", err)
	}
	if !strings.HasPrefix(out, "<?xml") {
		t.Fatalf("缺少 XML 声明: %s", out[:40])
	}
	if strings.Count(out, "<url>") != 3 {
		t.Fatalf("应 3 条（b 去重 + 空路径跳过），实际 %d", strings.Count(out, "<url>"))
	}

	if strings.Index(out, "https://e.com/a") > strings.Index(out, "https://e.com/b") {
		t.Fatalf("条目未按 URL 升序")
	}

	if !strings.Contains(out, "&amp;") {
		t.Fatalf("查询参数未转义: %s", out)
	}
	if !strings.Contains(out, "<lastmod>2026-09-08</lastmod>") {
		t.Fatalf("lastmod 未输出")
	}
}

func TestBuildSitemapDeterministic(t *testing.T) {
	in := []SitemapEntry{{Loc: "https://e.com/c"}, {Loc: "https://e.com/a"}, {Loc: "https://e.com/b"}}
	a, _ := BuildSitemap(in)
	b, _ := BuildSitemap(in)
	if a != b {
		t.Fatalf("两次输出不一致")
	}
}

func TestBuildRobots(t *testing.T) {
	out := BuildRobots("https://e.com/", "/sitemap.xml")
	if !strings.Contains(out, "Sitemap: https://e.com/sitemap.xml") {
		t.Fatalf("缺少 Sitemap 行: %s", out)
	}
	if !strings.Contains(out, "Disallow: /admin") {
		t.Fatalf("缺少后台屏蔽")
	}
	if !strings.Contains(out, "User-agent: *") {
		t.Fatalf("缺少 User-agent")
	}

	if strings.Contains(BuildRobots("", ""), "Sitemap:") {
		t.Fatalf("无 baseURL 不应输出 Sitemap")
	}
}

func TestEntriesFromPaths(t *testing.T) {
	out := EntriesFromPaths("https://e.com", []string{"/", "/about", ""})
	if len(out) != 2 {
		t.Fatalf("应 2 条（空路径跳过），实际 %d", len(out))
	}
	if out[0].Priority != "1.0" || out[0].ChangeFreq != "daily" {
		t.Fatalf("根路径应为 1.0/daily，实际 %s/%s", out[0].Priority, out[0].ChangeFreq)
	}
	if out[0].Loc != "https://e.com/" {
		t.Fatalf("根路径 URL 拼接错误: %s", out[0].Loc)
	}
	if out[1].Loc != "https://e.com/about" {
		t.Fatalf("路径拼接错误: %s", out[1].Loc)
	}
}

func TestWriteSiteFiles(t *testing.T) {
	dir := t.TempDir()
	err := WriteSiteFiles(dir, "https://e.com", EntriesFromPaths("https://e.com", []string{"/", "/a"}))
	if err != nil {
		t.Fatalf("WriteSiteFiles: %v", err)
	}
	for _, name := range []string{"sitemap.xml", "robots.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s 未写入: %v", name, err)
		}
	}
	sm, _ := os.ReadFile(filepath.Join(dir, "sitemap.xml"))
	if !strings.Contains(string(sm), "https://e.com/a") {
		t.Fatalf("sitemap 内容缺少条目")
	}
}

func TestJoinURL(t *testing.T) {
	cases := [][3]string{
		{"https://e.com", "/a", "https://e.com/a"},
		{"https://e.com/", "/a", "https://e.com/a"},
		{"https://e.com/", "", "https://e.com/"},
		{"https://e.com", "a", "https://e.com/a"},
	}
	for _, c := range cases {
		if got := JoinURL(c[0], c[1]); got != c[2] {
			t.Errorf("JoinURL(%q,%q) = %q，期望 %q", c[0], c[1], got, c[2])
		}
	}
}
