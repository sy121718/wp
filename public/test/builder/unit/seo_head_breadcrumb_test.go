package unit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// 经公开构建入口解析真实 JSON-LD，防止 URL 字节合法但路径层级错误。
func TestSEOHeadBreadcrumbURLs(t *testing.T) {
	for _, tt := range []struct {
		name, url    string
		items, names []string
	}{
		{"站内路径", "/news/post", []string{"/", "/news", "/news/post"}, []string{"Home", "news", "post"}},
		{"绝对路径", "https://example.test/news/post", []string{"https://example.test/", "https://example.test/news", "https://example.test/news/post"}, []string{"Home", "news", "post"}},
		{"查询与锚点", "https://example.test/news/post?next=/other/path#part", []string{"https://example.test/", "https://example.test/news", "https://example.test/news/post"}, []string{"Home", "news", "post"}},
		{"编码路径", "/%E4%B8%AD%E6%96%87/a%2Fb", []string{"/", "/%E4%B8%AD%E6%96%87", "/%E4%B8%AD%E6%96%87/a%2Fb"}, []string{"Home", "中文", "a/b"}},
		{"连续斜杠", "/news//post/", []string{"/", "/news", "/news//post"}, []string{"Home", "news", "post"}},
		{"根路径", "/", nil, nil},
		{"只有主机与查询", "https://example.test?next=/news/post", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := builder.BuildSEOHead(builder.SEO{Title: `标题 </script><script>alert(1)</script>`}, tt.url, "", "", "", nil)
			const start = `<script type="application/ld+json">`
			_, rest, found := strings.Cut(out, start)
			if !found {
				t.Fatal("缺少 JSON-LD")
			}
			payload, _, found := strings.Cut(rest, "</script>")
			if !found {
				t.Fatal("JSON-LD 没有闭合")
			}
			var doc struct {
				Breadcrumb struct {
					Items []struct {
						Position   int
						Name, Item string
					} `json:"itemListElement"`
				} `json:"breadcrumb"`
			}
			if err := json.Unmarshal([]byte(payload), &doc); err != nil {
				t.Fatalf("JSON-LD 无效: %v", err)
			}
			var items, names []string
			for i, crumb := range doc.Breadcrumb.Items {
				if crumb.Position != i+1 {
					t.Errorf("位置不连续: %d", crumb.Position)
				}
				items = append(items, crumb.Item)
				names = append(names, crumb.Name)
			}
			if !reflect.DeepEqual(items, tt.items) {
				t.Errorf("链接 = %q，期望 %q", items, tt.items)
			}
			if !reflect.DeepEqual(names, tt.names) {
				t.Errorf("名称 = %q，期望 %q", names, tt.names)
			}
			if again := builder.BuildSEOHead(builder.SEO{Title: `标题 </script><script>alert(1)</script>`}, tt.url, "", "", "", nil); again != out {
				t.Fatal("同输入输出不确定")
			}
		})
	}
}
