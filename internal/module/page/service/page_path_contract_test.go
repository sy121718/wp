package pageservice

// page_path_contract_test.go — 页面路径归一化的行为契约（审计 CQ-012）。
//
// 本处（normalizePagePath）是六处里**最早收敛**的一处：它一直就是
// pipeline.NormalizeURL 的薄包装（把错误映射成 ErrInvalidPath 给运营看）。
// 这里把它钉住，证明「页面前台写入口径 == 发布管道口径」，其余调用点收敛后
// 也必须与它逐条一致。

import (
	"strings"
	"testing"

	"go_wp/internal/pipeline"
)

var pagePathMatrix = []string{
	"",
	"/",
	"//",
	"/a",
	"/a/",
	"/a//b",
	"/a///",
	"a",
	" /a",
	"/a ",
	"/A/",
	"/a?x=1",
	"/a#frag",
	"/a/?x=1",
	"/../a",
	"/a/../b",
	"/a/./b",
	"/a/..",
	"/a/.",
	"/index",
	"/index.html",
	"/index/",
	"/%2e%2e/x",
	"/%2E%2E/x",
	"/%2e/x",
	"/admin",
	"/admin/x",
	"/api/x",
	"/_fragments/x",
	"/assets/x",
	"/objects/x",
	"/a\b",
	"/a	b",
	"/中文/路径",
	"/a b",
	"/" + strings.Repeat("x", 499),
	"/" + strings.Repeat("x", 500),
}

func TestPageNormalizePagePathMatrix(t *testing.T) {
	for _, in := range pagePathMatrix {
		got, err := normalizePagePath(in)
		want, wantErr := pipeline.NormalizeURL(in)
		if (err != nil) != (wantErr != nil) {
			t.Errorf("normalizePagePath(%q) 的接受/拒绝与 pipeline.NormalizeURL 不一致：err=%v pipeline=%v", in, err, wantErr)
			continue
		}
		if err == nil && got != want {
			t.Errorf("normalizePagePath(%q) = %q，pipeline.NormalizeURL 得到 %q", in, got, want)
		}
		if err != nil {
			t.Logf("NORMALIZE_MATRIX\t%q\t=> ERROR\t%s", in, err)
			continue
		}
		t.Logf("NORMALIZE_MATRIX\t%q\t=> %q", in, got)
	}
}
