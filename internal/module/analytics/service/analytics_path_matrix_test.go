package analyticsservice

// 路径归一化行为对照（审计 CQ-012）：同一批输入喂给本包实现，逐条打印结果。
// 收敛前用于确认「哪些是真重复、哪些语义本就不同」；收敛后作为回归基准。

// analytics_collect.go 的 sanitizeTrackPath 语义不同：打点上报路径清洗 + 截断。
// 审计 CQ-012 的结论是「保留但改名」，因为它的输入域（访客可控自由文本）与
// 路由路径（写入前必须拒绝畸形输入）根本不同 —— 合并会把「不拒绝」暴露到路由侧。

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
