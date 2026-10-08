package templates

import (
	"strings"
	"testing"
)

func TestRedirectCreateDrawer(t *testing.T) {
	out := renderAdminEmptyProbe(t, "admin/page/page_redirects", redirectProbeData(nil))
	for _, want := range []string{
		`data-drawer-open="#tpl-redirect-create"`,
		`<template id="tpl-redirect-create">`,
		`data-drawer-mask hidden`,
		`data-drawer-body`,
		`/static/js/ui/drawer.js`,
		`/static/js/ui/confirm.js`,
		`method="post" action="/api/page/redirect/create?project=pr1"`,
		`name="csrf_token"`,
		`name="project" value="pr1"`,
		`for="redirect-source"`,
		`for="redirect-target"`,
		`data-drawer-close`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("新建抽屉缺少 %q", want)
		}
	}
	if strings.Contains(out, `class="filter-bar"`) {
		t.Error("创建写表单仍伪装成筛选栏")
	}
	form := out[strings.Index(out, `<form method="post" action="/api/page/redirect/create?project=pr1"`):]
	form = form[:strings.Index(form, "</form>")]
	if strings.Contains(form, "data-confirm") {
		t.Error("普通创建误加二次确认")
	}
	if !strings.Contains(out, `data-confirm-danger`) || !strings.Contains(out, `action="/admin/page-redirects/bulk-delete?project=pr1"`) {
		t.Error("删除确认或原有批量删除路由丢失")
	}
}

func TestRedirectCreateFailureEcho(t *testing.T) {
	out := renderAdminEmptyProbe(t, "admin/page/page_redirects", redirectProbeData(map[string]any{
		"ErrKey": "admin.redirect.err.invalid", "CreateFailed": true,
		"CreateSource": `/old"<`, "CreateTarget": "/new",
	}))
	for _, want := range []string{`role="alert"`, `value="/old&#34;&lt;"`, `value="/new"`,
		`document.addEventListener('DOMContentLoaded'`, `opener.click();`} {
		if !strings.Contains(out, want) {
			t.Errorf("失败后错误回显/抽屉重开缺少 %q", want)
		}
	}
	if strings.Contains(out, `value="/old"<"`) {
		t.Error("源路径回显未做 HTML 转义")
	}
	withoutProject := renderAdminEmptyProbe(t, "admin/page/page_redirects", redirectProbeData(map[string]any{
		"Projects": []any{}, "SelectedProject": "", "CreateFailed": true,
		"ErrKey": "admin.redirect.err.invalid",
	}))
	if strings.Contains(withoutProject, `<template id="tpl-redirect-create">`) || strings.Contains(withoutProject, `/api/page/redirect/create`) {
		t.Error("没有工程时不应渲染可提交的创建表单")
	}
	if !strings.Contains(withoutProject, `role="alert"`) {
		t.Error("没有工程时仍应展示创建失败提示")
	}
}
