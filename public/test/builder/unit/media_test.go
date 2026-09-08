// Package unit 构建器单元测试：core.image 组件的编译闭环与属性校验。
// 本文件同时提供 mustParse 共享解析辅助（被同包多数组件测试复用）。
package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// TestImageCompilePipeline 数据流闭环：Page Document（仅 src URL）→ 编译 → <img> 直出。
func TestImageCompilePipeline(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pic","type":"core.image","props":{"src":"/storage/hero.jpg","alt":"局部覆盖","title":"标题"}}]}`
	c, err := compile(t, mustParse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, want := range []string{
		`<img src="/storage/hero.jpg"`,
		`loading="lazy"`,
		`decoding="async"`,
		`alt="局部覆盖"`,
		`title="标题"`,
	} {
		if !strings.Contains(c.HTML, want) {
			t.Errorf("HTML 缺少 %q\n实际: %s", want, c.HTML)
		}
	}
}

// TestImageValidateProps 图片组件校验：非法地址/叶子约束。
func TestImageValidateProps(t *testing.T) {
	cases := []struct{ name, json, want string }{
		{"非法地址", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"src":"a b"}}]}`, "媒体值非法"},
		{"图片带子节点", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"src":"/x.jpg"},"children":[{"id":"c1","type":"core.image","props":{"src":"/y.jpg"}}]}]}`, "叶子节点"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, mustParse(t, tc.json))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

// mustParse 解析页面文档（失败即 Fatal）。
func mustParse(t *testing.T, jsonStr string) *builder.Page {
	t.Helper()
	p, err := builder.ParsePage([]byte(jsonStr))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return p
}
