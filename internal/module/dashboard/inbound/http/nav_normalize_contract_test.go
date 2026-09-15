package dashboardhttp

// nav_normalize_contract_test.go — 后台导航路径匹配键的行为契约（审计 CQ-012）。
//
// 收敛前这里打印「本包自己的去尾斜杠实现」的行为矩阵用于与另外几处对照；
// 收敛后本包不再有自己的实现，测试改为钉住**入口函数 navPathFor 的输出 ==
// pkg/pathkit.MatchKey**（除别名表命中的固定几个子页面外），并保留矩阵输出作为
// 对照记录。

import (
	"strings"
	"testing"

	"go_wp/pkg/pathkit"
)

var dashboardNavPathMatrix = []string{
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

func TestDashboardNormalizePathMatrix(t *testing.T) {
	for _, in := range dashboardNavPathMatrix {
		key := pathkit.MatchKey(in)
		got := navPathFor(in)
		if _, isAlias := navPathAlias[key]; !isAlias && got != key {
			t.Errorf("navPathFor(%q) = %q，期望与匹配键一致（%q）", in, got, key)
		}
		t.Logf("NORMALIZE_MATRIX	%q	=> %q", in, got)
	}
}
