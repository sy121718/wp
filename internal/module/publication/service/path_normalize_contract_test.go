package pubservice

// path_normalize_contract_test.go — 路由路径归一化的行为契约（审计 CQ-012）。
//
// 收敛前这里打印「publication 自己的去尾斜杠实现」的行为矩阵，用于与 pipeline /
// dashboard / nav / analytics 对照（对照结论：publication 与 pipeline 有两处**真实
// 分歧** —— "/a//b" 被原样接受、"/index" 不归一为根路径）。
// 收敛后本包不再有自己的实现，测试改为断言 normalizePath 与 pkg/pathkit 的
// 接受/拒绝边界逐条一致，并保留矩阵输出作为对照记录。

import (
	"strings"
	"testing"

	"go_wp/pkg/pathkit"
)

var publicationPathMatrix = []string{
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

func TestPublicationNormalizePathMatrix(t *testing.T) {
	for _, in := range publicationPathMatrix {
		got, err := normalizePath(in)
		want, wantErr := pathkit.NormalizeRoutePath(in)
		if (err != nil) != (wantErr != nil) {
			t.Errorf("normalizePath(%q) 的接受/拒绝与 pkg/pathkit 不一致：err=%v pathkit=%v", in, err, wantErr)
			continue
		}
		if err == nil && got != want {
			t.Errorf("normalizePath(%q) = %q，pkg/pathkit 得到 %q", in, got, want)
		}
		if err != nil {
			t.Logf("NORMALIZE_MATRIX\t%q\t=> ERROR\t%s", in, err)
			continue
		}
		t.Logf("NORMALIZE_MATRIX\t%q\t=> %q", in, got)
	}
}
