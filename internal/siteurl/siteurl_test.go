package siteurl

import "testing"

func TestDetailPath(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		slug     string
		id       string
		patterns map[string]string
		want     string
	}{
		{"默认模式-文章", KindArticle, "hello-world", "a1", nil, "/blog/hello-world"},
		{"默认模式-商品", KindProduct, "tee", "p1", nil, "/products/tee"},
		{"站点覆盖优先", KindArticle, "hello", "a1", map[string]string{KindArticle: "/news/{slug}"}, "/news/hello"},
		{"空配置回落默认", KindArticle, "hello", "a1", map[string]string{KindArticle: "   "}, "/blog/hello"},
		{"slug 为空回落 id", KindArticle, "", "a1", nil, "/blog/a1"},
		{"id 占位符", KindProduct, "tee", "pid-9", map[string]string{KindProduct: "/p/{id}"}, "/p/pid-9"},
		{"slug 与 id 同时用", KindProduct, "tee", "pid-9", map[string]string{KindProduct: "/p/{id}-{slug}"}, "/p/pid-9-tee"},
		{"未知类型没有模式", "unknown", "x", "y", nil, ""},
		{"配置里多打的斜杠会被折叠", KindProduct, "tee", "", map[string]string{KindProduct: "products//{slug}/"}, "/products/tee"},
		{"中文 slug 原样保留", KindArticle, "你好", "a1", nil, "/blog/你好"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetailPath(tc.kind, tc.slug, tc.id, tc.patterns); got != tc.want {
				t.Fatalf("DetailPath(%q, %q, %q) = %q，期望 %q", tc.kind, tc.slug, tc.id, got, tc.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"   ":      "",
		"about":    "/about",
		"/about":   "/about",
		"/about/":  "/about",
		"//a//b//": "/a/b",
		"/blog/x/": "/blog/x",
		"/blog//x": "/blog/x",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	ok := []string{"/blog/{slug}", "/products/{slug}", "/p/{id}", "/news/{slug}/detail", "/a.b/{slug}"}
	for _, p := range ok {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("合法模式 %q 被拒：%v", p, err)
		}
	}
	bad := map[string]string{
		"空":      "",
		"不以斜杠开头": "blog/{slug}",
		"含查询串":   "/blog/{slug}?x=1",
		"含锚点":    "/blog/{slug}#a",
		"含空格":    "/blog post/{slug}",
		"含中文":    "/博客/{slug}",
		"没有占位符":  "/blog/all",
		"过长":     "/" + string(make([]byte, MaxPatternLen)) + "{slug}",
	}
	for name, p := range bad {
		if err := ValidatePattern(p); err == nil {
			t.Errorf("%s 的模式 %q 应被拒", name, p)
		}
	}
}

// TestDefaultPatternsCoverKnownKinds 默认模式必须覆盖所有内置类型 ——
// 少一个的表现是"这个类型永远派生不出路径"，而调用方只会看到一个空表单。
func TestDefaultPatternsCoverKnownKinds(t *testing.T) {
	for _, k := range KnownKinds {
		if PatternOf(k.Kind, nil) == "" {
			t.Errorf("类型 %s 没有默认模式", k.Kind)
		}
		if err := ValidatePattern(PatternOf(k.Kind, nil)); err != nil {
			t.Errorf("类型 %s 的默认模式自身不合法：%v", k.Kind, err)
		}
	}
}
