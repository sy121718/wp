package pipeline

// 路径归一化行为对照（审计 CQ-012）：同一批输入喂给本包实现，逐条打印结果。
// 收敛前用于确认「哪些是真重复、哪些语义本就不同」；收敛后作为回归基准。

// url.go 的 NormalizeURL 是文档化的唯一口径（docs/03-pipeline.md §5.1）。

import (
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
		got, err := NormalizeURL(in)
		if err != nil {
			t.Logf("NORMALIZE_MATRIX\t%q\t=> ERROR\t%s", in, err)
			continue
		}
		t.Logf("NORMALIZE_MATRIX\t%q\t=> %q", in, got)
	}
}
