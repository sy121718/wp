package pageservice

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
