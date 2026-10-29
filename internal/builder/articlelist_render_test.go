package builder

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

type articleListRenderSource struct{ empty bool }

func (s articleListRenderSource) ResolveCollection(context.Context, string, map[string]string) ([]map[string]any, error) {
	if s.empty {
		return nil, nil
	}
	return []map[string]any{{"title": "第一篇", "slug": "first", "excerpt": "第一摘要"}, {"title": "第二篇", "slug": "second"}}, nil
}

func TestArticleListPublishedHTMLUsesDeclaredViewFieldsInsideRange(t *testing.T) {
	page, err := ParsePage([]byte(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"articles","type":"core.articleList","props":{"collectionSource":"content:article","label":"继续阅读 & 了解","titleTag":"h3","linkPrefix":"/notes"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatal(err)
	}
	for _, empty := range []bool{false, true} {
		compiled, err := Compile(page, WithComponentSet(set), WithCollectionResolver(articleListRenderSource{empty}), WithCompileMode(CompileModePublish))
		if err != nil {
			t.Fatal(err)
		}
		if empty {
			if !strings.Contains(compiled.HTML, `class="sky-article-list-empty">暂无文章</p>`) || strings.Contains(compiled.HTML, "sky-article-list-card") {
				t.Fatalf("空集合必须是现有空态: %s", compiled.HTML)
			}
			continue
		}
		for _, want := range []string{`<h3 class="sky-article-list-title"><a href="/notes/first">第一篇</a></h3>`, `<h3 class="sky-article-list-title"><a href="/notes/second">第二篇</a></h3>`} {
			if !strings.Contains(compiled.HTML, want) {
				t.Fatalf("卡片范围中的标题/链接必须消费同一View: 缺%s\n%s", want, compiled.HTML)
			}
		}
		if strings.Count(compiled.HTML, `继续阅读 &amp; 了解</a>`) != 2 || strings.Count(compiled.HTML, `class="sky-article-list-excerpt"`) != 1 {
			t.Fatalf("每张有链接卡片必须输出转义后的声明文案，仅有摘要卡片输出摘要: %s", compiled.HTML)
		}
	}
}
