package builder

// image_loading_test.go — 所有输出 <img> 的组件共享同一套懒加载三态（docs/09 §3 图片优化）。
import (
	"strings"
	"testing"

	mediacontract "go_wp/internal/module/media/contract"
	"go_wp/internal/templates"
)

// TestImageLoadingAllComponents 组件级三态（空=继承主题 / on=强制懒加载 / off=强制立即加载）
// 在 card、gallery、infobox 上与 core.image 行为一致。
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
			doc:  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c1","type":"core.card","props":{"title":"T","imageSrc":"/storage/a.jpg"}}]}`,
			want: []string{`loading="lazy"`, "is-skeleton"},
		},
		{
			name: "card 关闭懒加载",
			doc:  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c2","type":"core.card","props":{"title":"T","imageSrc":"/storage/a.jpg","loading":"off"}}]}`,
			want: []string{`loading="eager"`},
			not:  []string{"is-skeleton"},
		},
		{
			name: "gallery 默认懒加载加骨架类",
			doc:  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"g1","type":"core.gallery","props":{"items":[{"url":"/storage/a.jpg","alt":"A"},{"url":"/storage/b.jpg","alt":"B"}]}}]}`,
			want: []string{`loading="lazy"`, "gi is-skeleton"},
		},
		{
			name: "infobox 关闭懒加载",
			doc:  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.infobox","props":{"mediaImage":"/storage/a.jpg","title":"T","loading":"off"}}]}`,
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

// TestImageSrcsetFromAssetProbe 媒体变体存在时输出 srcset/sizes（构建期探测，访客零查询）。
func TestImageSrcsetFromAssetProbe(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	// 伪候选故意用**与 media 模块真实命名同形**的名字
	// （<stem>_<type>-<generation>-<hash8>.jpg）：用占位名会让「命名约定一变、
	// 这里仍然绿」—— 断言就失去了守住约定的能力。
	probe := func(url string) []mediacontract.VariantRef {
		if url == "/storage/a.jpg" {
			return []mediacontract.VariantRef{
				{URL: "/storage/a_thumb-1-ab12cd34.jpg", Width: 320},
				{URL: "/storage/a_medium-1-ab12cd34.jpg", Width: 1280},
			}
		}
		return nil
	}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"src":"/storage/a.jpg","alt":"A"}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	c, err := Compile(page, WithComponentSet(set), WithThemeSettings(&ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}), WithAssetProbe(probe))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// 候选按宽度升序输出，URL 原样取自 probe（组件层不再自行拼名）。
	want := `srcset="/storage/a_thumb-1-ab12cd34.jpg 320w, /storage/a_medium-1-ab12cd34.jpg 1280w"`
	if !strings.Contains(c.HTML, want) {
		t.Errorf("缺少 srcset 候选 %q\n%s", want, c.HTML)
	}
	if !strings.Contains(c.HTML, `sizes="(max-width: 640px) 100vw, 50vw"`) {
		t.Errorf("缺少 sizes\n%s", c.HTML)
	}
}

// TestGalleryPerItemLoading 图集单图设置优先于组件级设置：轮播/网格里每张图可独立
// 配置 loading 与 fetchPriority（空=继承组件级 → 主题默认）。
func TestGalleryPerItemLoading(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	theme := &ThemeSettings{Images: ThemeImages{LazyLoad: "on", Skeleton: true}}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"g2","type":"core.gallery","props":{"loading":"on","items":[{"url":"/storage/a.jpg","alt":"A"},{"url":"/storage/b.jpg","alt":"B","loading":"off","fetchPriority":"high"}]}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	c, err := Compile(page, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// 第一张：未设置 → 继承组件级 on → 懒加载 + 骨架
	if !strings.Contains(c.HTML, `class="gi is-skeleton" src="/storage/a.jpg" loading="lazy"`) {
		t.Errorf("第一张应继承组件级懒加载与骨架\n%s", c.HTML)
	}
	// 第二张：单图 off 覆盖组件级 → 立即加载、无骨架，并带高优先级提示
	if !strings.Contains(c.HTML, `class="gi" src="/storage/b.jpg" loading="eager" fetchpriority="high"`) {
		t.Errorf("第二张应按单图设置立即加载 + 高优先\n%s", c.HTML)
	}
}

// TestCardFetchPriority 组件级 fetchPriority 输出到 <img>（空=auto 不输出属性）。
func TestCardFetchPriority(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	theme := &ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c9","type":"core.card","props":{"title":"T","imageSrc":"/storage/a.jpg","loading":"off","fetchPriority":"high"}}]}`
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
	// 未设置时不输出属性（默认 auto）
	doc2 := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c10","type":"core.card","props":{"title":"T","imageSrc":"/storage/a.jpg"}}]}`
	page2, _ := ParsePage([]byte(doc2))
	c2, err := Compile(page2, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c2.HTML, "fetchpriority") {
		t.Errorf("未设置时不应输出 fetchpriority\n%s", c2.HTML)
	}
}

// TestGalleryCarouselFirstEager 轮播首图优先加载默认开启（首图 eager + high），
// 可在 carousel.firstEagerOff 关闭后回落到组件级/主题设置。
func TestGalleryCarouselFirstEager(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	theme := &ThemeSettings{Images: ThemeImages{LazyLoad: "on"}}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"gc1","type":"core.gallery","props":{"mode":"carousel","items":[{"url":"/storage/a.jpg","alt":"A"},{"url":"/storage/b.jpg","alt":"B"}]}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	c, err := Compile(page, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, `src="/storage/a.jpg" loading="eager" fetchpriority="high"`) {
		t.Errorf("轮播首图应默认立即加载 + 高优先\n%s", c.HTML)
	}
	if !strings.Contains(c.HTML, `src="/storage/b.jpg" loading="lazy" fetchpriority="low"`) {
		t.Errorf("轮播非首图应懒加载 + 低优先\n%s", c.HTML)
	}

	// 关闭开关：首图回落主题懒加载
	doc2 := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"gc2","type":"core.gallery","props":{"mode":"carousel","carousel":{"firstEagerOff":true},"items":[{"url":"/storage/a.jpg","alt":"A"}]}}]}`
	page2, _ := ParsePage([]byte(doc2))
	c2, err := Compile(page2, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c2.HTML, `src="/storage/a.jpg" loading="lazy"`) {
		t.Errorf("关闭首图优先加载后应回落懒加载\n%s", c2.HTML)
	}
}
