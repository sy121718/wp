package pipeline

import (
	"go_wp/pkg/publish"
	"strings"
	"testing"
)

var urlNormalizeMatrix = []string{
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

func TestNormalizeURLContract(t *testing.T) {
	for _, in := range urlNormalizeMatrix {
		got, err := publish.NormalizeURL(in)
		if err != nil {
			t.Logf("NORMALIZE_MATRIX\t%q\t=> ERROR\t%s", in, err)
			continue
		}
		t.Logf("NORMALIZE_MATRIX\t%q\t=> %q", in, got)
	}
}
