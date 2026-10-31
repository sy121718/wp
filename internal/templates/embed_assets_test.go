package templates

import (
	"testing"
)

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

	underscore, err := fsys.Open("/js/ui/_util.js")
	if err != nil {
		t.Errorf("js/ui/_util.js 未打进 embed（embed 指令需要 all: 前缀）: %v", err)
	} else {
		_ = underscore.Close()
	}
}

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
