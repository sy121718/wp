package analyticsservice

import (
	"strings"
	"testing"
)

var analyticsPathMatrix = []string{
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
	"/a\\b",
	"/a\tb",
	"/中文/路径",
	"/a b",
	"/" + strings.Repeat("x", 499),
	"/" + strings.Repeat("x", 500),
}

func TestAnalyticsNormalizePathMatrix(t *testing.T) {
	for _, in := range analyticsPathMatrix {
		got := sanitizeTrackPath(in)
		t.Logf("NORMALIZE_MATRIX\t%q\t=> %q", in, got)
	}
}
