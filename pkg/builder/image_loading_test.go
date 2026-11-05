package builder

import (
	"go_wp/pkg/builder/core"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

func TestImageLoadingAllComponents(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	theme := &ThemeSettings{Images: ThemeImages{LazyLoad: "on", Skeleton: true}}

	cases := []struct {
		name string
		doc  string
		want []string
		not  []string
	}{
		{
			name: "card 默认继承主题",
			doc:  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c1","type":"core.card","props":{"title":"T","imageSrc":"https://media.example.test/storage/a.jpg"}}]}`,
			want: []string{`loading="lazy"`, "is-skeleton"},
		},
		{
			name: "card 关闭懒加载",
			doc:  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c2","type":"core.card","props":{"title":"T","imageSrc":"https://media.example.test/storage/a.jpg","loading":"off"}}]}`,
			want: []string{`loading="eager"`},
			not:  []string{"is-skeleton"},
		},
		{
			name: "gallery 默认懒加载加骨架类",
			doc:  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"g1","type":"core.gallery","props":{"items":[{"url":"https://media.example.test/storage/a.jpg","alt":"A"},{"url":"https://media.example.test/storage/b.jpg","alt":"B"}]}}]}`,
			want: []string{`loading="lazy"`, "gi is-skeleton"},
		},
		{
			name: "infobox 关闭懒加载",
			doc:  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.infobox","props":{"mediaImage":"https://media.example.test/storage/a.jpg","title":"T","loading":"off"}}]}`,
			want: []string{`loading="eager"`},
			not:  []string{"is-skeleton"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := ParsePage([]byte(tc.doc))
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			c, err := Compile(page, WithComponentSet(set), WithThemeSettings(theme))
			if err != nil {
				t.Fatalf("编译失败: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(c.HTML, w) {
					t.Errorf("缺少 %q\n%s", w, c.HTML)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(c.HTML, n) {
					t.Errorf("不应包含 %q\n%s", n, c.HTML)
				}
			}
		})
	}
}

func TestImageSrcsetFromAssetProbe(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()

	probe := func(url string) []core.AssetVariant {
		if url == "https://media.example.test/storage/a.jpg" {
			return []core.AssetVariant{
				{URL: "https://media.example.test/storage/a_thumb-1-ab12cd34.jpg", Width: 320},
				{URL: "https://media.example.test/storage/a_medium-1-ab12cd34.jpg", Width: 1280},
			}
		}
		return nil
	}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"src":"https://media.example.test/storage/a.jpg","alt":"A"}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	c, err := Compile(page, WithComponentSet(set), WithThemeSettings(&ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}), WithAssetProbe(probe))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	want := `srcset="https://media.example.test/storage/a_thumb-1-ab12cd34.jpg 320w, https://media.example.test/storage/a_medium-1-ab12cd34.jpg 1280w"`
	if !strings.Contains(c.HTML, want) {
		t.Errorf("缺少 srcset 候选 %q\n%s", want, c.HTML)
	}
	if !strings.Contains(c.HTML, `sizes="(max-width: 640px) 100vw, 50vw"`) {
		t.Errorf("缺少 sizes\n%s", c.HTML)
	}
}

func TestGalleryPerItemLoading(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	theme := &ThemeSettings{Images: ThemeImages{LazyLoad: "on", Skeleton: true}}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"g2","type":"core.gallery","props":{"loading":"on","items":[{"url":"https://media.example.test/storage/a.jpg","alt":"A"},{"url":"https://media.example.test/storage/b.jpg","alt":"B","loading":"off","fetchPriority":"high"}]}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	c, err := Compile(page, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	if !strings.Contains(c.HTML, `class="gi is-skeleton" src="https://media.example.test/storage/a.jpg" loading="lazy"`) {
		t.Errorf("第一张应继承组件级懒加载与骨架\n%s", c.HTML)
	}

	if !strings.Contains(c.HTML, `class="gi" src="https://media.example.test/storage/b.jpg" loading="eager" fetchpriority="high"`) {
		t.Errorf("第二张应按单图设置立即加载 + 高优先\n%s", c.HTML)
	}
}

func TestCardFetchPriority(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	theme := &ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c9","type":"core.card","props":{"title":"T","imageSrc":"https://media.example.test/storage/a.jpg","loading":"off","fetchPriority":"high"}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	c, err := Compile(page, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, `loading="eager" fetchpriority="high"`) {
		t.Errorf("应输出 fetchpriority=high\n%s", c.HTML)
	}

	doc2 := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c10","type":"core.card","props":{"title":"T","imageSrc":"https://media.example.test/storage/a.jpg"}}]}`
	page2, _ := ParsePage([]byte(doc2))
	c2, err := Compile(page2, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c2.HTML, "fetchpriority") {
		t.Errorf("未设置时不应输出 fetchpriority\n%s", c2.HTML)
	}
}

func TestGalleryCarouselFirstEager(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	theme := &ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"gc1","type":"core.gallery","props":{"mode":"carousel","items":[{"url":"https://media.example.test/storage/a.jpg","alt":"A"},{"url":"https://media.example.test/storage/b.jpg","alt":"B"}]}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	c, err := Compile(page, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, `src="https://media.example.test/storage/a.jpg" loading="eager" fetchpriority="high"`) {
		t.Errorf("轮播首图应默认立即加载 + 高优先\n%s", c.HTML)
	}
	if !strings.Contains(c.HTML, `src="https://media.example.test/storage/b.jpg" loading="lazy" fetchpriority="low"`) {
		t.Errorf("轮播非首图应懒加载 + 低优先\n%s", c.HTML)
	}

	doc2 := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"gc2","type":"core.gallery","props":{"mode":"carousel","carousel":{"firstEagerOff":true},"items":[{"url":"https://media.example.test/storage/a.jpg","alt":"A"}]}}]}`
	page2, _ := ParsePage([]byte(doc2))
	c2, err := Compile(page2, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c2.HTML, `src="https://media.example.test/storage/a.jpg" loading="lazy"`) {
		t.Errorf("关闭首图优先加载后应回落懒加载\n%s", c2.HTML)
	}
}
