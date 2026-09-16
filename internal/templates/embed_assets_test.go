package templates

// embed_assets_test.go — 单二进制资源出口的回归测试（审计 OSS-018）。
//
// 钉两件事：模板名与磁盘布局一致、静态资产能从 embed 取到且目录列表仍被禁用。

import (
	"testing"
)

// TestAdminTemplateNamesMatchDiskLayout 后台模板名必须与磁盘 loader 解析结果一致。
//
// 这条钉的是一个只在「切换模板来源」时才暴露的坑：embed loader 若把根抬到 admin/ 之下，
// 模板名会从 admin/login.html 变成 login.html，生产模式下找不到模板 ——
// 而失败形态是**页面空体 + 状态码 200**，从外部看一切正常。
func TestAdminTemplateNamesMatchDiskLayout(t *testing.T) {
	loader, err := newAdminTemplateLoader()
	if err != nil {
		t.Fatalf("构建 embed loader 失败: %v", err)
	}
	for _, name := range []string{"admin/login.html", "admin/layout.html", "admin/dashboard.html"} {
		if !loader.Exists(name) {
			t.Errorf("模板 %s 在 embed loader 里不存在（名字与磁盘路径不一致）", name)
		}
	}
	if loader.Exists("login.html") {
		t.Error("embed loader 的根被抬到了 admin/ 之下：模板名与磁盘路径对不上")
	}
}

// TestEmbeddedStaticFSServesAssets 生产模式的 /static 能取到真实资产。
//
// 请求路径形态对齐 http.FileServer 的调用方式：StripPrefix 之后是 /css/ui.css。
func TestEmbeddedStaticFSServesAssets(t *testing.T) {
	fsys, err := EmbeddedStaticFS()
	if err != nil {
		t.Fatalf("构建 embed 静态文件系统失败: %v", err)
	}
	file, err := fsys.Open("/css/ui.css")
	if err != nil {
		t.Fatalf("embed 中读不到 /css/ui.css: %v", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	if info.Size() == 0 {
		t.Error("css/ui.css 大小为 0")
	}

	// all: 前缀不能丢：go:embed 默认忽略以 _ 与 . 开头的文件，
	// 而 static/js/ui/_util.js 正是下划线开头 —— 漏掉它不报错，只在生产模式多一个 404。
	underscore, err := fsys.Open("/js/ui/_util.js")
	if err != nil {
		t.Errorf("js/ui/_util.js 未打进 embed（embed 指令需要 all: 前缀）: %v", err)
	} else {
		_ = underscore.Close()
	}
}

// TestEmbeddedStaticFSRejectsDirListing 目录列表仍被禁用。
//
// 原本由 gin.Dir(dir, false) 保证（审计 Low：目录列表开启）；换成 embed 不能把这条丢掉，
// 否则 /static 会把资产清单列给访客。
func TestEmbeddedStaticFSRejectsDirListing(t *testing.T) {
	fsys, err := EmbeddedStaticFS()
	if err != nil {
		t.Fatalf("构建 embed 静态文件系统失败: %v", err)
	}
	dir, err := fsys.Open("/css")
	if err != nil {
		t.Skipf("embed 中没有 css 目录条目，跳过: %v", err)
	}
	defer func() { _ = dir.Close() }()
	if _, err := dir.Readdir(0); err == nil {
		t.Error("目录列表未被禁用：与 gin.Dir(dir, false) 的语义不一致")
	}
}
