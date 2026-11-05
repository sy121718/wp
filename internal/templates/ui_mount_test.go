package templates

import (
	"testing"
	"testing/fstest"

	uipkg "go_wp/pkg/view/ui"
)

// TestUIMountCoversPublicPartials 保证公共 UI 模板能按原逻辑键从 pkg 层打开。
func TestUIMountCoversPublicPartials(t *testing.T) {
	composite, err := newUICompositeFS(fstest.MapFS{})
	if err != nil {
		t.Fatalf("构建复合 FS 失败：%v", err)
	}
	for _, key := range []string{
		"admin/partials/sidebar.html",
		"admin/partials/nav-nodes.html",
		"admin/partials/badge.html",
		"admin/partials/pagination.html",
		"admin/partials/empty_state.html",
		"admin/partials/write_success.html",
		"partials/ui_scripts.html",
	} {
		if _, err := composite.Open(key); err != nil {
			t.Errorf("公共 UI 逻辑键 %s 打不开：%v", key, err)
		}
	}

	// 逻辑键必须与磁盘属主同形：pkg 层就是这些文件的唯一实体。
	for _, key := range []string{"admin/partials/sidebar.html", "partials/ui_scripts.html"} {
		if _, err := uipkg.TemplatesFS().Open(key); err != nil {
			t.Errorf("pkg/view/ui 直接读不到 %s：%v", key, err)
		}
	}
}

// TestUIMountRejectsDuplicateLogicalKeys 守住「每个逻辑键只有一个实体属主」。
func TestUIMountRejectsDuplicateLogicalKeys(t *testing.T) {
	dup := fstest.MapFS{
		"admin/partials/badge.html": &fstest.MapFile{Data: []byte("<span></span>")},
	}
	_, err := newUICompositeFS(dup)
	if err == nil {
		t.Fatal("应用侧与 pkg 侧同名模板必须显式报错，不能静默选一份")
	}
}

// TestUIMountDoesNotShadowPrivateAppTemplates 保证业务模板仍由应用侧提供。
func TestUIMountDoesNotShadowPrivateAppTemplates(t *testing.T) {
	app := fstest.MapFS{
		"admin/product/products.html": &fstest.MapFile{Data: []byte("{{extends \"../layout.html\"}}")},
	}
	composite, err := newUICompositeFS(app)
	if err != nil {
		t.Fatalf("构建复合 FS 失败：%v", err)
	}
	body, err := readAll(composite, "admin/product/products.html")
	if err != nil {
		t.Fatalf("读不到业务模板：%v", err)
	}
	if string(body) != `{{extends "../layout.html"}}` {
		t.Fatalf("业务模板内容被改写：%q", body)
	}
}
